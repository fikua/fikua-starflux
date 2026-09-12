// Package photometry implements aperture photometry: centroid refinement,
// aperture flux summation, and sky background subtraction via an annulus.
package photometry

import (
	"fmt"
	"math"
	"sort"
)

// PixelSource provides read-only access to an image's pixel grid.
type PixelSource interface {
	Width() int
	Height() int
	At(x, y int) float64
}

// Aperture defines the photometric aperture and sky annulus radii, in
// pixels, all measured from the refined star center.
type Aperture struct {
	R    float64 // aperture radius
	RIn  float64 // sky annulus inner radius
	ROut float64 // sky annulus outer radius
}

// Result is the outcome of measuring one star on one image.
type Result struct {
	X, Y      float64 // refined centroid position
	SkyPerPx  float64 // estimated sky background per pixel
	ApSum     float64 // raw sum of ADUs inside the aperture
	NetFlux   float64 // ApSum minus the sky contribution
	ApPixels  float64 // effective pixel count covered by the aperture
}

// Centroid refines an approximate star position (x0, y0) to a sub-pixel
// center of mass, searching within a box of the given half-width around it.
//
// pixels are used as intensity weights after subtracting bg (a flat noise
// floor); pixels at or below bg contribute zero weight.
func Centroid(img PixelSource, x0, y0 int, halfWidth int, bg float64) (x, y float64, err error) {
	if halfWidth < 1 {
		return 0, 0, fmt.Errorf("photometry: halfWidth must be >= 1, got %d", halfWidth)
	}

	var sumI, sumIX, sumIY float64
	for j := y0 - halfWidth; j <= y0+halfWidth; j++ {
		if j < 0 || j >= img.Height() {
			continue
		}
		for i := x0 - halfWidth; i <= x0+halfWidth; i++ {
			if i < 0 || i >= img.Width() {
				continue
			}
			w := img.At(i, j) - bg
			if w <= 0 {
				continue
			}
			sumI += w
			sumIX += w * float64(i)
			sumIY += w * float64(j)
		}
	}

	if sumI <= 0 {
		return 0, 0, fmt.Errorf("photometry: no signal above background in centroid box around (%d, %d)", x0, y0)
	}

	return sumIX / sumI, sumIY / sumI, nil
}

// Measure performs full aperture photometry for a star centered at
// (x0, y0), using ap to size the aperture and sky annulus.
func Measure(img PixelSource, x0, y0 int, halfWidth int, ap Aperture) (Result, error) {
	// A first pass with bg=0 gives a rough center to seed the sky estimate;
	// callers wanting a tighter centroid can call Centroid themselves first.
	cx, cy, err := Centroid(img, x0, y0, halfWidth, 0)
	if err != nil {
		return Result{}, err
	}

	sky, err := skyBackground(img, cx, cy, ap.RIn, ap.ROut)
	if err != nil {
		return Result{}, err
	}

	apSum, apPixels := apertureSum(img, cx, cy, ap.R)
	netFlux := apSum - apPixels*sky

	return Result{
		X:        cx,
		Y:        cy,
		SkyPerPx: sky,
		ApSum:    apSum,
		NetFlux:  netFlux,
		ApPixels: apPixels,
	}, nil
}

// apertureSum sums pixel ADUs within radius r of (cx, cy). Pixels straddling
// the aperture edge are weighted by the fraction of their area estimated to
// lie inside the circle, via 4x4 subpixel sampling.
func apertureSum(img PixelSource, cx, cy, r float64) (sum float64, coveredPixels float64) {
	const subSamples = 4
	minX := int(math.Floor(cx - r))
	maxX := int(math.Ceil(cx + r))
	minY := int(math.Floor(cy - r))
	maxY := int(math.Ceil(cy + r))

	for y := minY; y <= maxY; y++ {
		if y < 0 || y >= img.Height() {
			continue
		}
		for x := minX; x <= maxX; x++ {
			if x < 0 || x >= img.Width() {
				continue
			}
			frac := pixelCoverageFraction(x, y, cx, cy, r, subSamples)
			if frac <= 0 {
				continue
			}
			sum += frac * img.At(x, y)
			coveredPixels += frac
		}
	}
	return sum, coveredPixels
}

// pixelCoverageFraction estimates what fraction of pixel (px, py) — a unit
// square centered on integer coordinates — lies within radius r of (cx, cy),
// by sampling an n x n subpixel grid.
func pixelCoverageFraction(px, py int, cx, cy, r float64, n int) float64 {
	inside := 0
	step := 1.0 / float64(n)
	half := 0.5 - step/2
	for j := 0; j < n; j++ {
		sy := float64(py) - half + step*float64(j)
		for i := 0; i < n; i++ {
			sx := float64(px) - half + step*float64(i)
			dx := sx - cx
			dy := sy - cy
			if dx*dx+dy*dy <= r*r {
				inside++
			}
		}
	}
	return float64(inside) / float64(n*n)
}

// skyBackground estimates the per-pixel sky level as the median of all
// whole pixels whose centers fall within the annulus [rIn, rOut] around
// (cx, cy).
func skyBackground(img PixelSource, cx, cy, rIn, rOut float64) (float64, error) {
	minX := int(math.Floor(cx - rOut))
	maxX := int(math.Ceil(cx + rOut))
	minY := int(math.Floor(cy - rOut))
	maxY := int(math.Ceil(cy + rOut))

	var samples []float64
	for y := minY; y <= maxY; y++ {
		if y < 0 || y >= img.Height() {
			continue
		}
		for x := minX; x <= maxX; x++ {
			if x < 0 || x >= img.Width() {
				continue
			}
			dx := float64(x) - cx
			dy := float64(y) - cy
			d2 := dx*dx + dy*dy
			if d2 >= rIn*rIn && d2 <= rOut*rOut {
				samples = append(samples, img.At(x, y))
			}
		}
	}

	if len(samples) == 0 {
		return 0, fmt.Errorf("photometry: sky annulus [%.1f, %.1f] around (%.1f, %.1f) contains no pixels", rIn, rOut, cx, cy)
	}

	return median(samples), nil
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
