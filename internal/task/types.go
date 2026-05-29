package task

import (
	"time"

	"sparkle-transcoder/internal/media"
)

const (
	StateQueued           = "queued"
	StateRunning          = "running"
	StateIncomplete       = "incomplete"
	StateStreamsExtracted = "streams_extracted"
	StateComplete         = "complete"
	StateFailed           = "failed"
	StateCanceled         = "canceled"

	JobFile      = "job.json"
	ThumbnailVTT = "storyboard.vtt"
)

type Params struct {
	Fast           bool     `json:"fast"`
	EnableEncode   *bool    `json:"enableEncode,omitempty"`
	EnableSprites  *bool    `json:"enableSprites,omitempty"`
	Encoders       []string `json:"encoders,omitempty"`
	VideoExt       string   `json:"videoExt,omitempty"`
	Quality        string   `json:"quality,omitempty"`
	AudioKbps      int      `json:"audioKbps,omitempty"`
	ExtractStreams *bool    `json:"extractStreams,omitempty"`
}

type Task struct {
	ID             string              `json:"id"`
	MediaID        string              `json:"mediaId,omitempty"`
	InputPath      string              `json:"inputPath"`
	InputRelPath   string              `json:"inputRelPath,omitempty"`
	InputParent    string              `json:"inputParent,omitempty"`
	Input          string              `json:"input"`
	OutputDir      string              `json:"outputDir"`
	State          string              `json:"state"`
	Running        bool                `json:"running,omitempty"`
	Error          string              `json:"error,omitempty"`
	Params         Params              `json:"params"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	StartedAt      *time.Time          `json:"startedAt,omitempty"`
	FinishedAt     *time.Time          `json:"finishedAt,omitempty"`
	EncodedCodecs  []string            `json:"encodedCodecs,omitempty"`
	SubtitleLangs  []string            `json:"subtitleLanguages,omitempty"`
	MappedAudio    map[string][]Stream `json:"mappedAudio,omitempty"`
	Streams        []Stream            `json:"streams,omitempty"`
	Duration       float64             `json:"duration,omitempty"`
	Width          int                 `json:"width,omitempty"`
	Height         int                 `json:"height,omitempty"`
	EncodedExt     string              `json:"encodedExt,omitempty"`
	Chapters       []Chapter           `json:"chapters,omitempty"`
	DominantColors []string            `json:"dominantColors,omitempty"`
	Files          map[string]int64    `json:"files,omitempty"`
	OriSize        int64               `json:"oriSize,omitempty"`
	OriModTime     int64               `json:"oriModTime,omitempty"`
	Legacy         bool                `json:"legacy,omitempty"`
	Media          *media.Item         `json:"media,omitempty"`
}

type Stream struct {
	Bitrate    int    `json:"bitrate,omitempty"`
	CodecName  string `json:"codecName,omitempty"`
	CodecType  string `json:"codecType,omitempty"`
	Index      int    `json:"index"`
	Location   string `json:"location,omitempty"`
	Language   string `json:"language,omitempty"`
	Title      string `json:"title,omitempty"`
	Filename   string `json:"filename,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
}

type Chapter struct {
	ID        int64                  `json:"id"`
	StartTime string                 `json:"start_time"`
	EndTime   string                 `json:"end_time"`
	Start     int                    `json:"start"`
	End       int                    `json:"end"`
	TimeBase  string                 `json:"time_base"`
	Tags      map[string]interface{} `json:"tags"`
}

type StreamInfo struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Channels  int    `json:"channels,omitempty"`
	Bitrate   int    `json:"bit_rate,string,omitempty"`
	Tags      struct {
		Language string `json:"language"`
		Title    string `json:"title"`
		Filename string `json:"filename"`
		MimeType string `json:"mimetype"`
	}
}

type probeOutput struct {
	Streams  []StreamInfo `json:"streams"`
	Chapters []Chapter    `json:"chapters"`
}

type CreateRequest struct {
	MediaID string `json:"mediaId"`
	Params  Params `json:"params"`
}

type ListFilter struct {
	State string
}

type ListStatus struct {
	Refreshing  bool       `json:"refreshing"`
	RefreshedAt *time.Time `json:"refreshedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}
