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
