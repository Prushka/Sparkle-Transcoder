package task

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sparkle-transcoder/internal/config"
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
