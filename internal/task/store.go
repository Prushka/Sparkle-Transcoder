package task

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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

	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	queue   chan string
	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	cacheMu    sync.RWMutex
	cache      []Task
	cacheAt    time.Time
	cacheErr   string
	refreshing bool
}

var ErrTaskRunning = errors.New("task is running; cancel it before deleting")
var ErrTaskActive = errors.New("task is already queued or running")

func NewStore(cfg *config.Config, scanner *media.Scanner, runner *Runner) *Store {
	return NewStoreWithContext(context.Background(), cfg, scanner, runner)
}

func NewStoreWithContext(ctx context.Context, cfg *config.Config, scanner *media.Scanner, runner *Runner) *Store {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := context.WithCancel(ctx)
	store := &Store{
		cfg:     cfg,
		scanner: scanner,
		runner:  runner,
		ctx:     ctx,
		stop:    stop,
		queue:   make(chan string, 256),
		cancels: map[string]context.CancelFunc{},
	}
	for i := 0; i < cfg.TaskConcurrency; i++ {
		store.wg.Add(1)
		go func() {
			defer store.wg.Done()
			store.worker()
		}()
	}
	return store
}

func (s *Store) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.stop != nil {
		s.stop()
	}

	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *Store) List(filter ListFilter) ([]Task, error) {
	s.cacheMu.RLock()
	cached := cloneTasks(s.cache)
	s.cacheMu.RUnlock()
	s.markRunning(cached)
	if filter.State == "" {
		return cached, nil
	}
	filtered := make([]Task, 0, len(cached))
	for _, task := range cached {
		if task.State == filter.State {
			filtered = append(filtered, task)
		}
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
	s.cache = cacheTasks(tasks)
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
		task, err := s.read(entry.Name(), true)
		if err != nil {
			continue
		}
		tasks = append(tasks, *task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	return tasks, nil
}

func (s *Store) Read(id string) (*Task, error) {
	task, err := s.read(id, true)
	if err != nil {
		return nil, err
	}
	tasks := []Task{*task}
	s.markRunning(tasks)
	*task = tasks[0]
	return task, nil
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
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if err := media.ValidateUnderRoot(s.cfg.MediaRoot, item.Path); err != nil {
		return nil, err
	}
	if params.ExtractStreams == nil {
		extractStreams := true
		params.ExtractStreams = &extractStreams
	}
	id, outputDir, err := s.reserveTaskDir()
	if err != nil {
		return nil, err
	}
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
		OutputDir:    outputDir,
		State:        StateQueued,
		Params:       params,
		CreatedAt:    now,
		UpdatedAt:    now,
		OriSize:      item.Size,
		OriModTime:   item.ModTime.Unix(),
		Media:        &item,
	}
	if err := s.write(task); err != nil {
		return nil, err
	}
	s.upsertCache(task)
	if err := s.enqueue(task.ID); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Store) enqueue(id string) error {
	if s.queue == nil {
		return fmt.Errorf("task queue is not initialized")
	}
	storeCtx := s.context()
	select {
	case s.queue <- id:
		return nil
	case <-storeCtx.Done():
		return storeCtx.Err()
	}
}

func (s *Store) Cancel(id string) (*Task, error) {
	s.mu.Lock()
	cancel := s.cancels[id]
	if cancel != nil {
		cancel()
	}
	task, err := s.read(id, true)
	if err != nil {
		s.mu.Unlock()
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
	task.Running = cancel != nil
	s.mu.Unlock()
	s.upsertCache(task)
	return task, err
}

func (s *Store) Retry(ctx context.Context, id string) (*Task, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	if s.cancels[id] != nil {
		s.mu.Unlock()
		return nil, ErrTaskActive
	}
	task, err := s.read(id, true)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if task.InputPath == "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("task has no input path")
	}
	if task.State == StateQueued || task.State == StateRunning {
		s.mu.Unlock()
		return nil, ErrTaskActive
	}
	now := time.Now().UTC()
	task.State = StateQueued
	task.Error = ""
	task.StartedAt = nil
	task.FinishedAt = nil
	task.UpdatedAt = now
	if err := s.write(task); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	s.upsertCache(task)
	if err := s.enqueue(task.ID); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Store) Delete(id string) error {
	dir, err := s.taskDir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	task, err := s.read(id, true)
	if err == nil && task.State == StateRunning {
		s.mu.Unlock()
		return ErrTaskRunning
	}
	cancel := s.cancels[id]
	if cancel != nil {
		s.mu.Unlock()
		return ErrTaskRunning
	}
	if err := os.RemoveAll(dir); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.removeCache(id)
	return nil
}

func (s *Store) worker() {
	for {
		select {
		case <-s.context().Done():
			return
		case id, ok := <-s.queue:
			if !ok {
				return
			}
			s.runQueued(id)
		}
	}
}

func (s *Store) runQueued(id string) {
	task, ctx, cancel, ok := s.claimQueued(id)
	if !ok {
		return
	}

	err := s.runner.Run(ctx, task)
	cancel()
	if err != nil {
		_ = s.runner.fail(task, err)
		s.upsertCache(task)
		s.releaseRunning(id)
		return
	}
	s.upsertCache(task)
	s.releaseRunning(id)
}

func (s *Store) claimQueued(id string) (*Task, context.Context, context.CancelFunc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
	if s.context().Err() != nil || s.cancels[id] != nil {
		return nil, nil, nil, false
	}
	task, err := s.read(id, true)
	if err != nil || task.State != StateQueued {
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithCancel(s.context())
	s.cancels[id] = cancel
	task.Running = true
	return task, ctx, cancel, true
}

func (s *Store) releaseRunning(id string) {
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

func (s *Store) taskDir(id string) (string, error) {
	if err := validateTaskID(id); err != nil {
		return "", fmt.Errorf("invalid task id")
	}
	output, err := filepath.Abs(s.cfg.Output)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(output, id)
	rel, err := filepath.Rel(output, dir)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("task directory outside output root")
	}
	return dir, nil
}

func (s *Store) write(task *Task) error {
	return writeTask(task)
}

func (s *Store) upsertCache(task *Task) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	for i := range s.cache {
		if s.cache[i].ID == task.ID {
			s.cache[i] = cacheTask(*task)
			return
		}
	}
	s.cache = append([]Task{cacheTask(*task)}, s.cache...)
}

func (s *Store) removeCache(id string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	next := s.cache[:0]
	for _, task := range s.cache {
		if task.ID != id {
			next = append(next, task)
		}
	}
	s.cache = next
}

func writeTask(task *Task) error {
	if err := os.MkdirAll(task.OutputDir, 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(task.OutputDir, ".job-*.tmp")
	if err != nil {
		return err
	}
	tmpName := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0644); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := replaceTaskFile(tmpName, filepath.Join(task.OutputDir, JobFile)); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func replaceTaskFile(tmpName, target string) error {
	if err := os.Rename(tmpName, target); err != nil {
		if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
			return err
		}
		return os.Rename(tmpName, target)
	}
	return nil
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
		if isTaskWriteTemp(entry.Name()) {
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

func isTaskWriteTemp(name string) bool {
	return strings.HasPrefix(name, ".job-") && strings.HasSuffix(name, ".tmp")
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

func (s *Store) reserveTaskDir() (string, string, error) {
	if err := os.MkdirAll(s.cfg.Output, 0755); err != nil {
		return "", "", err
	}
	for {
		id := randomID(5)
		dir := filepath.Join(s.cfg.Output, id)
		if err := os.Mkdir(dir, 0755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		return id, dir, nil
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

func (s *Store) markRunning(tasks []Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range tasks {
		tasks[i].Running = s.cancels[tasks[i].ID] != nil
	}
}

func compactTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i, task := range tasks {
		out[i] = compactTask(task)
	}
	return out
}

func cacheTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i, task := range tasks {
		out[i] = cacheTask(task)
	}
	return out
}

func cacheTask(task Task) Task {
	task.SubtitleLangs = subtitleLanguages(task.Streams)
	task.Media = nil
	return task
}

func compactTask(task Task) Task {
	task.SubtitleLangs = subtitleLanguages(task.Streams)
	task.Streams = nil
	task.MappedAudio = nil
	task.Chapters = nil
	task.DominantColors = nil
	task.Media = nil
	task.Files = nil
	return task
}

func subtitleLanguages(streams []Stream) []string {
	seen := map[string]bool{}
	for _, stream := range streams {
		if stream.CodecType != subtitleType {
			continue
		}
		lang := strings.TrimSpace(stream.Language)
		if lang == "" {
			lang = "und"
		}
		seen[lang] = true
	}
	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}
