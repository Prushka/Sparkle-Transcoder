package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sparkle-transcoder/internal/config"
)

func TestScannerFullScanCollectsRelatedFiles(t *testing.T) {
	root := testMediaRoot(t)
	video := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.mkv")
	writeFile(t, video, "video")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "poster.jpg"), "poster")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.en.srt"), "subtitle")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.nfo"), "nfo")

	scanner := NewScanner(testConfig(t, root))
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(idx.Items))
	}
	item := onlyItem(idx)
	stat, err := os.Stat(video)
	if err != nil {
		t.Fatal(err)
	}
	wantCreatedAt := fileCreationTime(video, stat)
	if !item.CreatedAt.Equal(wantCreatedAt) {
		t.Fatalf("creation time = %s, want %s", item.CreatedAt, wantCreatedAt)
	}
	if item.Poster == nil {
		t.Fatal("poster was not collected")
	}
	if len(item.Subtitles) != 1 {
		t.Fatalf("subtitles = %d, want 1", len(item.Subtitles))
	}
	if len(item.NFO) == 0 {
		t.Fatal("nfo was not collected")
	}
}

func TestScannerIncrementalAddsNewVideoAndRefreshesModifiedFile(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	first := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.mkv")
	second := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E02 - Next.mkv")
	writeFile(t, first, "video")

	scanner := NewScanner(cfg)
	if _, err := scanner.Scan(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	writeFile(t, second, "video")
	idx, err := scanner.Scan(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 2 {
		t.Fatalf("items after add = %d, want 2", len(idx.Items))
	}

	var firstID string
	for _, item := range idx.Items {
		if item.FileName == filepath.Base(first) {
			firstID = item.ID
		}
	}
	time.Sleep(10 * time.Millisecond)
	writeFile(t, first, "video with more bytes")
	idx, err = scanner.Scan(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.Items[firstID].Size; got != int64(len("video with more bytes")) {
		t.Fatalf("modified size = %d", got)
	}
}

func TestScannerIncrementalRefreshesModifiedRelatedFile(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	video := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.mkv")
	subtitle := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.en.srt")
	writeFile(t, video, "video")
	writeFile(t, subtitle, "old")

	scanner := NewScanner(cfg)
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	item := onlyItem(idx)
	if len(item.Subtitles) != 1 {
		t.Fatalf("subtitles = %d, want 1", len(item.Subtitles))
	}
	time.Sleep(10 * time.Millisecond)
	writeFile(t, subtitle, "updated subtitle")

	idx, err = scanner.Scan(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	item = idx.Items[item.ID]
	if got := item.Subtitles[0].Size; got != int64(len("updated subtitle")) {
		t.Fatalf("subtitle size = %d, want %d", got, len("updated subtitle"))
	}
}

func TestScannerLoadCacheRestoresLastFinishedAt(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	video := filepath.Join(root, "Movies", "Example Movie (2026)", "Example Movie (2026).mkv")
	writeFile(t, video, "video")

	scanner := NewScanner(cfg)
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}

	cached := NewScanner(cfg)
	if err := cached.LoadCache(); err != nil {
		t.Fatal(err)
	}
	status := cached.Status()
	if status.LastFinishedAt == nil {
		t.Fatal("last finished time was not restored from cache")
	}
	if got, want := status.LastFinishedAt.Format(time.RFC3339Nano), idx.GeneratedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("last finished time = %s, want %s", got, want)
	}
	if status.Items != 1 {
		t.Fatalf("items = %d, want 1", status.Items)
	}
}

func TestScannerStatusKeepsRunningItemProgress(t *testing.T) {
	root := testMediaRoot(t)
	scanner := NewScanner(testConfig(t, root))
	scanner.mu.Lock()
	scanner.index = NewIndex(root)
	scanner.status.Running = true
	scanner.status.Items = 42
	scanner.mu.Unlock()

	status := scanner.Status()
	if status.Items != 42 {
		t.Fatalf("items = %d, want live running progress", status.Items)
	}
}

func TestScannerFullScansWhenScanConfigChanges(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	cfg.MediaLibraries = []string{"Movies"}
	movie := filepath.Join(root, "Movies", "Example Movie (2026)", "Example Movie (2026).mkv")
	episode := filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.mkv")
	writeFile(t, movie, "movie")
	writeFile(t, episode, "episode")

	scanner := NewScanner(cfg)
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 1 {
		t.Fatalf("initial items = %d, want 1", len(idx.Items))
	}

	cfg.MediaLibraries = []string{"Movies", "TV-Shows"}
	idx, err = scanner.Scan(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 2 {
		t.Fatalf("items after scan config change = %d, want full rescan with 2 items", len(idx.Items))
	}
}

func TestScannerFullScansWhenCacheVersionChanges(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	video := filepath.Join(root, "Anime", "Dr. Stone", "Season 1", "Dr. STONE - S01E01 - STONE WORLD WEBDL-2160p.mkv")
	writeFile(t, video, "video")

	scanner := NewScanner(cfg)
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	item := onlyItem(idx)
	item.Show = "Dr"
	idx.Items[item.ID] = item
	idx.Version = currentIndexVersion - 1
	content, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ScanCacheFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	idx, err = scanner.Scan(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	item = onlyItem(idx)
	if idx.Version != currentIndexVersion {
		t.Fatalf("version = %d, want %d", idx.Version, currentIndexVersion)
	}
	if item.Show != "Dr. Stone" {
		t.Fatalf("show = %q", item.Show)
	}
}

func TestScannerMarksDuplicateMoviesAndEpisodes(t *testing.T) {
	root := testMediaRoot(t)
	cfg := testConfig(t, root)
	writeFile(t, filepath.Join(root, "Movies", "Example Movie (2026)", "Example Movie (2026).mkv"), "movie")
	writeFile(t, filepath.Join(root, "Movies", "Example Movie (2026) Remux", "Example Movie (2026) Remux.mkv"), "movie")
	writeFile(t, filepath.Join(root, "Movies", "Unique Movie (2026)", "Unique Movie (2026).mkv"), "movie")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot.mkv"), "episode")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E01 - Pilot 4K.mkv"), "episode")
	writeFile(t, filepath.Join(root, "TV-Shows", "Example Show", "Season 1", "Example Show - S01E02 - Next.mkv"), "episode")

	scanner := NewScanner(cfg)
	idx, err := scanner.Scan(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}

	duplicates := map[string]bool{}
	for _, item := range idx.Items {
		duplicates[item.FileName] = item.HasDuplicate
	}
	for _, name := range []string{"Example Movie (2026).mkv", "Example Movie (2026) Remux.mkv", "Example Show - S01E01 - Pilot.mkv", "Example Show - S01E01 - Pilot 4K.mkv"} {
		if !duplicates[name] {
			t.Fatalf("%s was not marked duplicate; duplicates = %+v", name, duplicates)
		}
	}
	for _, name := range []string{"Unique Movie (2026).mkv", "Example Show - S01E02 - Next.mkv"} {
		if duplicates[name] {
			t.Fatalf("%s was marked duplicate; duplicates = %+v", name, duplicates)
		}
	}
}

func testMediaRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Managed-Videos")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func testConfig(t *testing.T, root string) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	return &config.Config{
		MediaRoot:        root,
		Output:           filepath.Join(root, "Public", "output"),
		DataDir:          tmp,
		ScanCacheFile:    filepath.Join(tmp, "scan-cache.json"),
		IncrementalScan:  true,
		MediaLibraries:   []string{"TV-Shows", "Movies"},
		MediaExcludeDirs: []string{"Public/output"},
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func onlyItem(idx *Index) Item {
	for _, item := range idx.Items {
		return item
	}
	return Item{}
}
