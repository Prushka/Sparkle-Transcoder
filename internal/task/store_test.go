package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/executil"
	"sparkle-transcoder/internal/media"
)

func TestListReadsLegacyJobJSON(t *testing.T) {
	output := t.TempDir()
	jobDir := filepath.Join(output, "S1Van")
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"Id":"S1Van","Input":"Example - S01E01.mkv","State":"complete","EncodedCodecs":["hevc"],"OriSize":42,"Fast":true}`
	if err := os.WriteFile(filepath.Join(jobDir, JobFile), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	store := &Store{cfg: &config.Config{Output: output}}
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	got := tasks[0]
	if got.ID != "S1Van" || got.State != StateComplete || !got.Params.Fast {
		t.Fatalf("legacy task not decoded correctly: %+v", got)
	}
	if got.Params.ExtractStreams == nil || !*got.Params.ExtractStreams {
		t.Fatalf("legacy extract streams = %+v, want true", got.Params.ExtractStreams)
	}
	if len(got.EncodedCodecs) != 1 || got.EncodedCodecs[0] != "hevc" {
		t.Fatalf("encoded codecs = %+v", got.EncodedCodecs)
	}
}

func TestDecodeNewTaskJSON(t *testing.T) {
	content := []byte(`{"id":"abc12","input":"movie.mkv","state":"queued","params":{"fast":true,"extractStreams":true}}`)
	got, err := decodeTask(content)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "abc12" || got.State != StateQueued || !got.Params.Fast {
		t.Fatalf("task = %+v", got)
	}
	if got.Params.ExtractStreams == nil || !*got.Params.ExtractStreams {
		t.Fatalf("extract streams = %+v, want true", got.Params.ExtractStreams)
	}
}

func TestListDoesNotScanOutput(t *testing.T) {
	output := t.TempDir()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateComplete,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	store := &Store{cfg: &config.Config{Output: output}}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("List returned %d tasks before explicit refresh, want 0", len(tasks))
	}
}

func TestRefreshIncludesTaskDetails(t *testing.T) {
	output := t.TempDir()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateComplete,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Streams:   []Stream{{CodecType: subtitleType, Language: "eng", Index: 2}},
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.OutputDir, "hevc.mp4"), []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	store := &Store{cfg: &config.Config{Output: output}}
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if len(tasks[0].Streams) != 1 {
		t.Fatalf("streams = %+v, want task details", tasks[0].Streams)
	}
	if tasks[0].Files["hevc.mp4"] != int64(len("video")) {
		t.Fatalf("files = %+v, want output file size", tasks[0].Files)
	}
}

func TestFailPersistsErrorReason(t *testing.T) {
	output := t.TempDir()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateRunning,
		Running:   true,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := os.MkdirAll(task.OutputDir, 0755); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(&config.Config{}, nil, fakeExec{})
	if err := runner.fail(task, errors.New("HandBrakeCLI failed: encoder unavailable")); err != nil {
		t.Fatal(err)
	}
	got, err := decodeTask(mustReadFile(t, filepath.Join(task.OutputDir, JobFile)))
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateFailed {
		t.Fatalf("state = %s, want %s", got.State, StateFailed)
	}
	if !strings.Contains(got.Error, "encoder unavailable") {
		t.Fatalf("error = %q, want failure reason", got.Error)
	}
	if got.Running {
		t.Fatal("failed job persisted running=true")
	}
}

func TestCreateHonorsExplicitExtractStreamsFalse(t *testing.T) {
	root := t.TempDir()
	output := t.TempDir()
	input := filepath.Join(root, "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	extractStreams := false
	store := &Store{
		cfg: &config.Config{
			MediaRoot:       root,
			Output:          output,
			EnableEncode:    true,
			EnableSprite:    true,
			VideoExt:        "mp4",
			ConstantQuality: "18",
			AudioKbps:       144,
			Encoder:         "hevc",
		},
		queue:   make(chan string, 1),
		cancels: map[string]context.CancelFunc{},
	}
	task, err := store.Create(context.Background(), testItem(input), Params{ExtractStreams: &extractStreams})
	if err != nil {
		t.Fatal(err)
	}
	if task.Params.ExtractStreams == nil || *task.Params.ExtractStreams {
		t.Fatalf("extract streams = %+v, want false", task.Params.ExtractStreams)
	}
}

func TestWorkerUpdatesListCacheAfterRun(t *testing.T) {
	output := t.TempDir()
	input := filepath.Join(t.TempDir(), "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	enableEncode := false
	enableSprites := false
	extractStreams := false
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		InputPath: input,
		Input:     filepath.Base(input),
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateQueued,
		Params: Params{
			EnableEncode:   &enableEncode,
			EnableSprites:  &enableSprites,
			ExtractStreams: &extractStreams,
			VideoExt:       "mp4",
			AudioKbps:      144,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Output: output, Ffprobe: "ffprobe", Ffmpeg: "ffmpeg"}
	store := &Store{
		cfg:     cfg,
		runner:  NewRunner(cfg, nil, fakeExec{}),
		queue:   make(chan string, 1),
		cancels: map[string]context.CancelFunc{},
		cache:   []Task{compactTask(*task)},
		cacheAt: now,
	}
	store.queue <- task.ID
	close(store.queue)

	done := make(chan struct{})
	go func() {
		store.worker()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish")
	}

	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].State != StateComplete {
		t.Fatalf("cached task state = %s, want %s", tasks[0].State, StateComplete)
	}
	if tasks[0].FinishedAt == nil {
		t.Fatal("cached task missing finished timestamp")
	}
	got, err := decodeTask(mustReadFile(t, filepath.Join(output, task.ID, JobFile)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Running {
		t.Fatal("completed job persisted running=true")
	}
}

func TestRunnerUpdateHookReportsIntermediateStates(t *testing.T) {
	output := t.TempDir()
	input := filepath.Join(t.TempDir(), "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	enableEncode := false
	enableSprites := false
	extractStreams := false
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		InputPath: input,
		Input:     filepath.Base(input),
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateQueued,
		Params: Params{
			EnableEncode:   &enableEncode,
			EnableSprites:  &enableSprites,
			ExtractStreams: &extractStreams,
			VideoExt:       "mp4",
			AudioKbps:      144,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	runner := NewRunner(&config.Config{Output: output, Ffprobe: "ffprobe", Ffmpeg: "ffmpeg"}, nil, fakeExec{})
	states := []string{}
	runner.SetUpdateHook(func(task *Task) {
		states = append(states, task.State)
	})
	if err := runner.Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{StateRunning, StateIncomplete, StateStreamsExtracted, StateComplete} {
		if !stringSliceContains(states, want) {
			t.Fatalf("runner update states = %+v, missing %s", states, want)
		}
	}
}

func TestRunnerPropagatesCanceledOptionalExtraction(t *testing.T) {
	output := t.TempDir()
	input := filepath.Join(t.TempDir(), "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}

	enableEncode := false
	enableSprites := false
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		InputPath: input,
		Input:     filepath.Base(input),
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateQueued,
		Params: Params{
			EnableEncode:  &enableEncode,
			EnableSprites: &enableSprites,
			VideoExt:      "mp4",
			AudioKbps:     144,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	runner := NewRunner(&config.Config{Output: output, Ffprobe: "ffprobe", Ffmpeg: "ffmpeg"}, nil, cancelStreamProbeExec{})
	err := runner.Run(context.Background(), task)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if task.State == StateStreamsExtracted || task.State == StateComplete {
		t.Fatalf("task advanced to %s after cancellation", task.State)
	}
}

func TestRecordRunnerTaskUpdatesCacheAndStatus(t *testing.T) {
	output := t.TempDir()
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateQueued,
		Params:    Params{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &Store{
		cfg:     &config.Config{Output: output},
		cancels: map[string]context.CancelFunc{task.ID: cancel},
		cache:   []Task{compactTask(*task)},
	}
	running := *task
	running.State = StateStreamsExtracted
	running.UpdatedAt = now.Add(time.Second)
	store.recordRunnerTask(&running)

	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != StateStreamsExtracted || !tasks[0].Running {
		t.Fatalf("List task = %+v, want active streams-extracted task", tasks)
	}
	status := store.Status()
	if len(status.ActiveTasks) != 1 || status.ActiveTasks[0].ID != task.ID || status.ActiveTasks[0].State != StateStreamsExtracted || !status.ActiveTasks[0].Running {
		t.Fatalf("active tasks = %+v, want active runner task", status.ActiveTasks)
	}
}

func TestRecordRunnerTaskRemovesTerminalActiveTask(t *testing.T) {
	output := t.TempDir()
	now := time.Now().UTC()
	active := Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateStreamsExtracted,
		Params:    Params{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	store := &Store{
		cfg:    &config.Config{Output: output},
		active: map[string]Task{active.ID: active},
		cache:  []Task{cacheTask(active)},
	}
	complete := active
	complete.State = StateComplete
	complete.Running = false
	complete.UpdatedAt = now.Add(time.Second)
	store.recordRunnerTask(&complete)

	status := store.Status()
	if len(status.ActiveTasks) != 0 {
		t.Fatalf("active tasks = %+v, want none after terminal runner update", status.ActiveTasks)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != StateComplete || tasks[0].Running {
		t.Fatalf("tasks = %+v, want complete non-running task", tasks)
	}
}

func TestRecoverActiveRequeuesPersistedActiveTasks(t *testing.T) {
	root := t.TempDir()
	output := t.TempDir()
	input := filepath.Join(root, "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	task := &Task{
		ID:             "abc12",
		MediaID:        "media-id",
		InputPath:      input,
		InputRelPath:   "Movies/Example/Example.mkv",
		InputParent:    filepath.Dir(input),
		Input:          filepath.Base(input),
		OutputDir:      filepath.Join(output, "abc12"),
		State:          StateStreamsExtracted,
		Running:        true,
		Error:          "previous failure",
		Params:         Params{VideoExt: "mp4"},
		CreatedAt:      started.Add(-time.Minute),
		UpdatedAt:      started,
		StartedAt:      &started,
		EncodedCodecs:  []string{"hevc"},
		Streams:        []Stream{{CodecType: subtitleType, Language: "eng", Index: 2}},
		Duration:       12,
		Width:          1920,
		Height:         1080,
		EncodedExt:     "mp4",
		DominantColors: []string{"#111111"},
		Files:          map[string]int64{"stale.mp4": 5},
		Media:          &media.Item{ID: "media-id"},
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(task.OutputDir, "stale.mp4")
	if err := os.WriteFile(staleFile, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	store := &Store{
		cfg:     &config.Config{MediaRoot: root, Output: output},
		queue:   make(chan string, 1),
		cancels: map[string]context.CancelFunc{},
	}
	recovered, err := store.RecoverActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	select {
	case id := <-store.queue:
		if id != task.ID {
			t.Fatalf("queued id = %s, want %s", id, task.ID)
		}
	default:
		t.Fatal("task was not enqueued")
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("stale output still exists, stat error = %v", err)
	}
	got, err := decodeTask(mustReadFile(t, filepath.Join(output, task.ID, JobFile)))
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateQueued || got.Running || got.Error != "" || got.StartedAt != nil || got.FinishedAt != nil {
		t.Fatalf("recovered task status = %+v, want clean queued task", got)
	}
	if len(got.EncodedCodecs) != 0 || len(got.Streams) != 0 || got.Duration != 0 || got.Width != 0 || got.Height != 0 || got.EncodedExt != "" || got.Files != nil || got.Media != nil {
		t.Fatalf("recovered task kept generated fields: %+v", got)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != StateQueued {
		t.Fatalf("cache tasks = %+v, want recovered queued task", tasks)
	}
}

func TestRefreshKeepsActiveRunnerTaskAfterStaleScan(t *testing.T) {
	output := t.TempDir()
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateIncomplete,
		Params:    Params{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	active := *task
	active.State = StateStreamsExtracted
	active.UpdatedAt = now.Add(time.Second)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &Store{
		cfg:     &config.Config{Output: output},
		cancels: map[string]context.CancelFunc{task.ID: cancel},
		active:  map[string]Task{task.ID: active},
		cache:   []Task{cacheTask(active)},
	}
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].State != StateStreamsExtracted || !tasks[0].Running {
		t.Fatalf("tasks = %+v, want active runner task to win over stale scan", tasks)
	}
}

func TestListAndReadMarkActivelyRunningTask(t *testing.T) {
	output := t.TempDir()
	taskDir := filepath.Join(output, "abc12")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: taskDir,
		State:     StateIncomplete,
		Params:    Params{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &Store{
		cfg:     &config.Config{Output: output},
		cancels: map[string]context.CancelFunc{task.ID: cancel},
		cache:   []Task{compactTask(*task)},
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || !tasks[0].Running {
		t.Fatalf("List running = %+v, want active task marked running", tasks)
	}
	got, err := store.Read(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Running {
		t.Fatalf("Read running = false, want true")
	}
}

func TestRetryCleansOutputAndRequeuesTask(t *testing.T) {
	root := t.TempDir()
	output := t.TempDir()
	input := filepath.Join(root, "Movies", "Example", "Example.mkv")
	if err := os.MkdirAll(filepath.Dir(input), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	task := &Task{
		ID:           "abc12",
		InputPath:    input,
		InputRelPath: "Movies/Example/Example.mkv",
		InputParent:  filepath.Dir(input),
		Input:        filepath.Base(input),
		OutputDir:    filepath.Join(output, "abc12"),
		State:        StateCanceled,
		Running:      true,
		Error:        "canceled",
		Params:       Params{VideoExt: "mp4"},
		CreatedAt:    started.Add(-time.Minute),
		UpdatedAt:    started,
		StartedAt:    &started,
		FinishedAt:   &started,
		Files:        map[string]int64{"stale.mp4": 5},
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(task.OutputDir, "stale.mp4")
	if err := os.WriteFile(staleFile, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		cfg:     &config.Config{MediaRoot: root, Output: output},
		queue:   make(chan string, 1),
		cancels: map[string]context.CancelFunc{},
		cache:   []Task{cacheTask(*task)},
	}

	retried, err := store.Retry(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.State != StateQueued || retried.Running || retried.Error != "" || retried.StartedAt != nil || retried.FinishedAt != nil {
		t.Fatalf("retried task = %+v, want clean queued task", retried)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("stale output still exists, stat error = %v", err)
	}
	select {
	case id := <-store.queue:
		if id != task.ID {
			t.Fatalf("queued id = %s, want %s", id, task.ID)
		}
	default:
		t.Fatal("retried task was not enqueued")
	}
}

func TestRetryRejectsQueuedOrRunningTask(t *testing.T) {
	output := t.TempDir()
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		state   string
		running bool
	}{
		{name: "queued", state: StateQueued},
		{name: "running", state: StateIncomplete, running: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{
				ID:        "abc12",
				InputPath: filepath.Join(t.TempDir(), "movie.mkv"),
				Input:     "movie.mkv",
				OutputDir: filepath.Join(output, tc.name, "abc12"),
				State:     tc.state,
				Params:    Params{},
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := writeTask(task); err != nil {
				t.Fatal(err)
			}
			cancels := map[string]context.CancelFunc{}
			if tc.running {
				_, cancel := context.WithCancel(context.Background())
				defer cancel()
				cancels[task.ID] = cancel
			}
			store := &Store{
				cfg:     &config.Config{Output: filepath.Join(output, tc.name)},
				queue:   make(chan string, 1),
				cancels: cancels,
			}
			if _, err := store.Retry(context.Background(), task.ID); !errors.Is(err, ErrTaskActive) {
				t.Fatalf("Retry error = %v, want ErrTaskActive", err)
			}
		})
	}
}

func TestClaimQueuedPreventsDuplicateRun(t *testing.T) {
	output := t.TempDir()
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: filepath.Join(output, "abc12"),
		State:     StateQueued,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		cfg:     &config.Config{Output: output},
		cancels: map[string]context.CancelFunc{},
	}
	_, _, cancel, ok := store.claimQueued(task.ID)
	if !ok {
		t.Fatal("first claim failed")
	}
	defer cancel()
	if _, _, _, ok := store.claimQueued(task.ID); ok {
		t.Fatal("second claim succeeded while task was already running")
	}
	store.releaseRunning(task.ID)
}

func TestValidateTaskIDRejectsTraversal(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../escape", "nested/id", `nested\id`} {
		if err := validateTaskID(id); err == nil {
			t.Fatalf("validateTaskID(%q) succeeded, want error", id)
		}
	}
	if err := validateTaskID("abc12"); err != nil {
		t.Fatalf("validateTaskID(valid) = %v", err)
	}
}

func TestDeleteRemovesTaskFolderAndCache(t *testing.T) {
	output := t.TempDir()
	taskDir := filepath.Join(output, "abc12")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: taskDir,
		State:     StateComplete,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	store := &Store{
		cfg:     &config.Config{Output: output},
		cancels: map[string]context.CancelFunc{},
		cache:   []Task{compactTask(*task)},
	}
	if err := store.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("task dir still exists, stat error = %v", err)
	}
	tasks, err := store.List(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("cache has %d tasks, want 0", len(tasks))
	}
}

func TestDeleteRejectsRunningTask(t *testing.T) {
	output := t.TempDir()
	taskDir := filepath.Join(output, "abc12")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	task := &Task{
		ID:        "abc12",
		Input:     "movie.mkv",
		OutputDir: taskDir,
		State:     StateRunning,
		Params:    Params{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := writeTask(task); err != nil {
		t.Fatal(err)
	}
	store := &Store{cfg: &config.Config{Output: output}, cancels: map[string]context.CancelFunc{}}
	if err := store.Delete(task.ID); !errors.Is(err, ErrTaskRunning) {
		t.Fatalf("Delete error = %v, want ErrTaskRunning", err)
	}
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatalf("task dir missing after rejected delete: %v", err)
	}
}

func TestCompactTaskIncludesSubtitleLanguages(t *testing.T) {
	task := Task{
		ID:    "abc12",
		State: StateComplete,
		Streams: []Stream{
			{CodecType: subtitleType, Language: "jpn"},
			{CodecType: subtitleType, Language: ""},
			{CodecType: subtitleType, Language: "eng"},
			{CodecType: audioType, Language: "eng"},
		},
	}
	got := compactTask(task)
	want := []string{"eng", "jpn", "und"}
	if len(got.SubtitleLangs) != len(want) {
		t.Fatalf("subtitle languages = %+v, want %+v", got.SubtitleLangs, want)
	}
	for i := range want {
		if got.SubtitleLangs[i] != want[i] {
			t.Fatalf("subtitle languages = %+v, want %+v", got.SubtitleLangs, want)
		}
	}
	if got.Streams != nil {
		t.Fatalf("compact task kept streams: %+v", got.Streams)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

type fakeExec struct{}

var _ executil.Runner = fakeExec{}

func (fakeExec) Run(context.Context, string, ...string) ([]byte, error) {
	return []byte(`{"chapters":[],"streams":[]}`), nil
}

type cancelStreamProbeExec struct{}

var _ executil.Runner = cancelStreamProbeExec{}

func (cancelStreamProbeExec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	for _, arg := range args {
		if arg == "-show_streams" {
			return nil, context.Canceled
		}
	}
	return []byte(`{"chapters":[],"streams":[]}`), nil
}

func testItem(path string) media.Item {
	stat, _ := os.Stat(path)
	return media.Item{
		ID:       "media-id",
		Path:     path,
		RelPath:  "Movies/Example/Example.mkv",
		FileName: filepath.Base(path),
		Size:     stat.Size(),
		ModTime:  stat.ModTime(),
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
