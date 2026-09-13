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
	X, Y     float64 // refined centroid position
	SkyPerPx float64 // estimated sky background per pixel
	ApSum    float64 // raw sum of ADUs inside the aperture
	NetFlux  float64 // ApSum minus the sky contribution
	ApPixels float64 // effective pixel count covered by the aperture
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

// MaxADU returns the brightest pixel value within a box of the given
// half-width around (x0, y0), as a guide for spotting sensor saturation
// before it's baked into hours of imaging (mirrors the max-pixel value
// FotoDif shows in its star-selection window).
func MaxADU(img PixelSource, x0, y0 int, halfWidth int) float64 {
	max := 0.0
	for j := y0 - halfWidth; j <= y0+halfWidth; j++ {
		if j < 0 || j >= img.Height() {
			continue
		}
		for i := x0 - halfWidth; i <= x0+halfWidth; i++ {
			if i < 0 || i >= img.Width() {
				continue
			}
			if v := img.At(i, j); v > max {
				max = v
			}
		}
	}
	return max
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

// Distance returns the Euclidean pixel distance between two positions.
func Distance(prevX, prevY, newX, newY float64) float64 {
	return math.Hypot(newX-prevX, newY-prevY)
}

// WithinTolerance reports whether a star's newly measured position
// (newX, newY) has not drifted more than maxPixels from its previous
// known position (prevX, prevY).
func WithinTolerance(prevX, prevY, newX, newY, maxPixels float64) bool {
	return Distance(prevX, prevY, newX, newY) <= maxPixels
}

// CombineComparisons merges multiple comparison-star measurements into a
// single synthetic Result by averaging in flux space (NetFlux, ApSum,
// ApPixels, SkyPerPx, X, Y) rather than magnitude space — flux is what
// physically adds, so this is the statistically correct way to combine
// independent brightness measurements and matches standard multi-
// comparison differential photometry practice. Averaging magnitudes
// instead would bias the combined result toward the faintest star.
func CombineComparisons(results []Result) (Result, error) {
	if len(results) == 0 {
		return Result{}, fmt.Errorf("photometry: CombineComparisons: no results to combine")
	}

	var sumX, sumY, sumSky, sumApSum, sumNetFlux, sumApPixels float64
	for _, r := range results {
		sumX += r.X
		sumY += r.Y
		sumSky += r.SkyPerPx
		sumApSum += r.ApSum
		sumNetFlux += r.NetFlux
		sumApPixels += r.ApPixels
	}

	n := float64(len(results))
	return Result{
		X:        sumX / n,
		Y:        sumY / n,
		SkyPerPx: sumSky / n,
		ApSum:    sumApSum / n,
		NetFlux:  sumNetFlux / n,
		ApPixels: sumApPixels / n,
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

// Detection is one candidate star found by DetectStars: its pixel-precision
// position and peak ADU value.
type Detection struct {
	X, Y float64
	Peak float64
}

// DetectStars performs a simple bulk star-finder over the full image: it
// scans for local-maxima pixels exceeding minCounts, then greedily keeps
// the brightest detections first, suppressing any other candidate within
// minSeparation pixels of an already-kept one (non-maximum suppression).
//
// This deliberately does not attempt to replicate FotoDif's undocumented
// "RSR" (an unexplained signal-ratio threshold) — no specification of it
// exists anywhere in the transcribed manual. minCounts is the one
// threshold FotoDif's manual actually explains ("mínimo de cuentas"), and
// is sufficient for a "good enough" bulk marking aid; the manual itself
// frames this feature as needing experimental retuning regardless
// ("tal vez necesiten determinarse experimentalmente").
//
// Detected positions are pixel-precision (the local-max pixel), not
// centroid-refined — callers should re-run Centroid/Measure after
// accepting a detection if sub-pixel precision matters.
func DetectStars(img PixelSource, minCounts, minSeparation float64) []Detection {
	w, h := img.Width(), img.Height()

	var candidates []Detection
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			v := img.At(x, y)
			if v < minCounts {
				continue
			}
			if !isLocalMax3x3(img, x, y, v) {
				continue
			}
			candidates = append(candidates, Detection{X: float64(x), Y: float64(y), Peak: v})
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Peak > candidates[j].Peak })

	var kept []Detection
	for _, c := range candidates {
		if !tooCloseToAny(c, kept, minSeparation) {
			kept = append(kept, c)
		}
	}
	return kept
}

// isLocalMax3x3 reports whether (x, y)'s value v is >= every one of its 8
// immediate neighbors (a plateau of equal-value pixels is treated as a
// local max at each pixel; suppression removes the resulting duplicates).
func isLocalMax3x3(img PixelSource, x, y int, v float64) bool {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			if img.At(x+dx, y+dy) > v {
				return false
			}
		}
	}
	return true
}

func tooCloseToAny(c Detection, kept []Detection, minSeparation float64) bool {
	for _, k := range kept {
		if Distance(c.X, c.Y, k.X, k.Y) < minSeparation {
			return true
		}
	}
	return false
}
