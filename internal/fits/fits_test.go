package fits

import (
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/astrogo/fitsio"
)

func writeTestFITS(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()

	file, err := fitsio.Create(f)
	if err != nil {
		t.Fatalf("fitsio.Create: %v", err)
	}
	defer file.Close()

	axes := []int{3, 2} // width=3, height=2
	data := []float64{
		0, 1, 2,
		3, 4, 5,
	}

	img := fitsio.NewImage(-64, axes)
	defer img.Close()

	if err := img.Header().Append(
		fitsio.Card{Name: "INSTRUME", Value: "TestCam", Comment: "instrument"},
		fitsio.Card{Name: "EXPTIME", Value: 30.5, Comment: "exposure seconds"},
		fitsio.Card{Name: "DATE-OBS", Value: "2026-09-12T00:00:00", Comment: "observation date"},
	); err != nil {
		t.Fatalf("append header cards: %v", err)
	}

	if err := img.Write(data); err != nil {
		t.Fatalf("write image data: %v", err)
	}
	if err := file.Write(img); err != nil {
		t.Fatalf("write image to file: %v", err)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.fits")
	writeTestFITS(t, path)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if img.Width() != 3 || img.Height() != 2 {
		t.Fatalf("got dimensions %dx%d, want 3x2", img.Width(), img.Height())
	}
	if img.Instrument != "TestCam" {
		t.Errorf("got Instrument %q, want %q", img.Instrument, "TestCam")
	}
	if img.ExposureS != 30.5 {
		t.Errorf("got ExposureS %v, want 30.5", img.ExposureS)
	}
	if img.DateObs != "2026-09-12T00:00:00" {
		t.Errorf("got DateObs %q, want %q", img.DateObs, "2026-09-12T00:00:00")
	}

	want := []float64{0, 1, 2, 3, 4, 5}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}

	if got := img.At(2, 1); got != 5 {
		t.Errorf("At(2, 1) = %v, want 5", got)
	}
	if got := img.At(0, 0); got != 0 {
		t.Errorf("At(0, 0) = %v, want 0", got)
	}
}

func TestLoad_missingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.fits")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}
