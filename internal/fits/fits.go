// Package fits reads FITS image files and exposes their pixel data and
// observation metadata for the photometry pipeline.
package fits

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codeberg.org/astrogo/fitsio"
)

// Image holds a single FITS image's pixel data and the header fields the
// photometry and timeseries packages need.
type Image struct {
	W, H   int
	Pixels []float64 // row-major, length W*H

	Instrument string
	ExposureS  float64
	DateObs    string
	JD         float64 // Julian Date derived from DATE-OBS, 0 if unparsable

	Path string // source file path, set by LoadDir
}

// Width returns the image width in pixels.
func (img *Image) Width() int { return img.W }

// Height returns the image height in pixels.
func (img *Image) Height() int { return img.H }

// Load reads a FITS file from disk and returns its primary HDU as an Image.
func Load(path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fits: open %s: %w", path, err)
	}
	defer f.Close()

	file, err := fitsio.Open(f)
	if err != nil {
		return nil, fmt.Errorf("fits: parse %s: %w", path, err)
	}
	defer file.Close()

	hdu := file.HDU(0)
	header := hdu.Header()

	img, ok := hdu.(fitsio.Image)
	if !ok {
		return nil, fmt.Errorf("fits: %s: primary HDU is not an image", path)
	}

	axes := header.Axes()
	if len(axes) != 2 {
		return nil, fmt.Errorf("fits: %s: expected a 2D image, got axes %v", path, axes)
	}
	width, height := axes[0], axes[1]

	pixels, err := readPixels(img, header, width*height)
	if err != nil {
		return nil, fmt.Errorf("fits: %s: read pixels: %w", path, err)
	}

	dateObs := headerString(header, "DATE-OBS")

	return &Image{
		W:          width,
		H:          height,
		Pixels:     pixels,
		Instrument: headerString(header, "INSTRUME"),
		ExposureS:  headerFloat(header, "EXPTIME"),
		DateObs:    dateObs,
		JD:         julianDate(dateObs),
		Path:       path,
	}, nil
}

// LoadDir loads every FITS file (.fits, .fit, .fts, case-insensitive) found
// directly inside dir, sorted by filename, which for a normally-named
// observing session corresponds to acquisition order.
func LoadDir(dir string) ([]*Image, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("fits: read dir %s: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isFITSExt(e.Name()) {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("fits: %s: no FITS files found", dir)
	}

	return LoadFiles(paths)
}

// LoadFiles loads each of the given FITS file paths, sorted by filename
// (which for a normally-named observing session corresponds to acquisition
// order).
func LoadFiles(paths []string) ([]*Image, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("fits: no files given")
	}

	sorted := append([]string(nil), paths...)
	sort.Slice(sorted, func(i, j int) bool {
		return filepath.Base(sorted[i]) < filepath.Base(sorted[j])
	})

	images := make([]*Image, 0, len(sorted))
	for _, path := range sorted {
		img, err := Load(path)
		if err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, nil
}

func isFITSExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".fits", ".fit", ".fts":
		return true
	default:
		return false
	}
}

// julianDate converts a FITS DATE-OBS string (ISO 8601, e.g.
// "2011-08-17T00:48:21" or "2011-08-17") to a Julian Date. Returns 0 if the
// value can't be parsed.
func julianDate(dateObs string) float64 {
	if dateObs == "" {
		return 0
	}
	layouts := []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, dateObs)
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0
	}

	// Fliegel-Van Flandern algorithm for the Julian Day Number, plus the
	// fractional day from the time-of-day (JD 0 begins at noon UTC, hence
	// the 12h offset).
	y, m, d := t.Date()
	a := (14 - int(m)) / 12
	y2 := y + 4800 - a
	m2 := int(m) + 12*a - 3
	jdn := d + (153*m2+2)/5 + 365*y2 + y2/4 - y2/100 + y2/400 - 32045

	dayFrac := (float64(t.Hour())-12)/24 + float64(t.Minute())/1440 + float64(t.Second())/86400
	return float64(jdn) + dayFrac
}

// At returns the pixel intensity at (x, y), where x is the column and y is
// the row, both zero-based.
func (img *Image) At(x, y int) float64 {
	return img.Pixels[y*img.W+x]
}

// readPixels reads the raw pixel data using the type matching the image's
// bitpix (fitsio requires an exact match), converts it to float64, and
// applies BSCALE/BZERO rescaling per the FITS standard.
func readPixels(img fitsio.Image, header *fitsio.Header, n int) ([]float64, error) {
	bitpix := header.Bitpix()

	var out []float64
	switch bitpix {
	case 8:
		raw := make([]byte, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = make([]float64, n)
		for i, v := range raw {
			out[i] = float64(v)
		}
	case 16:
		raw := make([]int16, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = make([]float64, n)
		for i, v := range raw {
			out[i] = float64(v)
		}
	case 32:
		raw := make([]int32, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = make([]float64, n)
		for i, v := range raw {
			out[i] = float64(v)
		}
	case 64:
		raw := make([]int64, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = make([]float64, n)
		for i, v := range raw {
			out[i] = float64(v)
		}
	case -32:
		raw := make([]float32, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = make([]float64, n)
		for i, v := range raw {
			out[i] = float64(v)
		}
	case -64:
		raw := make([]float64, n)
		if err := img.Read(&raw); err != nil {
			return nil, err
		}
		out = raw
	default:
		return nil, fmt.Errorf("unsupported bitpix %d", bitpix)
	}

	bzero := headerFloat(header, "BZERO")
	bscale := headerFloat(header, "BSCALE")
	if bscale == 0 {
		bscale = 1
	}
	if bzero != 0 || bscale != 1 {
		for i, v := range out {
			out[i] = v*bscale + bzero
		}
	}

	return out, nil
}

func headerString(h *fitsio.Header, key string) string {
	card := h.Get(key)
	if card == nil {
		return ""
	}
	if s, ok := card.Value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", card.Value)
}

func headerFloat(h *fitsio.Header, key string) float64 {
	card := h.Get(key)
	if card == nil {
		return 0
	}
	switch v := card.Value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
