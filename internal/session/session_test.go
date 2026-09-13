package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fikua/fikua-starflux/internal/ui"
)

func TestSaveLoad_roundTrip(t *testing.T) {
	stars := []ui.Star{
		{Name: "VAR-1", Role: ui.RoleTarget, X: 100.5, Y: 200.25},
		{Name: "CONTROL", Role: ui.RoleComparison, X: 50, Y: 60},
		{Name: "CHK-1", Role: ui.RoleCheck, X: 10, Y: 20},
	}

	path := filepath.Join(t.TempDir(), "stars.json")
	if err := Save(stars, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(got, stars) {
		t.Errorf("Load() = %+v, want %+v", got, stars)
	}
}

func TestSaveLoad_emptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := Save(nil, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load() = %+v, want empty", got)
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

func TestSave_unwritablePath(t *testing.T) {
	// A directory that doesn't exist can't be written into.
	path := filepath.Join(t.TempDir(), "does-not-exist", "stars.json")
	if err := Save(nil, path); err == nil {
		t.Fatal("expected an error when the target directory doesn't exist")
	}
}
