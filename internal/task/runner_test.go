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

func TestHandbrakeTranscodeRunsFromInputDirWithShortInputName(t *testing.T) {
	inputDir := filepath.Join(t.TempDir(),
		"WorldEnd - What are you doing at the end of the world! Are you busy! Will you save us!",
		"Season 1")
	input := filepath.Join(inputDir, "WorldEnd - What are you doing at the end of the world! Are you busy! Will you save us! - S01E02 - Late Autumn Night's Dream Bluray-1080p.mkv")
	outputDir := t.TempDir()
	exec := &spriteRecordingExec{}
	runner := NewRunner(&config.Config{
		HandbrakeCli: "HandBrakeCLI",
		Av1Encoder:   "svt_av1_10bit",
		Av1Preset:    "4",
	}, nil, exec)
	task := &Task{
		InputPath: input,
		OutputDir: outputDir,
		Params: Params{
			Encoders:  []string{"av1"},
			VideoExt:  "mp4",
			Quality:   "20",
			AudioKbps: 144,
		},
	}

	if err := runner.handbrakeTranscode(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	calls := exec.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].dir != inputDir {
		t.Fatalf("cwd = %q, want %q", calls[0].dir, inputDir)
	}
	if calls[0].name != "HandBrakeCLI" {
		t.Fatalf("command = %q, want HandBrakeCLI", calls[0].name)
	}
	wantInput := filepath.Base(input)
	if got := calls[0].args[1]; got != wantInput {
		t.Fatalf("input arg = %q, want %q", got, wantInput)
	}
	if filepath.IsAbs(calls[0].args[1]) {
		t.Fatalf("input arg should be a short filename, got %q", calls[0].args[1])
	}
	wantOutput := filepath.Join(outputDir, "av1.mp4")
	for i, arg := range calls[0].args {
		if arg == "-o" {
			if i+1 >= len(calls[0].args) || calls[0].args[i+1] != wantOutput {
				t.Fatalf("output arg after -o = %q, want %q", calls[0].args[i+1], wantOutput)
			}
			return
		}
	}
	t.Fatalf("args missing -o: %+v", calls[0].args)
}

func TestHandbrakeTranscodePrioritizesSubtitleLanguageBeforeFormat(t *testing.T) {
	inputDir := t.TempDir()
	input := filepath.Join(inputDir, "Example.mkv")
	outputDir := t.TempDir()
	exec := &spriteRecordingExec{}
	runner := NewRunner(&config.Config{
		HandbrakeCli: "HandBrakeCLI",
		Av1Encoder:   "svt_av1_10bit",
		Av1Preset:    "4",
	}, nil, exec)
	burnInSubtitles := true
	task := &Task{
		Input:     "Example.mkv",
		InputPath: input,
		OutputDir: outputDir,
		Params: Params{
			BurnInSubtitles: &burnInSubtitles,
			Encoders:        []string{"av1"},
			VideoExt:        "mp4",
			Quality:         "20",
			AudioKbps:       144,
		},
		Streams: []Stream{
			{CodecType: subtitleType, CodecName: "ass", Location: "4-rus.ass", Language: "rus"},
			{CodecType: subtitleType, CodecName: "webvtt", Location: "3-chi.vtt", Language: "chi"},
			{CodecType: subtitleType, CodecName: "subrip", Location: "2-eng.srt", Language: "eng"},
		},
	}

	if err := runner.handbrakeTranscode(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	calls := exec.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(calls), calls)
	}
	args := calls[0].args
	if !hasArgPair(args, "--srt-file", filepath.Join(outputDir, "2-eng.srt")) {
		t.Fatalf("args missing selected English SRT subtitle: %+v", args)
	}
	if !hasArgPair(args, "--srt-lang", "eng") || !hasArg(args, "--srt-burn=1") {
		t.Fatalf("args missing SRT burn settings: %+v", args)
	}
	if hasArg(args, "--subtitle") {
		for i, arg := range args {
			if arg == "--subtitle" && i+1 < len(args) && args[i+1] != "none" {
				t.Fatalf("unexpected subtitle selection args: %+v", args)
			}
		}
	}
}

func TestHandbrakeTranscodeConvertsVTTBurnInSubtitle(t *testing.T) {
	inputDir := t.TempDir()
	input := filepath.Join(inputDir, "Example.mkv")
	outputDir := t.TempDir()
	exec := &spriteRecordingExec{}
	runner := NewRunner(&config.Config{
		Ffmpeg:       "ffmpeg",
		HandbrakeCli: "HandBrakeCLI",
		Av1Encoder:   "svt_av1_10bit",
		Av1Preset:    "4",
	}, nil, exec)
	burnInSubtitles := true
	task := &Task{
		Input:     "Example.mkv",
		InputPath: input,
		OutputDir: outputDir,
		Params: Params{
			BurnInSubtitles: &burnInSubtitles,
			Encoders:        []string{"av1"},
			VideoExt:        "mp4",
			Quality:         "20",
			AudioKbps:       144,
		},
		Streams: []Stream{
			{CodecType: subtitleType, CodecName: "webvtt", Location: "2-eng.vtt"},
			{CodecType: subtitleType, CodecName: "subrip", Location: "3-eng.srt"},
		},
	}

	if err := runner.handbrakeTranscode(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	calls := exec.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2: %+v", len(calls), calls)
	}
	vttPath := filepath.Join(outputDir, "2-eng.vtt")
	assPath := filepath.Join(outputDir, "2-eng.burn.ass")
	if calls[0].name != "ffmpeg" || !reflect.DeepEqual(calls[0].args, []string{"-y", "-i", vttPath, "-c:s", "ass", assPath}) {
		t.Fatalf("vtt conversion call = %+v, want ffmpeg conversion", calls[0])
	}
	args := calls[1].args
	if !hasArgPair(args, "--ssa-file", assPath) || !hasArgPair(args, "--ssa-lang", "eng") || !hasArg(args, "--ssa-burn=1") {
		t.Fatalf("args missing converted VTT burn settings: %+v", args)
	}
}

func TestLanguageFromSubtitleNameSupportsDashCodes(t *testing.T) {
	cases := map[string]string{
		"2-eng.vtt":         "eng",
		"Example.en.srt":    "en",
		"Movie.zh-Hans.ass": "zh",
		"Dialog.vtt":        "",
	}
	for name, want := range cases {
		if got := languageFromSubtitleName(name); got != want {
			t.Fatalf("languageFromSubtitleName(%q) = %q, want %q", name, got, want)
		}
	}
}

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

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasArgPair(args []string, key string, value string) bool {
	for i, arg := range args {
		if arg == key && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

type spriteRecordingExec struct {
	mu    sync.Mutex
	calls []spriteExecCall
}

var _ executil.Runner = (*spriteRecordingExec)(nil)

type spriteExecCall struct {
	dir  string
	name string
	args []string
}

func (e *spriteRecordingExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, spriteExecCall{name: name, args: append([]string(nil), args...)})
	return nil, nil
}

func (e *spriteRecordingExec) RunInDir(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, spriteExecCall{dir: dir, name: name, args: append([]string(nil), args...)})
	return nil, nil
}

func (e *spriteRecordingExec) Calls() []spriteExecCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	calls := make([]spriteExecCall, len(e.calls))
	copy(calls, e.calls)
	return calls
}
