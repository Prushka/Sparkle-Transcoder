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
	active  map[string]Task

	cacheMu    sync.RWMutex
	cache      []Task
	cacheAt    time.Time
	cacheErr   string
	refreshing bool
}

type queuedTaskCandidate struct {
	id        string
	createdAt time.Time
	updatedAt time.Time
}

var ErrTaskRunning = errors.New("task is running; cancel it before deleting")
var ErrTaskActive = errors.New("task is already queued or running")

const generatedTaskIDLength = 10

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
		active:  map[string]Task{},
	}
	if runner != nil {
		runner.SetUpdateHook(store.recordRunnerTask)
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
	var refreshedAt *time.Time
	if !s.cacheAt.IsZero() {
		t := s.cacheAt
		refreshedAt = &t
	}
	refreshing := s.refreshing
	cacheErr := s.cacheErr
	s.cacheMu.RUnlock()
	return ListStatus{
		Refreshing:  refreshing,
		RefreshedAt: refreshedAt,
		Error:       cacheErr,
		ActiveTasks: s.activeTasks(),
	}
}

func (s *Store) Refresh(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	scanStarted := time.Now().UTC()
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
	s.cache = s.mergeRefreshTasks(cacheTasks(tasks), scanStarted)
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
	if task.State == StateQueued || task.State == StateRunning {
		s.mu.Unlock()
		return nil, ErrTaskActive
	}
	inputPath, err := s.recoverInputPath(task)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	outputDir, err := s.taskDir(task.ID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := os.RemoveAll(outputDir); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	now := time.Now().UTC()
	resetTaskForQueue(task, inputPath, outputDir, now)
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

func (s *Store) RecoverActive(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tasks, err := s.scanOutput(ctx)
	if err != nil {
		return 0, err
	}
	recovered := 0
	errs := make([]error, 0)
	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return recovered, errors.Join(append(errs, err)...)
		}
		if !isRecoverableState(task.State) {
			continue
		}
		next := task
		if err := s.recoverTask(ctx, &next); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", task.ID, err))
			continue
		}
		recovered++
	}
	return recovered, errors.Join(errs...)
}

func (s *Store) recoverTask(ctx context.Context, task *Task) error {
	if err := validateTaskID(task.ID); err != nil {
		return err
	}
	if s.queue == nil {
		return fmt.Errorf("task queue is not initialized")
	}
	inputPath, err := s.recoverInputPath(task)
	if err != nil {
		return err
	}
	outputDir, err := s.taskDir(task.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	queuedAt := now
	if task.State == StateQueued && !task.UpdatedAt.IsZero() {
		queuedAt = task.UpdatedAt
	}

	s.mu.Lock()
	if s.cancels[task.ID] != nil {
		s.mu.Unlock()
		return ErrTaskActive
	}
	if err := os.RemoveAll(outputDir); err != nil {
		s.mu.Unlock()
		return err
	}
	resetTaskForQueue(task, inputPath, outputDir, queuedAt)
	if err := s.write(task); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.upsertCache(task)
	if err := s.enqueue(task.ID); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Store) recoverInputPath(task *Task) (string, error) {
	candidates := []string{task.InputPath}
	if task.InputParent != "" && task.Input != "" {
		candidates = append(candidates, filepath.Join(task.InputParent, task.Input))
	}
	if task.InputRelPath != "" && s.cfg.MediaRoot != "" {
		candidates = append(candidates, filepath.Join(s.cfg.MediaRoot, filepath.FromSlash(task.InputRelPath)))
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) && s.cfg.MediaRoot != "" {
			candidate = filepath.Join(s.cfg.MediaRoot, candidate)
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if s.cfg.MediaRoot != "" {
			if err := media.ValidateUnderRoot(s.cfg.MediaRoot, abs); err != nil {
				continue
			}
		}
		stat, err := os.Stat(abs)
		if err == nil && !stat.IsDir() {
			return abs, nil
		}
	}
	return "", fmt.Errorf("input path is missing or unavailable")
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
			s.runNextQueued(id)
		}
	}
}

func (s *Store) runNextQueued(triggerID string) {
	task, ctx, cancel, ok := s.claimNextQueued(triggerID)
	if !ok {
		return
	}
	s.runClaimed(task, ctx, cancel)
}

func (s *Store) runQueued(id string) {
	task, ctx, cancel, ok := s.claimQueued(id)
	if !ok {
		return
	}
	s.runClaimed(task, ctx, cancel)
}

func (s *Store) runClaimed(task *Task, ctx context.Context, cancel context.CancelFunc) {
	id := task.ID
	err := s.runner.Run(ctx, task)
	cancel()
	if err != nil {
		if errors.Is(err, context.Canceled) && s.context().Err() != nil {
			// Shutdown interruptions stay recoverable for the next startup.
			s.releaseRunning(id)
			return
		}
		_ = s.runner.fail(task, err)
		s.upsertCache(task)
		s.releaseRunning(id)
		return
	}
	s.upsertCache(task)
	s.releaseRunning(id)
}

func (s *Store) claimNextQueued(triggerID string) (*Task, context.Context, context.CancelFunc, bool) {
	for _, id := range s.queuedTaskIDsByPriority(triggerID) {
		if task, ctx, cancel, ok := s.claimQueued(id); ok {
			return task, ctx, cancel, true
		}
	}
	return nil, nil, nil, false
}

func (s *Store) queuedTaskIDsByPriority(triggerID string) []string {
	candidates := make([]queuedTaskCandidate, 0)
	seen := map[string]bool{}
	add := func(task Task) {
		if task.ID == "" || seen[task.ID] || task.State != StateQueued {
			return
		}
		seen[task.ID] = true
		candidates = append(candidates, queuedTaskCandidate{
			id:        task.ID,
			createdAt: task.CreatedAt,
			updatedAt: task.UpdatedAt,
		})
	}

	s.cacheMu.RLock()
	for _, task := range s.cache {
		add(task)
	}
	s.cacheMu.RUnlock()

	if triggerID != "" && !seen[triggerID] {
		if task, err := s.read(triggerID, false); err == nil {
			add(*task)
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if !left.updatedAt.Equal(right.updatedAt) {
			return left.updatedAt.After(right.updatedAt)
		}
		if !left.createdAt.Equal(right.createdAt) {
			return left.createdAt.After(right.createdAt)
		}
		return left.id > right.id
	})

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.id)
	}
	return ids
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
	delete(s.active, id)
	s.mu.Unlock()
}

func (s *Store) recordRunnerTask(task *Task) {
	if task == nil || task.ID == "" {
		return
	}
	s.upsertCache(task)
	active := cacheTask(*task)
	s.mu.Lock()
	if s.active == nil {
		s.active = map[string]Task{}
	}
	if isActiveRunnerState(active.State) {
		active.Running = true
		s.active[active.ID] = active
	} else {
		delete(s.active, active.ID)
	}
	s.mu.Unlock()
}

func (s *Store) activeTasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]Task, 0, len(s.active))
	for _, task := range s.active {
		if !isActiveRunnerState(task.State) {
			continue
		}
		task.Running = true
		tasks = append(tasks, cloneTask(task))
	}
	sort.Slice(tasks, func(i, j int) bool {
		left := tasks[i].UpdatedAt
		right := tasks[j].UpdatedAt
		if tasks[i].StartedAt != nil {
			left = *tasks[i].StartedAt
		}
		if tasks[j].StartedAt != nil {
			right = *tasks[j].StartedAt
		}
		if left.Equal(right) {
			return tasks[i].ID < tasks[j].ID
		}
		return left.Before(right)
	})
	return tasks
}

func (s *Store) mergeRefreshTasks(tasks []Task, scanStarted time.Time) []Task {
	byID := make(map[string]int, len(tasks))
	next := tasks[:0]
	for _, task := range tasks {
		if !taskFileExists(task.OutputDir) {
			continue
		}
		byID[task.ID] = len(next)
		next = append(next, task)
	}
	for _, task := range s.cache {
		if task.UpdatedAt.Before(scanStarted) {
			continue
		}
		if idx, ok := byID[task.ID]; ok {
			next[idx] = cloneTask(task)
			continue
		}
		byID[task.ID] = len(next)
		next = append(next, cloneTask(task))
	}
	s.mu.Lock()
	for _, task := range s.active {
		active := cacheTask(task)
		active.Running = true
		if idx, ok := byID[active.ID]; ok {
			next[idx] = active
			continue
		}
		byID[active.ID] = len(next)
		next = append(next, active)
	}
	s.mu.Unlock()
	sortTasksByUpdatedAt(next)
	return next
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

func taskFileExists(outputDir string) bool {
	if outputDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(outputDir, JobFile))
	return err == nil
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
		id := randomID(generatedTaskIDLength)
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
	for i, task := range tasks {
		out[i] = cloneTask(task)
	}
	return out
}

func cloneTask(task Task) Task {
	task.EncodedCodecs = append([]string(nil), task.EncodedCodecs...)
	task.SubtitleLangs = append([]string(nil), task.SubtitleLangs...)
	task.Streams = append([]Stream(nil), task.Streams...)
	task.Chapters = append([]Chapter(nil), task.Chapters...)
	task.DominantColors = append([]string(nil), task.DominantColors...)
	if task.MappedAudio != nil {
		mapped := make(map[string][]Stream, len(task.MappedAudio))
		for key, streams := range task.MappedAudio {
			mapped[key] = append([]Stream(nil), streams...)
		}
		task.MappedAudio = mapped
	}
	if task.Files != nil {
		files := make(map[string]int64, len(task.Files))
		for key, size := range task.Files {
			files[key] = size
		}
		task.Files = files
	}
	return task
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
	task = cloneTask(task)
	task.SubtitleLangs = subtitleLanguages(task.Streams)
	task.Media = nil
	return task
}

func compactTask(task Task) Task {
	task = cloneTask(task)
	task.SubtitleLangs = subtitleLanguages(task.Streams)
	task.Streams = nil
	task.MappedAudio = nil
	task.Chapters = nil
	task.DominantColors = nil
	task.Media = nil
	task.Files = nil
	return task
}

func resetTaskForQueue(task *Task, inputPath string, outputDir string, now time.Time) {
	task.InputPath = inputPath
	if task.Input == "" {
		task.Input = filepath.Base(inputPath)
	}
	if task.InputParent == "" {
		task.InputParent = filepath.Dir(inputPath)
	}
	task.OutputDir = outputDir
	task.State = StateQueued
	task.Running = false
	task.Error = ""
	task.StartedAt = nil
	task.FinishedAt = nil
	task.UpdatedAt = now
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if stat, err := os.Stat(inputPath); err == nil {
		task.OriSize = stat.Size()
		task.OriModTime = stat.ModTime().Unix()
	}
	task.EncodedCodecs = nil
	task.SubtitleLangs = nil
	task.MappedAudio = nil
	task.Streams = nil
	task.Duration = 0
	task.Width = 0
	task.Height = 0
	task.EncodedExt = ""
	task.Chapters = nil
	task.DominantColors = nil
	task.Files = nil
	task.Media = nil
}

func isRecoverableState(state string) bool {
	switch state {
	case StateQueued, StateRunning, StateIncomplete, StateStreamsExtracted:
		return true
	default:
		return false
	}
}

func isActiveRunnerState(state string) bool {
	switch state {
	case StateRunning, StateIncomplete, StateStreamsExtracted:
		return true
	default:
		return false
	}
}

func sortTasksByUpdatedAt(tasks []Task) {
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].UpdatedAt.Equal(tasks[j].UpdatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
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
