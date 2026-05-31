package media

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	episodeRe = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`)
	seasonRe  = regexp.MustCompile(`(?i)^Season\s+(\d+)`)
	yearRe    = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
)

var videoExts = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".avi": {}, ".mov": {}, ".wmv": {}, ".flv": {},
	".webm": {}, ".m4v": {}, ".mpg": {}, ".mpeg": {}, ".ts": {}, ".vob": {},
	".3gp": {}, ".3g2": {},
}

var subtitleExts = map[string]struct{}{
	".srt": {}, ".ass": {}, ".ssa": {}, ".vtt": {}, ".sub": {}, ".idx": {}, ".sup": {},
}

var imageExts = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {},
}

func IsVideo(name string) bool {
	_, ok := videoExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func isSubtitle(name string) bool {
	_, ok := subtitleExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func isImage(name string) bool {
	_, ok := imageExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func StableID(relPath string) string {
	sum := sha1.Sum([]byte(filepath.ToSlash(relPath)))
	return hex.EncodeToString(sum[:])[:16]
}

// cleanTitle normalizes display names from path components; it must not treat dots as file extensions.
func cleanTitle(in string) string {
	in = strings.ReplaceAll(in, "_", " ")
	in = strings.Join(strings.Fields(in), " ")
	return strings.TrimSpace(in)
}

// cleanFileTitle also handles dot-separated file basenames from scene-style names.
func cleanFileTitle(in string) string {
	in = cleanTitle(in)
	if !strings.Contains(in, " ") {
		in = strings.ReplaceAll(in, ".", " ")
		in = strings.Join(strings.Fields(in), " ")
	}
	return strings.TrimSpace(in)
}

func splitEpisodeTitle(base string) string {
	match := episodeRe.FindStringIndex(base)
	if match == nil {
		return ""
	}
	rest := strings.TrimSpace(base[match[1]:])
	rest = strings.Trim(rest, " -")
	return cleanFileTitle(rest)
}

func parseItem(root, absPath string, size int64, modTimeNanos int64) (Item, error) {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return Item{}, err
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	fileName := filepath.Base(absPath)
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	item := Item{
		ID:       StableID(rel),
		Kind:     KindUnknown,
		Path:     absPath,
		RelPath:  rel,
		Parent:   filepath.Dir(absPath),
		FileName: fileName,
		Ext:      strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), "."),
		Size:     size,
		ModTime:  unixNanos(modTimeNanos),
		Title:    cleanFileTitle(base),
	}
	if len(parts) > 0 {
		item.Library = parts[0]
	}
	if m := episodeRe.FindStringSubmatch(fileName); len(m) == 3 {
		item.Kind = KindEpisode
		item.Season, _ = strconv.Atoi(m[1])
		item.Episode, _ = strconv.Atoi(m[2])
		item.EpisodeTitle = splitEpisodeTitle(base)
		item.Show = inferShow(parts)
		if item.EpisodeTitle != "" {
			item.Title = item.EpisodeTitle
		}
		item.SortKey = strings.ToLower(item.Library + "/" + item.Show + "/" + pad(item.Season) + "/" + pad(item.Episode) + "/" + item.FileName)
		return item, nil
	}
	if strings.Contains(strings.ToLower(item.Library), "movie") {
		item.Kind = KindMovie
		if len(parts) >= 2 {
			item.Title = cleanTitle(parts[len(parts)-2])
		}
		if y := yearRe.FindString(item.Title); y != "" {
			item.Year = y
		}
		item.SortKey = strings.ToLower(item.Library + "/" + item.Title + "/" + item.FileName)
		return item, nil
	}
	item.SortKey = strings.ToLower(item.Library + "/" + item.Title + "/" + item.FileName)
	return item, nil
}

func inferShow(parts []string) string {
	for i, part := range parts {
		if seasonRe.MatchString(part) || strings.EqualFold(part, "Specials") {
			if i > 0 {
				return cleanTitle(parts[i-1])
			}
			break
		}
	}
	if len(parts) >= 3 {
		return cleanTitle(parts[len(parts)-3])
	}
	if len(parts) >= 2 {
		return cleanTitle(parts[len(parts)-2])
	}
	return ""
}

func pad(v int) string {
	if v < 10 {
		return "00" + strconv.Itoa(v)
	}
	if v < 100 {
		return "0" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}
