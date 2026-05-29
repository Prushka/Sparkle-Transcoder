package media

import "time"

type Kind string

const (
	KindMovie   Kind = "movie"
	KindEpisode Kind = "episode"
	KindUnknown Kind = "unknown"
)

type RelatedKind string

const (
	RelatedPoster     RelatedKind = "poster"
	RelatedFanart     RelatedKind = "fanart"
	RelatedSubtitle   RelatedKind = "subtitle"
	RelatedNFO        RelatedKind = "nfo"
	RelatedAttachment RelatedKind = "attachment"
)

type RelatedFile struct {
	Kind    RelatedKind `json:"kind"`
	Path    string      `json:"path"`
	RelPath string      `json:"relPath"`
	Name    string      `json:"name"`
	Ext     string      `json:"ext"`
	Size    int64       `json:"size"`
	ModTime time.Time   `json:"modTime"`
}

type Item struct {
	ID           string        `json:"id"`
	Kind         Kind          `json:"kind"`
	Library      string        `json:"library"`
	Title        string        `json:"title"`
	Show         string        `json:"show,omitempty"`
	Season       int           `json:"season,omitempty"`
	Episode      int           `json:"episode,omitempty"`
	EpisodeTitle string        `json:"episodeTitle,omitempty"`
	Year         string        `json:"year,omitempty"`
	Path         string        `json:"path"`
	RelPath      string        `json:"relPath"`
	Parent       string        `json:"parent"`
	FileName     string        `json:"fileName"`
	Ext          string        `json:"ext"`
	Size         int64         `json:"size"`
	ModTime      time.Time     `json:"modTime"`
	Poster       *RelatedFile  `json:"poster,omitempty"`
	Fanart       *RelatedFile  `json:"fanart,omitempty"`
	Subtitles    []RelatedFile `json:"subtitles,omitempty"`
	NFO          []RelatedFile `json:"nfo,omitempty"`
	Attachments  []RelatedFile `json:"attachments,omitempty"`
	SortKey      string        `json:"sortKey"`
}

type DirectoryState struct {
	RelPath string    `json:"relPath"`
	ModTime time.Time `json:"modTime"`
	Size    int64     `json:"size"`
}

type Index struct {
	Version     int                       `json:"version"`
	MediaRoot   string                    `json:"mediaRoot"`
	GeneratedAt time.Time                 `json:"generatedAt"`
	Items       map[string]Item           `json:"items"`
	Dirs        map[string]DirectoryState `json:"dirs"`
}

type ScanStatus struct {
	Running        bool       `json:"running"`
	Incremental    bool       `json:"incremental"`
	LastStartedAt  *time.Time `json:"lastStartedAt,omitempty"`
	LastFinishedAt *time.Time `json:"lastFinishedAt,omitempty"`
	Error          string     `json:"error,omitempty"`
	Items          int        `json:"items"`
	Changed        int        `json:"changed"`
}

func NewIndex(root string) *Index {
	return &Index{
		Version:   1,
		MediaRoot: root,
		Items:     map[string]Item{},
		Dirs:      map[string]DirectoryState{},
	}
}
