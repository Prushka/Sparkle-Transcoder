package media

import (
	"os"
	"time"
)

var fallbackFileCreationTime = time.Date(2019, time.January, 1, 0, 0, 0, 0, time.UTC)

func fileCreationTime(path string, info os.FileInfo) time.Time {
	createdAt, ok := readFileCreationTime(path, info)
	if !ok {
		return fallbackFileCreationTime
	}
	return normalizeFileCreationTime(createdAt, time.Now().UTC())
}

func normalizeFileCreationTime(createdAt, now time.Time) time.Time {
	if createdAt.IsZero() || createdAt.After(now) {
		return fallbackFileCreationTime
	}
	return createdAt.UTC()
}
