package fits

import (
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/astrogo/fitsio"
)

// writeTestFITS writes a FITS file with the given bitpix and pixel data
// (data's concrete type must match bitpix, per fitsio's requirements),
// plus standard metadata cards and, optionally, BSCALE/BZERO.
func writeTestFITS(t *testing.T, path string, bitpix int, axes []int, data interface{}, bscale, bzero float64) {
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

	img := fitsio.NewImage(bitpix, axes)
	defer img.Close()

	cards := []fitsio.Card{
		{Name: "INSTRUME", Value: "TestCam", Comment: "instrument"},
		{Name: "EXPTIME", Value: 30.5, Comment: "exposure seconds"},
		{Name: "DATE-OBS", Value: "2026-09-12T00:00:00", Comment: "observation date"},
	}
	if bscale != 0 {
		cards = append(cards, fitsio.Card{Name: "BSCALE", Value: bscale, Comment: "scale"})
	}
	if bzero != 0 {
		cards = append(cards, fitsio.Card{Name: "BZERO", Value: bzero, Comment: "zero offset"})
	}
	if err := img.Header().Append(cards...); err != nil {
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
	data := []float64{
		0, 1, 2,
		3, 4, 5,
	}
	writeTestFITS(t, path, -64, []int{3, 2}, data, 0, 0)

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

func TestLoad_bitpix16(t *testing.T) {
	// bitpix=16 (16-bit signed integer ADU values) is the common format
	// produced by CCD/CMOS astro cameras and previously crashed Load with
	// "element-size do not match" since fitsio requires an exact-size
	// read type per bitpix.
	path := filepath.Join(t.TempDir(), "test16.fits")
	data := []int16{
		100, 200, 300,
		400, 500, 32767,
	}
	writeTestFITS(t, path, 16, []int{3, 2}, data, 0, 0)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []float64{100, 200, 300, 400, 500, 32767}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}
}

func TestLoad_bscaleBzero(t *testing.T) {
	// Unsigned 16-bit sensor data is commonly stored as bitpix=16 with
	// BZERO=32768 to represent the 0-65535 range; Load must apply the
	// rescale rather than return raw signed values.
	path := filepath.Join(t.TempDir(), "test_scaled.fits")
	data := []int16{-32768, 0, 32767} // -> 0, 32768, 65535 after BZERO
	writeTestFITS(t, path, 16, []int{3, 1}, data, 1, 32768)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []float64{0, 32768, 65535}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}
}

func TestLoad_missingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.fits")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestJulianDate(t *testing.T) {
	cases := []struct {
		dateObs string
		want    float64
	}{
		{"2000-01-01T12:00:00", 2451545.0}, // J2000.0 epoch, a well-known reference value
		{"", 0},
		{"not-a-date", 0},
	}
	for _, c := range cases {
		if got := julianDate(c.dateObs); got != c.want {
			t.Errorf("julianDate(%q) = %v, want %v", c.dateObs, got, c.want)
		}
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	// Three FITS files plus one non-FITS file that must be ignored, named
	// so alphabetical sort matches acquisition order.
	writeTestFITS(t, filepath.Join(dir, "img_000.fits"), 16, []int{2, 1}, []int16{1, 2}, 0, 0)
	writeTestFITS(t, filepath.Join(dir, "img_001.fit"), 16, []int{2, 1}, []int16{3, 4}, 0, 0)
	writeTestFITS(t, filepath.Join(dir, "img_002.FITS"), 16, []int{2, 1}, []int16{5, 6}, 0, 0)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not fits"), 0o644); err != nil {
		t.Fatalf("write readme.txt: %v", err)
	}

	images, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if len(images) != 3 {
		t.Fatalf("got %d images, want 3 (non-FITS file should be ignored)", len(images))
	}
	if images[0].Pixels[0] != 1 || images[1].Pixels[0] != 3 || images[2].Pixels[0] != 5 {
		t.Errorf("images not in sorted filename order: got first pixels %v, %v, %v",
			images[0].Pixels[0], images[1].Pixels[0], images[2].Pixels[0])
	}
}

func TestLoadFiles_sortsByFilenameRegardlessOfInputOrder(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a_000.fits")
	pathB := filepath.Join(dir, "b_001.fits")
	pathC := filepath.Join(dir, "c_002.fits")
	writeTestFITS(t, pathA, 16, []int{1, 1}, []int16{1}, 0, 0)
	writeTestFITS(t, pathB, 16, []int{1, 1}, []int16{2}, 0, 0)
	writeTestFITS(t, pathC, 16, []int{1, 1}, []int16{3}, 0, 0)

	// Deliberately out of order, simulating a multi-select file picker
	// that returns paths in selection order rather than sorted order.
	images, err := LoadFiles([]string{pathC, pathA, pathB})
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}

	if len(images) != 3 {
		t.Fatalf("got %d images, want 3", len(images))
	}
	if images[0].Pixels[0] != 1 || images[1].Pixels[0] != 2 || images[2].Pixels[0] != 3 {
		t.Errorf("images not sorted by filename: got first pixels %v, %v, %v",
			images[0].Pixels[0], images[1].Pixels[0], images[2].Pixels[0])
	}
}

func TestLoadFiles_empty(t *testing.T) {
	if _, err := LoadFiles(nil); err == nil {
		t.Fatal("expected an error for an empty file list")
	}
}

func TestLoadDir_empty(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected an error for a directory with no FITS files")
	}
}

func TestLoadDir_missingDir(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
