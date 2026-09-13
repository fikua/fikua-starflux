package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fikua/fikua-starflux/internal/photometry"
)

func TestDefault(t *testing.T) {
	want := Config{
		CentroidHalfWidth: 10,
		Aperture:          photometry.Aperture{R: 6, RIn: 10, ROut: 15},
		ToleranceLevel:    7,
	}
	if got := Default(); got != want {
		t.Errorf("Default() = %+v, want %+v", got, want)
	}
}

func TestSaveLoad_roundTrip(t *testing.T) {
	cfg := Config{
		CentroidHalfWidth: 12,
		Aperture:          photometry.Aperture{R: 5, RIn: 9, ROut: 14},
		ToleranceLevel:    3,
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != cfg {
		t.Errorf("Load() = %+v, want %+v", got, cfg)
	}
}

func TestLoad_missingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestLoad_malformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write bad.json: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}

func TestLoadOrDefault_fallsBackWhenPathUnavailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	// Even if resolution weirdness occurs, LoadOrDefault must never panic
	// or return a zero Config — it should always yield a usable value.
	got := LoadOrDefault()
	if got.CentroidHalfWidth <= 0 {
		t.Errorf("LoadOrDefault() = %+v, want a usable non-zero config", got)
	}
}

func TestSave_unwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "config.json")
	if err := Save(Default(), path); err == nil {
		t.Fatal("expected an error when the target directory doesn't exist")
	}
}
