package photometry

import (
	"math"
	"testing"
)

// grid is a simple in-memory PixelSource for tests.
type grid struct {
	w, h int
	data []float64
}

func newGrid(w, h int, fill float64) *grid {
	data := make([]float64, w*h)
	for i := range data {
		data[i] = fill
	}
	return &grid{w: w, h: h, data: data}
}

func (g *grid) Width() int  { return g.w }
func (g *grid) Height() int { return g.h }
func (g *grid) At(x, y int) float64 {
	return g.data[y*g.w+x]
}
func (g *grid) set(x, y int, v float64) {
	g.data[y*g.w+x] = v
}

// addGaussianStar adds a symmetric Gaussian source of the given peak
// amplitude and sigma centered at (cx, cy), on top of existing pixel values.
func addGaussianStar(g *grid, cx, cy, amplitude, sigma float64) {
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			v := amplitude * math.Exp(-(dx*dx+dy*dy)/(2*sigma*sigma))
			g.set(x, y, g.At(x, y)+v)
		}
	}
}

func TestCentroid_symmetricStar(t *testing.T) {
	g := newGrid(41, 41, 0)
	addGaussianStar(g, 20.0, 20.0, 1000, 3)

	x, y, err := Centroid(g, 20, 20, 10, 0)
	if err != nil {
		t.Fatalf("Centroid: %v", err)
	}
	if math.Abs(x-20.0) > 0.05 || math.Abs(y-20.0) > 0.05 {
		t.Errorf("got centroid (%.4f, %.4f), want ~(20, 20)", x, y)
	}
}

func TestCentroid_offCenterStar(t *testing.T) {
	g := newGrid(41, 41, 0)
	addGaussianStar(g, 22.7, 18.3, 1000, 3)

	x, y, err := Centroid(g, 22, 18, 10, 0)
	if err != nil {
		t.Fatalf("Centroid: %v", err)
	}
	if math.Abs(x-22.7) > 0.1 || math.Abs(y-18.3) > 0.1 {
		t.Errorf("got centroid (%.4f, %.4f), want ~(22.7, 18.3)", x, y)
	}
}

func TestCentroid_noSignal(t *testing.T) {
	g := newGrid(21, 21, 0)
	if _, _, err := Centroid(g, 10, 10, 5, 0); err == nil {
		t.Fatal("expected an error when there is no signal above background")
	}
}

func TestSkyBackground_constantAnnulus(t *testing.T) {
	g := newGrid(41, 41, 100) // flat sky at 100 ADU
	addGaussianStar(g, 20, 20, 5000, 2)

	sky, err := skyBackground(g, 20, 20, 10, 15)
	if err != nil {
		t.Fatalf("skyBackground: %v", err)
	}
	// star is tightly peaked (sigma=2) so it shouldn't leak into a
	// [10,15] radius annulus; sky should read back at ~100.
	if math.Abs(sky-100) > 0.5 {
		t.Errorf("got sky %.3f, want ~100", sky)
	}
}

func TestApertureSum_flatDisk(t *testing.T) {
	g := newGrid(41, 41, 0)
	// A uniform disk of value 10 over a large area, so edge fractional
	// coverage of the aperture circle is the only source of error.
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			g.set(x, y, 10)
		}
	}

	r := 8.0
	sum, pixels := apertureSum(g, 20, 20, r)

	wantPixels := math.Pi * r * r
	if math.Abs(pixels-wantPixels) > 1.0 {
		t.Errorf("got covered pixels %.3f, want ~%.3f (area of r=%.1f circle)", pixels, wantPixels, r)
	}

	wantSum := 10 * wantPixels
	if math.Abs(sum-wantSum) > 15 {
		t.Errorf("got sum %.3f, want ~%.3f", sum, wantSum)
	}
}

func TestMeasure_knownFlux(t *testing.T) {
	g := newGrid(61, 61, 50) // flat sky at 50 ADU
	addGaussianStar(g, 30, 30, 8000, 3)

	ap := Aperture{R: 12, RIn: 15, ROut: 20}
	res, err := Measure(g, 30, 30, 10, ap)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}

	if math.Abs(res.SkyPerPx-50) > 1 {
		t.Errorf("got SkyPerPx %.3f, want ~50", res.SkyPerPx)
	}
	if res.NetFlux <= 0 {
		t.Errorf("got NetFlux %.3f, want a large positive value (star flux minus sky)", res.NetFlux)
	}
	// A Gaussian with amplitude 8000 and sigma 3, integrated over all
	// space, totals amplitude * 2*pi*sigma^2. An r=12 aperture (4 sigma)
	// should capture nearly all of it.
	totalFlux := 8000 * 2 * math.Pi * 3 * 3
	if res.NetFlux < totalFlux*0.95 || res.NetFlux > totalFlux*1.02 {
		t.Errorf("got NetFlux %.1f, want close to the star's total flux %.1f", res.NetFlux, totalFlux)
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{1}, 1},
		{[]float64{1, 3}, 2},
		{[]float64{3, 1, 2}, 2},
		{[]float64{5, 1, 4, 2}, 3},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCombineComparisons_averagesFlux(t *testing.T) {
	results := []Result{
		{X: 10, Y: 20, SkyPerPx: 100, ApSum: 5000, NetFlux: 4000, ApPixels: 113},
		{X: 12, Y: 22, SkyPerPx: 110, ApSum: 7000, NetFlux: 6000, ApPixels: 113},
	}

	got, err := CombineComparisons(results)
	if err != nil {
		t.Fatalf("CombineComparisons: %v", err)
	}

	want := Result{X: 11, Y: 21, SkyPerPx: 105, ApSum: 6000, NetFlux: 5000, ApPixels: 113}
	if got != want {
		t.Errorf("CombineComparisons(%v) = %+v, want %+v", results, got, want)
	}
}

func TestCombineComparisons_singleResultIsUnchanged(t *testing.T) {
	r := Result{X: 5, Y: 6, SkyPerPx: 50, ApSum: 900, NetFlux: 800, ApPixels: 42}
	got, err := CombineComparisons([]Result{r})
	if err != nil {
		t.Fatalf("CombineComparisons: %v", err)
	}
	if got != r {
		t.Errorf("CombineComparisons(single) = %+v, want unchanged %+v", got, r)
	}
}

func TestCombineComparisons_emptyIsError(t *testing.T) {
	if _, err := CombineComparisons(nil); err == nil {
		t.Fatal("expected an error when combining zero results")
	}
}
