// Package ui provides the Fyne desktop interface: the FITS image viewer
// with star selection, and the configuration and light-curve windows.
package ui

import (
	"image"
	"image/color"
)

// PixelGrid is the minimal pixel access the renderer needs from a FITS
// image (satisfied by *fits.Image and photometry.PixelSource sources).
type PixelGrid interface {
	Width() int
	Height() int
	At(x, y int) float64
}

// Levels controls how ADU pixel values map to an 8-bit grayscale display,
// mirroring FotoDif's "Fondo" (background) and "Rango" (range) sliders.
type Levels struct {
	Background float64 // ADU value that maps to black
	Range      float64 // ADU span above Background that maps to white
}

// Render converts a FITS image into a grayscale image.Image for display,
// linearly mapping [Background, Background+Range] to [0, 255] and
// clamping outside that span.
func Render(img PixelGrid, lv Levels) *image.Gray {
	w, h := img.Width(), img.Height()
	out := image.NewGray(image.Rect(0, 0, w, h))

	span := lv.Range
	if span <= 0 {
		span = 1
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := (img.At(x, y) - lv.Background) / span * 255
			out.SetGray(x, y, color.Gray{Y: clamp8(v)})
		}
	}
	return out
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}
