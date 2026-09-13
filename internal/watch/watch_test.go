package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestFindNewFiles_findsUnseenFITSFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.fits")
	writeFile(t, dir, "b.fit")
	writeFile(t, dir, "notes.txt") // not a FITS file, must be ignored

	got, err := FindNewFiles(dir, map[string]bool{a: true})
	if err != nil {
		t.Fatalf("FindNewFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d new files, want 1: %v", len(got), got)
	}
	if filepath.Base(got[0]) != "b.fit" {
		t.Errorf("got %v, want [.../b.fit]", got)
	}
}

func TestFindNewFiles_noneNewWhenAllSeen(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.fits")

	got, err := FindNewFiles(dir, map[string]bool{a: true})
	if err != nil {
		t.Fatalf("FindNewFiles: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d new files, want 0: %v", len(got), got)
	}
}

func TestFindNewFiles_sortedByName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c.fits")
	writeFile(t, dir, "a.fits")
	writeFile(t, dir, "b.fits")

	got, err := FindNewFiles(dir, nil)
	if err != nil {
		t.Fatalf("FindNewFiles: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d files, want 3", len(got))
	}
	want := []string{"a.fits", "b.fits", "c.fits"}
	for i, w := range want {
		if filepath.Base(got[i]) != w {
			t.Errorf("got[%d] = %v, want %v", i, filepath.Base(got[i]), w)
		}
	}
}

func TestFindNewFiles_ignoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.fits")
	if err := os.Mkdir(filepath.Join(dir, "subdir.fits"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := FindNewFiles(dir, nil)
	if err != nil {
		t.Fatalf("FindNewFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d files, want 1 (subdirectory should be ignored): %v", len(got), got)
	}
}

func TestFindNewFiles_missingDir(t *testing.T) {
	if _, err := FindNewFiles(filepath.Join(t.TempDir(), "does-not-exist"), nil); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
