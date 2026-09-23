package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func isolatedConfig(t *testing.T) string {
	t.Helper()
	fields := reflect.TypeFor[Config]()
	for i := 0; i < fields.NumField(); i++ {
		name := strings.Split(fields.Field(i).Tag.Get("env"), ",")[0]
		if name != "" {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func TestLoadPortableDefaults(t *testing.T) {
	dir := isolatedConfig(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "media"), filepath.Join(dir, "output"),
		filepath.Join(dir, ".sparkle-transcoder"),
		filepath.Join(dir, ".sparkle-transcoder", "scan-cache.json"),
	}
	got := []string{cfg.MediaRoot, cfg.Output, cfg.DataDir, cfg.ScanCacheFile}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default paths = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(cfg.MediaLibraries, []string{"Movies", "TV-Shows"}) {
		t.Fatalf("default libraries = %v", cfg.MediaLibraries)
	}
}

func TestLoadResolvesOverridesBeforeWorkingDirectoryChanges(t *testing.T) {
	dir := isolatedConfig(t)
	media := t.TempDir()
	t.Setenv("MEDIA_ROOT", media)
	t.Setenv("OUTPUT", "custom-output")
	t.Setenv("DATA_DIR", "custom-data")
	t.Setenv("SCAN_CACHE_FILE", filepath.Join("cache", "index.json"))
	t.Setenv("MEDIA_LIBRARIES", "Clips,Films")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{media, filepath.Join(dir, "custom-output"), filepath.Join(dir, "custom-data"), filepath.Join(dir, "cache", "index.json")}
	got := []string{cfg.MediaRoot, cfg.Output, cfg.DataDir, cfg.ScanCacheFile}
	t.Chdir(t.TempDir())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configured paths = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(cfg.MediaLibraries, []string{"Clips", "Films"}) {
		t.Fatalf("configured libraries = %v", cfg.MediaLibraries)
	}
}
