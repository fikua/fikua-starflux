// Package fits reads FITS image files and exposes their pixel data and
// observation metadata for the photometry pipeline.
package fits

import (
	"fmt"
	"os"

	"codeberg.org/astrogo/fitsio"
)

// Image holds a single FITS image's pixel data and the header fields the
// photometry and timeseries packages need.
type Image struct {
	Width, Height int
	Pixels        []float64 // row-major, length Width*Height

	Instrument string
	ExposureS  float64
	DateObs    string
}

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

	pixels := make([]float64, width*height)
	if err := img.Read(&pixels); err != nil {
		return nil, fmt.Errorf("fits: %s: read pixels: %w", path, err)
	}

	return &Image{
		Width:      width,
		Height:     height,
		Pixels:     pixels,
		Instrument: headerString(header, "INSTRUME"),
		ExposureS:  headerFloat(header, "EXPTIME"),
		DateObs:    headerString(header, "DATE-OBS"),
	}, nil
}

// At returns the pixel intensity at (x, y), where x is the column and y is
// the row, both zero-based.
func (img *Image) At(x, y int) float64 {
	return img.Pixels[y*img.Width+x]
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
