package task

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/media"
)

type Store struct {
	cfg     *config.Config
	scanner *media.Scanner
	runner  *Runner

	queue   chan string
	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	cacheMu    sync.RWMutex
	cache      []Task
	cacheAt    time.Time
	cacheErr   string
	refreshing bool
}

func NewStore(cfg *config.Config, scanner *media.Scanner, runner *Runner) *Store {
	store := &Store{
		cfg:     cfg,
		scanner: scanner,
		runner:  runner,
		queue:   make(chan string, 256),
		cancels: map[string]context.CancelFunc{},
	}
	for i := 0; i < cfg.TaskConcurrency; i++ {
		go store.worker()
	}
	return store
}

func (s *Store) List(filter ListFilter) ([]Task, error) {
	s.cacheMu.RLock()
	cached := cloneTasks(s.cache)
	cacheErr := s.cacheErr
	refreshing := s.refreshing
	s.cacheMu.RUnlock()
	refreshWindow := s.cfg.TaskRefreshWindow
	if refreshWindow <= 0 {
		refreshWindow = 5 * time.Minute
	}
	if !refreshing && (len(cached) == 0 || time.Since(s.cacheAt) > refreshWindow) {
		go func() {
			_ = s.Refresh(context.Background())
		}()
	}
	if filter.State == "" {
		return cached, nil
	}
	filtered := make([]Task, 0, len(cached))
	for _, task := range cached {
		if task.State == filter.State {
			filtered = append(filtered, task)
		}
	}
	if cacheErr != "" && len(filtered) == 0 {
		return filtered, nil
	}
	return filtered, nil
}

func (s *Store) Status() ListStatus {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	var refreshedAt *time.Time
	if !s.cacheAt.IsZero() {
		t := s.cacheAt
		refreshedAt = &t
	}
	return ListStatus{
		Refreshing:  s.refreshing,
		RefreshedAt: refreshedAt,
		Error:       s.cacheErr,
	}
}

func (s *Store) Refresh(ctx context.Context) error {
	s.cacheMu.Lock()
	if s.refreshing {
		s.cacheMu.Unlock()
		return nil
	}
	s.refreshing = true
	s.cacheMu.Unlock()
	tasks, err := s.scanOutput(ctx)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.refreshing = false
	s.cacheAt = time.Now().UTC()
	if err != nil {
		s.cacheErr = err.Error()
		return err
	}
	s.cacheErr = ""
	s.cache = compactTasks(tasks)
	return nil
}

func (s *Store) scanOutput(ctx context.Context) ([]Task, error) {
	entries, err := os.ReadDir(s.cfg.Output)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}
	tasks := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return tasks, err
		}
		if !entry.IsDir() {
			continue
		}
		task, err := s.read(entry.Name(), false)
		if err != nil {
			continue
		}
		tasks = append(tasks, compactTask(*task))
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	return tasks, nil
}

func (s *Store) Read(id string) (*Task, error) {
	return s.read(id, true)
}

func (s *Store) read(id string, includeFiles bool) (*Task, error) {
	if err := validateTaskID(id); err != nil {
		return nil, fmt.Errorf("invalid task id")
	}
	path := filepath.Join(s.cfg.Output, id, JobFile)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	task, err := decodeTask(content)
	if err != nil {
		return nil, err
	}
	task.ID = firstNonEmpty(task.ID, id)
	task.OutputDir = filepath.Join(s.cfg.Output, id)
	if includeFiles {
		task.Files = collectFiles(task.OutputDir)
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = jobModTime(path)
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = task.UpdatedAt
	}
	return task, nil
}

func (s *Store) Create(ctx context.Context, item media.Item, params Params) (*Task, error) {
	if err := media.ValidateUnderRoot(s.cfg.MediaRoot, item.Path); err != nil {
		return nil, err
	}
	if params.ExtractStreams == nil {
		extractStreams := true
		params.ExtractStreams = &extractStreams
	}
	id := s.newID()
	now := time.Now().UTC()
	enableEncode := s.cfg.EnableEncode
	enableSprites := s.cfg.EnableSprite
	if params.EnableEncode == nil {
		params.EnableEncode = &enableEncode
	}
	if params.EnableSprites == nil {
		params.EnableSprites = &enableSprites
	}
	if len(params.Encoders) == 0 {
		params.Encoders = s.cfg.Encoders()
	}
	if params.VideoExt == "" {
		params.VideoExt = s.cfg.VideoExt
	}
	if params.Quality == "" {
		params.Quality = s.cfg.ConstantQuality
	}
	if params.AudioKbps <= 0 {
		params.AudioKbps = s.cfg.AudioKbps
	}
	task := &Task{
		ID:           id,
		MediaID:      item.ID,
		InputPath:    item.Path,
		InputRelPath: item.RelPath,
		InputParent:  filepath.Dir(item.Path),
		Input:        item.FileName,
		OutputDir:    filepath.Join(s.cfg.Output, id),
		State:        StateQueued,
		Params:       params,
		CreatedAt:    now,
		UpdatedAt:    now,
		OriSize:      item.Size,
		OriModTime:   item.ModTime.Unix(),
		Media:        &item,
	}
	if err := os.MkdirAll(task.OutputDir, 0755); err != nil {
		return nil, err
	}
	if err := s.write(task); err != nil {
		return nil, err
	}
	s.upsertCache(task)
	select {
	case s.queue <- task.ID:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Store) Cancel(id string) (*Task, error) {
	s.mu.Lock()
	cancel := s.cancels[id]
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	task, err := s.Read(id)
	if err != nil {
		return nil, err
	}
	if task.State == StateQueued {
		task.State = StateCanceled
		now := time.Now().UTC()
		task.FinishedAt = &now
		task.UpdatedAt = now
		task.Error = "canceled before start"
		err = s.write(task)
	}
	s.upsertCache(task)
	return task, err
}

func (s *Store) Retry(ctx context.Context, id string) (*Task, error) {
	task, err := s.Read(id)
	if err != nil {
		return nil, err
	}
	if task.InputPath == "" {
		return nil, fmt.Errorf("task has no input path")
	}
	now := time.Now().UTC()
	task.State = StateQueued
	task.Error = ""
	task.StartedAt = nil
	task.FinishedAt = nil
	task.UpdatedAt = now
	if err := s.write(task); err != nil {
		return nil, err
	}
	s.upsertCache(task)
	select {
	case s.queue <- task.ID:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Store) worker() {
	for id := range s.queue {
		task, err := s.Read(id)
		if err != nil || task.State != StateQueued {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancels[id] = cancel
		s.mu.Unlock()
		err = s.runner.Run(ctx, task)
		s.mu.Lock()
		delete(s.cancels, id)
		s.mu.Unlock()
		cancel()
		if err != nil {
			_ = s.runner.fail(task, err)
			s.upsertCache(task)
			continue
		}
		s.upsertCache(task)
	}
}

func (s *Store) write(task *Task) error {
	return writeTask(task)
}

func (s *Store) upsertCache(task *Task) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	for i := range s.cache {
		if s.cache[i].ID == task.ID {
			s.cache[i] = compactTask(*task)
			return
		}
	}
	s.cache = append([]Task{compactTask(*task)}, s.cache...)
}

func writeTask(task *Task) error {
	if err := os.MkdirAll(task.OutputDir, 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(task.OutputDir, JobFile), content, 0644)
}

func decodeTask(content []byte) (*Task, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, err
	}
	if _, legacyID := raw["Id"]; legacyID {
		return decodeLegacyTask(content)
	}
	task := &Task{}
	if err := json.Unmarshal(content, task); err != nil {
		return nil, err
	}
	return task, nil
}

func decodeLegacyTask(content []byte) (*Task, error) {
	var legacy struct {
		Id             string
		InputParent    string
		Input          string
		State          string
		EncodedCodecs  []string
		MappedAudio    map[string][]Stream
		Streams        []Stream
		Duration       float64
		Width          int
		Height         int
		EncodedExt     string
		Chapters       []Chapter
		DominantColors []string
		OriSize        int64
		OriModTime     int64
		Fast           bool
	}
	if err := json.Unmarshal(content, &legacy); err != nil {
		return nil, err
	}
	task := &Task{}
	task.ID = legacy.Id
	task.InputParent = legacy.InputParent
	task.Input = legacy.Input
	task.State = legacy.State
	task.EncodedCodecs = legacy.EncodedCodecs
	task.MappedAudio = legacy.MappedAudio
	task.Streams = legacy.Streams
	task.Duration = legacy.Duration
	task.Width = legacy.Width
	task.Height = legacy.Height
	task.EncodedExt = legacy.EncodedExt
	task.Chapters = legacy.Chapters
	task.DominantColors = legacy.DominantColors
	task.OriSize = legacy.OriSize
	task.OriModTime = legacy.OriModTime
	task.Params.Fast = legacy.Fast
	extractStreams := true
	task.Params.ExtractStreams = &extractStreams
	task.Legacy = true
	return task, nil
}

func collectFiles(dir string) map[string]int64 {
	files := map[string]int64{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		stat, err := entry.Info()
		if err != nil {
			continue
		}
		files[entry.Name()] = stat.Size()
	}
	return files
}

func newestModTime(dir string) time.Time {
	var newest time.Time
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Now().UTC()
	}
	for _, entry := range entries {
		stat, err := entry.Info()
		if err == nil && stat.ModTime().After(newest) {
			newest = stat.ModTime()
		}
	}
	if newest.IsZero() {
		return time.Now().UTC()
	}
	return newest.UTC()
}

func jobModTime(path string) time.Time {
	stat, err := os.Stat(path)
	if err != nil {
		return time.Now().UTC()
	}
	return stat.ModTime().UTC()
}

func (s *Store) newID() string {
	for {
		id := randomID(5)
		if _, err := os.Stat(filepath.Join(s.cfg.Output, id)); os.IsNotExist(err) {
			return id
		}
	}
}

func randomID(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	for i := range buf {
		buf[i] = chars[int(buf[i])%len(chars)]
	}
	return string(buf)
}

func validateTaskID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("empty or reserved id")
	}
	if filepath.Clean(id) != id {
		return fmt.Errorf("unclean id")
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("path separators are not allowed")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	copy(out, tasks)
	return out
}

func compactTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i, task := range tasks {
		out[i] = compactTask(task)
	}
	return out
}

func compactTask(task Task) Task {
	task.Streams = nil
	task.MappedAudio = nil
	task.Chapters = nil
	task.DominantColors = nil
	task.Media = nil
	task.Files = nil
	return task
}
