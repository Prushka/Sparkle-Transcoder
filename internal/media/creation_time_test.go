package media

import (
	"testing"
	"time"
)

func TestNormalizeFileCreationTimeUsesFallbackForFutureTime(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	got := normalizeFileCreationTime(now.Add(time.Second), now)
	if !got.Equal(fallbackFileCreationTime) {
		t.Fatalf("future creation time = %s, want %s", got, fallbackFileCreationTime)
	}
}

func TestNormalizeFileCreationTimePreservesPastTime(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour).In(time.FixedZone("test", 8*60*60))
	got := normalizeFileCreationTime(createdAt, now)
	if !got.Equal(createdAt) || got.Location() != time.UTC {
		t.Fatalf("creation time = %s (%s), want %s in UTC", got, got.Location(), createdAt)
	}
}
