package task

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/executil"
)

func TestGenerateSpritesSeeksBeforeFilteringAndCapsVTT(t *testing.T) {
	outputDir := t.TempDir()
	exec := &spriteRecordingExec{}
	runner := NewRunner(&config.Config{
		Ffmpeg:                 "ffmpeg",
		ThumbnailHeight:        200,
		ThumbnailInterval:      2,
		ThumbnailChunkInterval: 800,
	}, nil, exec)
	task := &Task{
		OutputDir: outputDir,
		Duration:  801.25,
		Width:     1920,
		Height:    1080,
	}

	if err := runner.generateSprites(context.Background(), task, "video.mp4"); err != nil {
		t.Fatal(err)
	}

	calls := exec.Calls()
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].args[len(calls[i].args)-1] < calls[j].args[len(calls[j].args)-1]
	})
	if len(calls) != 2 {
		t.Fatalf("ffmpeg calls = %d, want 2: %+v", len(calls), calls)
	}

	wantFirst := []string{"-y", "-ss", "0", "-t", "800.000", "-i", "video.mp4", "-vf", "setpts=PTS-STARTPTS,fps=1/2:start_time=0:eof_action=pass,scale=356:200,tile=20x20:nb_frames=400,format=yuvj420p", "-frames:v", "1", filepath.Join(outputDir, "sp_1.jpg")}
	if !reflect.DeepEqual(calls[0].args, wantFirst) {
		t.Fatalf("first sprite args = %+v, want %+v", calls[0].args, wantFirst)
	}
	wantSecond := []string{"-y", "-ss", "800", "-t", "1.250", "-i", "video.mp4", "-vf", "setpts=PTS-STARTPTS,fps=1/2:start_time=0:eof_action=pass,scale=356:200,tile=20x20:nb_frames=400,format=yuvj420p", "-frames:v", "1", filepath.Join(outputDir, "sp_2.jpg")}
	if !reflect.DeepEqual(calls[1].args, wantSecond) {
		t.Fatalf("second sprite args = %+v, want %+v", calls[1].args, wantSecond)
	}

	vtt := string(mustReadFile(t, filepath.Join(outputDir, ThumbnailVTT)))
	if got := strings.Count(vtt, "sp_1.jpg#xywh="); got != 400 {
		t.Fatalf("sp_1 refs = %d, want 400", got)
	}
	if got := strings.Count(vtt, "sp_2.jpg#xywh="); got != 1 {
		t.Fatalf("sp_2 refs = %d, want 1", got)
	}
	if !strings.Contains(vtt, "00:13:20.000 --> 00:13:21.250\nsp_2.jpg#xywh=0,0,356,200") {
		t.Fatalf("vtt missing capped final cue:\n%s", vtt)
	}
	if strings.Contains(vtt, "00:13:22.000") {
		t.Fatalf("vtt includes a cue past the media duration:\n%s", vtt)
	}
}

type spriteRecordingExec struct {
	mu    sync.Mutex
	calls []spriteExecCall
}

var _ executil.Runner = (*spriteRecordingExec)(nil)

type spriteExecCall struct {
	name string
	args []string
}

func (e *spriteRecordingExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, spriteExecCall{name: name, args: append([]string(nil), args...)})
	return nil, nil
}

func (e *spriteRecordingExec) Calls() []spriteExecCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	calls := make([]spriteExecCall, len(e.calls))
	copy(calls, e.calls)
	return calls
}
