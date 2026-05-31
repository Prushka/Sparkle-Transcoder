package media

import (
	"path/filepath"
	"testing"
)

func TestParseEpisodeItem(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	path := filepath.Join(root, "TV-Shows", "Siren (2018)", "Season 2", "Siren (2018) - S02E04 - Oil and Water WEBDL-1080p.mkv")

	item, err := parseItem(root, path, 123, 456)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindEpisode {
		t.Fatalf("kind = %s, want %s", item.Kind, KindEpisode)
	}
	if item.Show != "Siren (2018)" {
		t.Fatalf("show = %q", item.Show)
	}
	if item.Season != 2 || item.Episode != 4 {
		t.Fatalf("season/episode = %d/%d", item.Season, item.Episode)
	}
	if item.Title != "Oil and Water WEBDL-1080p" {
		t.Fatalf("title = %q", item.Title)
	}
}

func TestParseEpisodeItemKeepsShowAndTitlePeriods(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	path := filepath.Join(root, "Anime", "Dr. Stone", "Season 4", "Dr. STONE - S04E04 - Dr. X Bluray-1080p Remux.mkv")

	item, err := parseItem(root, path, 123, 456)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindEpisode {
		t.Fatalf("kind = %s, want %s", item.Kind, KindEpisode)
	}
	if item.Show != "Dr. Stone" {
		t.Fatalf("show = %q", item.Show)
	}
	if item.Title != "Dr. X Bluray-1080p Remux" {
		t.Fatalf("title = %q", item.Title)
	}
}

func TestParseMovieItem(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	path := filepath.Join(root, "Movies", "Dune Part Two (2024)", "Dune Part Two (2024) Bluray-2160p.mkv")

	item, err := parseItem(root, path, 123, 456)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindMovie {
		t.Fatalf("kind = %s, want %s", item.Kind, KindMovie)
	}
	if item.Title != "Dune Part Two (2024)" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.Year != "2024" {
		t.Fatalf("year = %q", item.Year)
	}
}

func TestParseMovieItemKeepsDirectoryPeriods(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	path := filepath.Join(root, "Movies", "Dr. Strangelove (1964)", "Dr. Strangelove (1964) Bluray-1080p.mkv")

	item, err := parseItem(root, path, 123, 456)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindMovie {
		t.Fatalf("kind = %s, want %s", item.Kind, KindMovie)
	}
	if item.Title != "Dr. Strangelove (1964)" {
		t.Fatalf("title = %q", item.Title)
	}
	if item.Year != "1964" {
		t.Fatalf("year = %q", item.Year)
	}
}

func TestParseUnknownItemCleansDotSeparatedFileTitle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	path := filepath.Join(root, "Uploads", "Some.Show.Name.2024.WEBDL-1080p.mkv")

	item, err := parseItem(root, path, 123, 456)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindUnknown {
		t.Fatalf("kind = %s, want %s", item.Kind, KindUnknown)
	}
	if item.Title != "Some Show Name 2024 WEBDL-1080p" {
		t.Fatalf("title = %q", item.Title)
	}
}
