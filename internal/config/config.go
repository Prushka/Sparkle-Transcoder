package config

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env"
)

type Config struct {
	Debug bool `env:"DEBUG" envDefault:"false"`

	Addr      string `env:"API_ADDR" envDefault:":1323"`
	MediaRoot string `env:"MEDIA_ROOT" envDefault:"/Volumes/media/Managed-Videos"`
	Output    string `env:"OUTPUT" envDefault:"/Volumes/media/Managed-Videos/Public/output"`
	DataDir   string `env:"DATA_DIR" envDefault:"./.sparkle-transcoder"`

	ScanCacheFile    string        `env:"SCAN_CACHE_FILE" envDefault:""`
	IncrementalScan  bool          `env:"SCAN_INCREMENTAL" envDefault:"true"`
	ScanOnStartup    bool          `env:"SCAN_ON_STARTUP" envDefault:"true"`
	ScanInterval     time.Duration `env:"SCAN_INPUT_INTERVAL" envDefault:"4h"`
	MediaLibraries   []string      `env:"MEDIA_LIBRARIES" envDefault:"Anime,Anime-R,Movies,Movies-R,TV-Shows,TV-Shows-R,Public/input"`
	MediaExcludeDirs []string      `env:"MEDIA_EXCLUDE_DIRS" envDefault:"Public/output,Public/temp,.Trashes,.Spotlight-V100,.fseventsd"`

	Ffmpeg       string `env:"FFMPEG" envDefault:"ffmpeg"`
	Ffprobe      string `env:"FFPROBE" envDefault:"ffprobe"`
	MKVExtract   string `env:"MKVEXTRACT" envDefault:"mkvextract"`
	HandbrakeCli string `env:"HANDBRAKE_CLI" envDefault:"HandBrakeCLI"`

	ConstantQuality  string `env:"CONSTANT_QUALITY" envDefault:"18"`
	VideoExt         string `env:"VIDEO_EXT" envDefault:"mp4"`
	Encoder          string `env:"ENCODER" envDefault:"av1,hevc"`
	AudioKbps        int    `env:"AUDIO_KBPS" envDefault:"144"`
	Av1Encoder       string `env:"SVT_AV1_ENCODER" envDefault:"svt_av1_10bit"`
	Av1Preset        string `env:"AV1_PRESET" envDefault:"4"`
	HevcEncoder      string `env:"HEVC_ENCODER" envDefault:"nvenc_h265_10bit"`
	HevcPreset       string `env:"HEVC_PRESET" envDefault:"slowest"`
	H26410BitEncoder string `env:"H264_10BIT_ENCODER" envDefault:"x264_10bit"`
	H26410BitPreset  string `env:"H264_10BIT_PRESET" envDefault:"slow"`
	H2648BitEncoder  string `env:"H264_ENCODER" envDefault:"x264"`
	H2648BitPreset   string `env:"H264_PRESET" envDefault:"slow"`
	H2648BitProfile  string `env:"H264_PROFILE" envDefault:"baseline"`
	H2648BitTune     string `env:"H264_TUNE" envDefault:"fastdecode"`

	ThumbnailHeight        int `env:"THUMBNAIL_HEIGHT" envDefault:"320"`
	ThumbnailInterval      int `env:"THUMBNAIL_INTERVAL" envDefault:"2"`
	ThumbnailChunkInterval int `env:"THUMBNAIL_CHUNK_INTERVAL" envDefault:"1152"`

	EnableEncode      bool          `env:"ENABLE_ENCODE" envDefault:"true"`
	EnableSprite      bool          `env:"ENABLE_SPRITE" envDefault:"true"`
	EnableLowPriority bool          `env:"ENABLE_LOW_PRIORITY" envDefault:"true"`
	ComputeSHA256     bool          `env:"COMPUTE_SHA256" envDefault:"false"`
	TaskConcurrency   int           `env:"TASK_CONCURRENCY" envDefault:"1"`
	TaskRefreshWindow time.Duration `env:"TASK_REFRESH_WINDOW" envDefault:"5m"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	cfg.MediaRoot = filepath.Clean(cfg.MediaRoot)
	cfg.Output = filepath.Clean(cfg.Output)
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	if cfg.ScanCacheFile == "" {
		cfg.ScanCacheFile = filepath.Join(cfg.DataDir, "scan-cache.json")
	}
	if cfg.TaskConcurrency < 1 {
		cfg.TaskConcurrency = 1
	}
	cfg.VideoExt = strings.TrimPrefix(cfg.VideoExt, ".")
	return cfg, nil
}

func (c *Config) Encoders() []string {
	parts := strings.Split(c.Encoder, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
