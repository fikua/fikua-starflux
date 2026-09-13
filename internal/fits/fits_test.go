package fits

import (
	"fmt"
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

func TestLoad_bitpix8(t *testing.T) {
	// bitpix=8 (unsigned byte) — used by some old/low-dynamic-range FITS.
	path := filepath.Join(t.TempDir(), "test8.fits")
	data := []byte{0, 1, 127, 255}
	writeTestFITS(t, path, 8, []int{4, 1}, data, 0, 0)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []float64{0, 1, 127, 255}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}
}

func TestLoad_bitpix32(t *testing.T) {
	// bitpix=32 (32-bit signed integer) — used by some high-dynamic-range
	// sensors and stacked/summed images that overflow 16 bits.
	path := filepath.Join(t.TempDir(), "test32.fits")
	data := []int32{0, 70000, -70000, 2147483647}
	writeTestFITS(t, path, 32, []int{4, 1}, data, 0, 0)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []float64{0, 70000, -70000, 2147483647}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}
}

func TestLoad_bitpix64(t *testing.T) {
	// bitpix=64 (64-bit signed integer) — rare, but part of the FITS
	// standard's integer types.
	path := filepath.Join(t.TempDir(), "test64.fits")
	data := []int64{0, 1, -1, 9007199254740993} // beyond float64's exact-integer range
	writeTestFITS(t, path, 64, []int{4, 1}, data, 0, 0)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 9007199254740993 (2^53 + 1) isn't exactly representable as float64;
	// confirm the conversion rounds to the nearest representable value
	// rather than silently corrupting in some other way.
	want := []float64{0, 1, -1, 9007199254740992}
	for i, v := range want {
		if img.Pixels[i] != v {
			t.Errorf("Pixels[%d] = %v, want %v", i, img.Pixels[i], v)
		}
	}
}

func TestLoad_bitpixNeg32(t *testing.T) {
	// bitpix=-32 (32-bit IEEE float) — common for calibrated/processed
	// images where BSCALE/BZERO integer scaling isn't used.
	path := filepath.Join(t.TempDir(), "test_neg32.fits")
	data := []float32{0, 1.5, -3.25, 12345.75}
	writeTestFITS(t, path, -32, []int{4, 1}, data, 0, 0)

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []float64{0, 1.5, -3.25, 12345.75}
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

func TestHeaderFloat_numericTypeVariants(t *testing.T) {
	// FITS header card values can arrive as different concrete numeric
	// Go types depending on how they were originally written; headerFloat
	// must normalize all of them to float64, and headerString must
	// stringify a non-string value via its %v fallback.
	path := filepath.Join(t.TempDir(), "test_types.fits")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	file, err := fitsio.Create(f)
	if err != nil {
		t.Fatalf("fitsio.Create: %v", err)
	}
	defer file.Close()

	img := fitsio.NewImage(16, []int{1, 1})
	defer img.Close()
	if err := img.Header().Append(
		fitsio.Card{Name: "MININT", Value: int(7), Comment: "int-typed value"},
		fitsio.Card{Name: "MINI64", Value: int64(9), Comment: "int64-typed value"},
		fitsio.Card{Name: "MINF32", Value: float32(2.5), Comment: "float32-typed value"},
		fitsio.Card{Name: "INSTRUME", Value: "TestCam", Comment: "string-typed value"},
	); err != nil {
		t.Fatalf("append cards: %v", err)
	}
	if err := img.Write([]int16{10}); err != nil {
		t.Fatalf("write image data: %v", err)
	}
	if err := file.Write(img); err != nil {
		t.Fatalf("write image to file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	f.Close()

	readF, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer readF.Close()
	readFile, err := fitsio.Open(readF)
	if err != nil {
		t.Fatalf("fitsio.Open: %v", err)
	}
	defer readFile.Close()
	header := readFile.HDU(0).Header()

	if got := headerFloat(header, "MININT"); got != 7 {
		t.Errorf("headerFloat(int) = %v, want 7", got)
	}
	if got := headerFloat(header, "MINI64"); got != 9 {
		t.Errorf("headerFloat(int64) = %v, want 9", got)
	}
	if got := headerFloat(header, "MINF32"); got != 2.5 {
		t.Errorf("headerFloat(float32) = %v, want 2.5", got)
	}
	if got := headerFloat(header, "MISSING"); got != 0 {
		t.Errorf("headerFloat(missing key) = %v, want 0", got)
	}
	if got := headerString(header, "MININT"); got != "7" {
		t.Errorf("headerString(non-string value) = %q, want %q", got, "7")
	}
	if got := headerString(header, "MISSING"); got != "" {
		t.Errorf("headerString(missing key) = %q, want empty", got)
	}
	if got := headerFloat(header, "INSTRUME"); got != 0 {
		t.Errorf("headerFloat(string-typed value) = %v, want 0", got)
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

	images, errs, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("got errs %v, want none", errs)
	}

	if len(images) != 3 {
		t.Fatalf("got %d images, want 3 (non-FITS file should be ignored)", len(images))
	}
	if images[0].Pixels[0] != 1 || images[1].Pixels[0] != 3 || images[2].Pixels[0] != 5 {
		t.Errorf("images not in sorted filename order: got first pixels %v, %v, %v",
			images[0].Pixels[0], images[1].Pixels[0], images[2].Pixels[0])
	}
}

func TestLoadError_Error(t *testing.T) {
	le := LoadError{Path: "/tmp/bad.fits", Err: fmt.Errorf("boom")}
	want := "/tmp/bad.fits: boom"
	if got := le.Error(); got != want {
		t.Errorf("LoadError.Error() = %q, want %q", got, want)
	}
}

func TestLoadDir_partialFailureStillLoadsGoodFiles(t *testing.T) {
	dir := t.TempDir()

	writeTestFITS(t, filepath.Join(dir, "img_000.fits"), 16, []int{2, 1}, []int16{1, 2}, 0, 0)
	// A corrupt "FITS" file that will fail to parse.
	if err := os.WriteFile(filepath.Join(dir, "img_001.fits"), []byte("not a real fits file"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	writeTestFITS(t, filepath.Join(dir, "img_002.fits"), 16, []int{2, 1}, []int16{5, 6}, 0, 0)

	images, errs, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if len(images) != 2 {
		t.Fatalf("got %d images, want 2 (the good files should still load)", len(images))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
	if filepath.Base(errs[0].Path) != "img_001.fits" {
		t.Errorf("got error for %q, want img_001.fits", errs[0].Path)
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
	images, errs, err := LoadFiles([]string{pathC, pathA, pathB})
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("got errs %v, want none", errs)
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
	if _, _, err := LoadFiles(nil); err == nil {
		t.Fatal("expected an error for an empty file list")
	}
}

func TestLoadFiles_allFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.fits")
	if err := os.WriteFile(path, []byte("not a real fits file"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	images, errs, err := LoadFiles([]string{path})
	if err == nil {
		t.Fatal("expected an error when every file fails to load")
	}
	if len(images) != 0 {
		t.Errorf("got %d images, want 0", len(images))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
}

func TestLoadDir_empty(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LoadDir(dir); err == nil {
		t.Fatal("expected an error for a directory with no FITS files")
	}
}

func TestLoadDir_missingDir(t *testing.T) {
	if _, _, err := LoadDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}
