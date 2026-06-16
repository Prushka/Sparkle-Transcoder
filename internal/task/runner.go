package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/executil"
	"sparkle-transcoder/internal/media"

	"github.com/cenkalti/dominantcolor"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

const (
	subtitleType   = "subtitle"
	audioType      = "audio"
	attachmentType = "attachment"
	spritePrefix   = "sp"
	spriteExt      = ".jpg"
)

var imageSubtitleExt = map[string]string{
	"hdmv_pgs_subtitle": "sup",
}

type Runner struct {
	cfg      *config.Config
	scanner  *media.Scanner
	exec     executil.Runner
	updateMu sync.RWMutex
	onUpdate func(*Task)
}

func NewRunner(cfg *config.Config, scanner *media.Scanner, execRunner executil.Runner) *Runner {
	return &Runner{cfg: cfg, scanner: scanner, exec: execRunner}
}

func (r *Runner) SetUpdateHook(hook func(*Task)) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()
	r.onUpdate = hook
}

func (r *Runner) Run(ctx context.Context, task *Task) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	task.StartedAt = &now
	task.State = StateRunning
	task.UpdatedAt = now
	task.Error = ""
	if err := r.writeTask(task); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(task.OutputDir, 0755); err != nil {
		return err
	}
	task.State = StateIncomplete
	task.UpdatedAt = time.Now().UTC()
	if err := r.writeTask(task); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if task.Params.ExtractStreams == nil || *task.Params.ExtractStreams {
		if err := r.extractStreams(ctx, task, task.InputPath, subtitleType); err != nil {
			if err := optionalRunnerWarning(ctx, task, "subtitle extraction", err); err != nil {
				return err
			}
		}
		if err := r.extractExternalSubtitles(ctx, task); err != nil {
			return err
		}
	}
	if err := r.validateRequiredSubtitles(ctx, task); err != nil {
		return err
	}
	if task.Params.ExtractStreams == nil || *task.Params.ExtractStreams {
		if err := r.extractStreams(ctx, task, task.InputPath, attachmentType); err != nil {
			if err := optionalRunnerWarning(ctx, task, "attachment extraction", err); err != nil {
				return err
			}
		}
	}
	if r.cfg.ComputeSHA256 {
		sum, err := calculateSHA256(ctx, task.InputPath)
		if err != nil {
			return err
		}
		log.Infof("sha256 %s %s", task.Input, sum)
	}
	if err := r.copyRelatedFiles(ctx, task); err != nil {
		return err
	}
	if err := r.extractDominantColor(ctx, task); errors.Is(err, context.Canceled) {
		return err
	}
	if err := r.extractChapters(ctx, task); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	task.State = StateStreamsExtracted
	task.UpdatedAt = time.Now().UTC()
	if err := r.writeTask(task); err != nil {
		return err
	}

	if task.Params.EnableEncode == nil || *task.Params.EnableEncode {
		if task.Params.Fast {
			if err := r.ffmpegCopyOnly(ctx, task); err != nil {
				return err
			}
		} else if err := r.handbrakeTranscode(ctx, task); err != nil {
			return err
		}
		if len(task.EncodedCodecs) > 0 {
			if err := r.extractStreams(ctx, task, r.codecVideo(task, task.EncodedCodecs[0]), audioType); err != nil {
				if err := optionalRunnerWarning(ctx, task, "audio extraction", err); err != nil {
					return err
				}
			}
			if err := r.mapAudioTracks(ctx, task); err != nil {
				return err
			}
			if err := r.probe(ctx, task); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now = time.Now().UTC()
	task.State = StateComplete
	task.Running = false
	task.FinishedAt = &now
	task.UpdatedAt = now
	task.Files = collectFiles(task.OutputDir)
	return r.writeTask(task)
}

func optionalRunnerWarning(ctx context.Context, task *Task, label string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	log.Warnf("%s warning for %s: %v", label, task.Input, err)
	return nil
}

func taskHasSubtitleTracks(task *Task) bool {
	for _, stream := range task.Streams {
		if stream.CodecType == subtitleType {
			return true
		}
	}
	return false
}

func (r *Runner) validateRequiredSubtitles(ctx context.Context, task *Task) error {
	if task.Params.RequireSubtitles != nil && !*task.Params.RequireSubtitles {
		return nil
	}
	if taskHasSubtitleTracks(task) {
		return nil
	}
	if task.Params.ExtractStreams == nil || *task.Params.ExtractStreams {
		return fmt.Errorf("required subtitles not found for %s", task.Input)
	}
	if r.taskMediaHasExternalSubtitles(task) {
		return nil
	}
	hasSubtitles, err := r.sourceHasSubtitleTracks(ctx, task.InputPath)
	if err != nil {
		return err
	}
	if !hasSubtitles {
		return fmt.Errorf("required subtitles not found for %s", task.Input)
	}
	return nil
}

func (r *Runner) taskMediaHasExternalSubtitles(task *Task) bool {
	if task.Media != nil && len(task.Media.Subtitles) > 0 {
		return true
	}
	if r.scanner == nil || task.MediaID == "" {
		return false
	}
	item, ok := r.scanner.Get(task.MediaID)
	return ok && len(item.Subtitles) > 0
}

func (r *Runner) sourceHasSubtitleTracks(ctx context.Context, path string) (bool, error) {
	out, err := r.exec.Run(ctx, r.cfg.Ffprobe, "-v", "quiet", "-print_format", "json", "-show_streams", path)
	if err != nil {
		return false, err
	}
	var probe probeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return false, err
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == subtitleType {
			return true, nil
		}
	}
	return false, nil
}

func (r *Runner) fail(task *Task, err error) error {
	now := time.Now().UTC()
	task.UpdatedAt = now
	task.FinishedAt = &now
	task.Running = false
	if errors.Is(err, context.Canceled) {
		task.State = StateCanceled
		task.Error = "canceled"
	} else {
		task.State = StateFailed
		task.Error = err.Error()
	}
	task.Files = collectFiles(task.OutputDir)
	return r.writeTask(task)
}

func (r *Runner) extractChapters(ctx context.Context, task *Task) error {
	out, err := r.exec.Run(ctx, r.cfg.Ffprobe, "-v", "quiet", "-print_format", "json", "-show_chapters", task.InputPath)
	if err != nil {
		return err
	}
	var probe probeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return err
	}
	task.Chapters = probe.Chapters
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) extractStreams(ctx context.Context, task *Task, source string, streamType string) error {
	out, err := r.exec.Run(ctx, r.cfg.Ffprobe, "-v", "quiet", "-print_format", "json", "-show_streams", source)
	if err != nil {
		return err
	}
	var probe probeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return err
	}
	found := false
	for _, stream := range probe.Streams {
		if stream.CodecType != streamType {
			continue
		}
		found = true
		if err := r.extractOneStream(ctx, task, source, stream); err != nil {
			return err
		}
	}
	if !found && (streamType == audioType || streamType == subtitleType) {
		return fmt.Errorf("no %s streams found in %s", streamType, filepath.Base(source))
	}
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) extractOneStream(ctx context.Context, task *Task, source string, stream StreamInfo) error {
	lang := stream.Tags.Language
	if lang == "" {
		lang = "und"
	}
	id := fmt.Sprintf("%d-%s", stream.Index, safeName(lang))
	baseStream := Stream{
		CodecName: stream.CodecName,
		CodecType: stream.CodecType,
		Index:     stream.Index,
		Language:  stream.Tags.Language,
		Title:     stream.Tags.Title,
		Filename:  stream.Tags.Filename,
		MimeType:  stream.Tags.MimeType,
		Channels:  stream.Channels,
		Bitrate:   stream.Bitrate,
	}
	switch stream.CodecType {
	case subtitleType:
		if stream.CodecName == "dvd_subtitle" {
			// FFmpeg's .sub muxer is MicroDVD text; mkvextract preserves VobSub sidecars.
			filename := fmt.Sprintf("%s.sub", id)
			if _, err := r.exec.Run(ctx, firstNonEmpty(r.cfg.MKVExtract, "mkvextract"), "tracks", source, fmt.Sprintf("%d:%s", stream.Index, r.outputJoin(task, filename))); err != nil {
				return err
			}
			baseStream.Location = filename
			task.Streams = append(task.Streams, baseStream)
			return nil
		}
		if ext, ok := imageSubtitleExt[stream.CodecName]; ok {
			filename := fmt.Sprintf("%s.%s", id, ext)
			args := []string{"-y", "-i", source, "-map", fmt.Sprintf("0:%d", stream.Index), "-c:s", "copy", r.outputJoin(task, filename)}
			if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, args...); err != nil {
				return err
			}
			baseStream.Location = filename
			task.Streams = append(task.Streams, baseStream)
			return nil
		}
		ass := fmt.Sprintf("%s.ass", id)
		vtt := fmt.Sprintf("%s.vtt", id)
		if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", source, "-map", fmt.Sprintf("0:%d", stream.Index), "-c:s", "ass", r.outputJoin(task, ass)); err != nil {
			return err
		}
		if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", r.outputJoin(task, ass), "-c:s", "webvtt", r.outputJoin(task, vtt)); err != nil {
			return err
		}
		assStream := baseStream
		assStream.CodecName = "ass"
		assStream.Location = ass
		vttStream := baseStream
		vttStream.CodecName = "webvtt"
		vttStream.Location = vtt
		task.Streams = append(task.Streams, assStream, vttStream)
	case audioType:
		ext := safeName(stream.CodecName)
		if ext == "" {
			ext = "audio"
		}
		filename := fmt.Sprintf("%s.%s", id, ext)
		if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", source, "-map", fmt.Sprintf("0:%d", stream.Index), "-c:a", "copy", r.outputJoin(task, filename)); err != nil {
			return err
		}
		baseStream.Location = filename
		task.Streams = append(task.Streams, baseStream)
	case attachmentType:
		filename := safeName(stream.Tags.Filename)
		if filename == "" {
			filename = fmt.Sprintf("attachment-%d", stream.Index)
		}
		if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", fmt.Sprintf("-dump_attachment:%d", stream.Index), r.outputJoin(task, filename), "-i", source, "-t", "0", "-f", "null", "-"); err != nil {
			return err
		}
		baseStream.Location = filename
		task.Streams = append(task.Streams, baseStream)
	}
	return nil
}

func (r *Runner) extractExternalSubtitles(ctx context.Context, task *Task) error {
	if task.MediaID == "" {
		return nil
	}
	item, ok := r.scanner.Get(task.MediaID)
	if !ok {
		return nil
	}
	for _, sub := range item.Subtitles {
		if err := ctx.Err(); err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(sub.Name))
		dest := r.outputJoin(task, safeName(sub.Name))
		if _, err := copyFile(ctx, sub.Path, dest); err != nil {
			return err
		}
		stream := Stream{CodecType: subtitleType, CodecName: strings.TrimPrefix(ext, "."), Location: filepath.Base(dest), Language: languageFromSubtitleName(sub.Name)}
		task.Streams = append(task.Streams, stream)
		switch ext {
		case ".ass", ".ssa":
			vtt := strings.TrimSuffix(dest, filepath.Ext(dest)) + ".vtt"
			if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", dest, "-c:s", "webvtt", vtt); err != nil {
				return err
			}
			task.Streams = append(task.Streams, Stream{CodecType: subtitleType, CodecName: "webvtt", Location: filepath.Base(vtt), Language: stream.Language})
		case ".srt":
			ass := strings.TrimSuffix(dest, filepath.Ext(dest)) + ".ass"
			vtt := strings.TrimSuffix(dest, filepath.Ext(dest)) + ".vtt"
			if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", dest, "-c:s", "ass", ass); err != nil {
				return err
			}
			if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", dest, "-c:s", "webvtt", vtt); err != nil {
				return err
			}
			task.Streams = append(task.Streams,
				Stream{CodecType: subtitleType, CodecName: "ass", Location: filepath.Base(ass), Language: stream.Language},
				Stream{CodecType: subtitleType, CodecName: "webvtt", Location: filepath.Base(vtt), Language: stream.Language},
			)
		}
	}
	return nil
}

func (r *Runner) ffmpegCopyOnly(ctx context.Context, task *Task) error {
	outputFile := r.outputJoin(task, fmt.Sprintf("hevc.%s", task.Params.VideoExt))
	args := []string{
		"-y",
		"-i", task.InputPath,
		"-map", "0:v",
		"-c:v", "copy",
		"-map", "0:a?",
		"-c:a", "libopus",
		"-ac", "2",
		"-b:a", fmt.Sprintf("%dk", task.Params.AudioKbps),
		"-map", "-0:s",
		outputFile,
	}
	if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, args...); err != nil {
		return err
	}
	task.EncodedCodecs = appendUnique(task.EncodedCodecs, "hevc")
	task.EncodedExt = task.Params.VideoExt
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) handbrakeTranscode(ctx context.Context, task *Task) error {
	task.EncodedExt = task.Params.VideoExt
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	for _, encoder := range task.Params.Encoders {
		encoder := strings.TrimSpace(encoder)
		if encoder == "" {
			continue
		}
		encoderCmd, preset, profile, tune, err := r.encoderSettings(encoder)
		if err != nil {
			return err
		}
		g.Go(func() error {
			outputFile := r.outputJoin(task, fmt.Sprintf("%s.%s", encoder, task.Params.VideoExt))
			args := []string{
				"-i", task.InputPath,
				"-o", outputFile,
				"--encoder", encoderCmd,
				"--vfr",
				"--quality", task.Params.Quality,
				"--encoder-preset", preset,
				"--color-range", "auto",
				"--subtitle", "none",
				"--aencoder", "opus",
				"--ab", fmt.Sprintf("%d", task.Params.AudioKbps),
				"--audio-lang-list", "any",
				"--all-audio",
				"--optimize",
				"--mixdown", "stereo",
			}
			if profile != "" {
				args = append(args, "--encoder-profile", profile)
			}
			if tune != "" {
				args = append(args, "--encoder-tune", tune)
			}
			if _, err := r.exec.Run(ctx, r.cfg.HandbrakeCli, args...); err != nil {
				return err
			}
			mu.Lock()
			task.EncodedCodecs = appendUnique(task.EncodedCodecs, encoder)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) encoderSettings(encoder string) (cmd string, preset string, profile string, tune string, err error) {
	switch encoder {
	case "av1":
		return r.cfg.Av1Encoder, r.cfg.Av1Preset, "", "", nil
	case "hevc":
		return r.cfg.HevcEncoder, r.cfg.HevcPreset, "", "", nil
	case "h264-10bit":
		return r.cfg.H26410BitEncoder, r.cfg.H26410BitPreset, "", "", nil
	case "h264-8bit":
		return r.cfg.H2648BitEncoder, r.cfg.H2648BitPreset, r.cfg.H2648BitProfile, r.cfg.H2648BitTune, nil
	default:
		return "", "", "", "", fmt.Errorf("unsupported encoder %q", encoder)
	}
}

func (r *Runner) mapAudioTracks(ctx context.Context, task *Task) error {
	task.MappedAudio = map[string][]Stream{}
	for _, audio := range task.Streams {
		if audio.CodecType != audioType || audio.Location == "" {
			continue
		}
		for _, codec := range task.EncodedCodecs {
			id := fmt.Sprintf("%s-%d-%s", codec, audio.Index, safeName(firstNonEmpty(audio.Language, "und")))
			output := r.outputJoin(task, fmt.Sprintf("%s.%s", id, task.Params.VideoExt))
			if _, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-i", r.codecVideo(task, codec), "-i", r.outputJoin(task, audio.Location), "-map", "0:v", "-map", "1:a", "-c:v", "copy", "-c:a", "copy", "-shortest", output); err != nil {
				return err
			}
			task.MappedAudio[codec] = append(task.MappedAudio[codec], audio)
		}
	}
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) probe(ctx context.Context, task *Task) error {
	if len(task.EncodedCodecs) == 0 {
		return nil
	}
	videoFile := r.codecVideo(task, task.EncodedCodecs[0])
	out, err := r.exec.Run(ctx, r.cfg.Ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoFile)
	if err != nil {
		return err
	}
	task.Duration, _ = strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	out, err = r.exec.Run(ctx, r.cfg.Ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", videoFile)
	if err != nil {
		return err
	}
	if width, height, ok := parseProbeDimensions(out); ok {
		task.Width = width
		task.Height = height
	} else {
		log.Warnf("unable to parse video dimensions for %s from ffprobe output %q", task.Input, strings.TrimSpace(string(out)))
	}
	if task.Params.EnableSprites != nil && *task.Params.EnableSprites && task.Duration > 0 && task.Width > 0 && task.Height > 0 {
		if err := r.generateSprites(ctx, task, videoFile); err != nil {
			return err
		}
	}
	task.UpdatedAt = time.Now().UTC()
	return r.writeTask(task)
}

func (r *Runner) generateSprites(ctx context.Context, task *Task, videoFile string) error {
	thumbnailHeight := r.cfg.ThumbnailHeight
	thumbnailInterval := r.cfg.ThumbnailInterval
	chunkInterval := r.cfg.ThumbnailChunkInterval
	if thumbnailHeight <= 0 || thumbnailInterval <= 0 || chunkInterval <= 0 {
		return nil
	}
	numThumbnailsPerChunk := int(math.Ceil(float64(chunkInterval) / float64(thumbnailInterval)))
	if numThumbnailsPerChunk <= 0 {
		return nil
	}
	numChunks := int(math.Ceil(task.Duration / float64(chunkInterval)))
	aspectRatio := float64(task.Width) / float64(task.Height)
	thumbnailWidth := int(math.Round(float64(thumbnailHeight) * aspectRatio))
	gridSize := int(math.Ceil(math.Sqrt(float64(numThumbnailsPerChunk))))
	var vtt strings.Builder
	vtt.WriteString("WEBVTT\n\n")
	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < numChunks; i++ {
		i := i
		g.Go(func() error {
			chunkStartTime := i * chunkInterval
			chunkDuration := math.Min(float64(chunkInterval), task.Duration-float64(chunkStartTime))
			spriteFile := r.outputJoin(task, fmt.Sprintf("%s_%d%s", spritePrefix, i+1, spriteExt))
			_, err := r.exec.Run(ctx, r.cfg.Ffmpeg, "-y", "-ss", fmt.Sprintf("%d", chunkStartTime), "-t", formatFFmpegSeconds(chunkDuration), "-i", videoFile, "-vf", fmt.Sprintf("setpts=PTS-STARTPTS,fps=1/%d:start_time=0:eof_action=pass,scale=%d:%d,tile=%dx%d:nb_frames=%d,format=yuvj420p", thumbnailInterval, thumbnailWidth, thumbnailHeight, gridSize, gridSize, numThumbnailsPerChunk), "-frames:v", "1", spriteFile)
			return err
		})
		for j := 0; j < numThumbnailsPerChunk; j++ {
			thumbnailTime := float64(i*chunkInterval + j*thumbnailInterval)
			if thumbnailTime >= task.Duration {
				break
			}
			start := formatVTTTime(thumbnailTime)
			end := formatVTTTime(math.Min(thumbnailTime+float64(thumbnailInterval), task.Duration))
			row := j / gridSize
			col := j % gridSize
			coords := fmt.Sprintf("%d,%d,%d,%d", col*thumbnailWidth, row*thumbnailHeight, thumbnailWidth, thumbnailHeight)
			vtt.WriteString(fmt.Sprintf("%s --> %s\n%s_%d%s#xywh=%s\n\n", start, end, spritePrefix, i+1, spriteExt, coords))
		}
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return os.WriteFile(r.outputJoin(task, ThumbnailVTT), []byte(vtt.String()), 0644)
}

func parseProbeDimensions(out []byte) (int, int, bool) {
	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, 0, false
	}
	fields := strings.Split(line, "x")
	values := make([]int, 0, 2)
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil || value <= 0 {
			return 0, 0, false
		}
		values = append(values, value)
		if len(values) == 2 {
			return values[0], values[1], true
		}
	}
	return 0, 0, false
}

func (r *Runner) copyRelatedFiles(ctx context.Context, task *Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.MediaID == "" {
		return nil
	}
	item, ok := r.scanner.Get(task.MediaID)
	if !ok {
		return nil
	}
	copies := []struct {
		src  *media.RelatedFile
		name string
	}{
		{item.Poster, "poster.jpg"},
		{item.Fanart, "fanart" + filepath.Ext(nilSafeName(item.Fanart))},
	}
	for _, copyOp := range copies {
		if copyOp.src == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := copyOp.name
		if name == "fanart" {
			name = "fanart.jpg"
		}
		if _, err := copyFile(ctx, copyOp.src.Path, r.outputJoin(task, name)); err != nil {
			return err
		}
	}
	if len(item.NFO) > 0 {
		if _, err := copyFile(ctx, item.NFO[0].Path, r.outputJoin(task, "info.nfo")); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) extractDominantColor(ctx context.Context, task *Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(r.outputJoin(task, "poster.jpg"))
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	task.DominantColors = []string{dominantcolor.Hex(dominantcolor.Find(img))}
	return r.writeTask(task)
}

func (r *Runner) writeTask(task *Task) error {
	if err := writeTask(task); err != nil {
		return err
	}
	r.updateMu.RLock()
	hook := r.onUpdate
	r.updateMu.RUnlock()
	if hook != nil {
		hook(task)
	}
	return nil
}

func (r *Runner) codecVideo(task *Task, codec string) string {
	return r.outputJoin(task, fmt.Sprintf("%s.%s", codec, task.Params.VideoExt))
}

func (r *Runner) outputJoin(task *Task, parts ...string) string {
	return filepath.Join(append([]string{task.OutputDir}, parts...)...)
}

func copyFile(ctx context.Context, src, dst string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	stat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	if !stat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return 0, err
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	buf := make([]byte, 1024*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		nr, readErr := in.Read(buf)
		if nr > 0 {
			if err := ctx.Err(); err != nil {
				return written, err
			}
			nw, writeErr := out.Write(buf[:nr])
			written += int64(nw)
			if writeErr != nil {
				return written, writeErr
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func calculateSHA256(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			if _, err := hash.Write(buf[:n]); err != nil {
				return "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func safeName(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	return strings.Trim(name, "-.")
}

func nilSafeName(file *media.RelatedFile) string {
	if file == nil {
		return ".jpg"
	}
	return file.Name
}

func languageFromSubtitleName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(base, ".")
	if len(parts) > 1 {
		return safeName(parts[len(parts)-1])
	}
	return ""
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func formatFFmpegSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', 3, 64)
}

func formatVTTTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	milliseconds := int(math.Round(seconds * 1000))
	hour := milliseconds / 3600000
	milliseconds %= 3600000
	minute := milliseconds / 60000
	milliseconds %= 60000
	second := milliseconds / 1000
	millisecond := milliseconds % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hour, minute, second, millisecond)
}
