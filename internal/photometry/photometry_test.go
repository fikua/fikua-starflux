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

func TestMaxADU_findsPeakInBox(t *testing.T) {
	g := newGrid(41, 41, 100) // flat background at 100 ADU
	g.set(20, 20, 60000)      // a bright, near-saturated pixel

	got := MaxADU(g, 20, 20, 10)
	if got != 60000 {
		t.Errorf("MaxADU = %v, want 60000", got)
	}
}

func TestMaxADU_ignoresPixelsOutsideBox(t *testing.T) {
	g := newGrid(41, 41, 100)
	g.set(5, 5, 65000) // bright pixel well outside the box around (20, 20)

	got := MaxADU(g, 20, 20, 3)
	if got != 100 {
		t.Errorf("MaxADU = %v, want 100 (the flat background, not the distant bright pixel)", got)
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

func TestMeasure_offCenterSeedConvergesToStarNotBoxCenter(t *testing.T) {
	// A realistic sky background (matching the ~2818 ADU seen on the
	// project's own v_000.fit fixture), with a star seeded a few pixels
	// off its true center inside a halfWidth=10 (21x21) box. Before
	// Measure subtracted the local background ahead of the first centroid
	// pass, this flat background dominated the bg=0 weighted center of
	// mass, so the "centroid" converged toward the search box's geometric
	// center (30, 32) rather than the real star at (30, 30).
	g := newGrid(61, 61, 2818)
	addGaussianStar(g, 30, 30, 5000, 3)

	ap := Aperture{R: 8, RIn: 12, ROut: 18}
	res, err := Measure(g, 30, 32, 10, ap)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if math.Abs(res.X-30) > 0.5 || math.Abs(res.Y-30) > 0.5 {
		t.Errorf("got centroid (%.3f, %.3f), want ~(30, 30) (the real star, not the seed box's center (30, 32))", res.X, res.Y)
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

func TestDistance(t *testing.T) {
	cases := []struct {
		name                           string
		prevX, prevY, newX, newY, want float64
	}{
		{"3-4-5 triangle", 0, 0, 3, 4, 5},
		{"zero distance", 10, 10, 10, 10, 0},
		{"horizontal only", 0, 0, 7, 0, 7},
		{"vertical only", 0, 0, 0, 7, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Distance(c.prevX, c.prevY, c.newX, c.newY)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("Distance(%v,%v, %v,%v) = %v, want %v", c.prevX, c.prevY, c.newX, c.newY, got, c.want)
			}
		})
	}
}

func TestDetectStars_findsIsolatedPeaks(t *testing.T) {
	g := newGrid(61, 61, 0)
	addGaussianStar(g, 10, 10, 1000, 2)
	addGaussianStar(g, 30, 40, 1000, 2)
	addGaussianStar(g, 50, 20, 1000, 2)

	got := DetectStars(g, 100, 5, DefaultSigmaMultiplier)
	if len(got) != 3 {
		t.Fatalf("DetectStars found %d detections, want 3: %+v", len(got), got)
	}
	wantCenters := [][2]float64{{10, 10}, {30, 40}, {50, 20}}
	for _, want := range wantCenters {
		found := false
		for _, d := range got {
			if Distance(d.X, d.Y, want[0], want[1]) <= 1 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no detection within 1px of expected star at %v; got %+v", want, got)
		}
	}
}

func TestDetectStars_suppressesCloseDuplicates(t *testing.T) {
	g := newGrid(41, 41, 0)
	addGaussianStar(g, 20, 20, 1000, 2) // brighter
	addGaussianStar(g, 22, 20, 400, 2)  // dimmer, close by

	got := DetectStars(g, 100, 10, DefaultSigmaMultiplier)
	if len(got) != 1 {
		t.Fatalf("DetectStars found %d detections, want 1 (close duplicate suppressed): %+v", len(got), got)
	}
	if Distance(got[0].X, got[0].Y, 20, 20) > 1 {
		t.Errorf("got detection at (%v, %v), want the brighter star near (20, 20)", got[0].X, got[0].Y)
	}
}

func TestDetectStars_respectsMinCounts(t *testing.T) {
	g := newGrid(41, 41, 0)
	addGaussianStar(g, 20, 20, 50, 2) // faint peak

	if got := DetectStars(g, 100, 5, DefaultSigmaMultiplier); len(got) != 0 {
		t.Errorf("DetectStars with high minCounts found %d detections, want 0: %+v", len(got), got)
	}
	if got := DetectStars(g, 10, 5, DefaultSigmaMultiplier); len(got) != 1 {
		t.Errorf("DetectStars with low minCounts found %d detections, want 1", len(got))
	}
}

func TestDetectStars_emptyImageYieldsNoDetections(t *testing.T) {
	g := newGrid(21, 21, 5) // flat background, no stars
	// minCounts above the flat background level: no pixel qualifies as a
	// candidate, regardless of the flat plateau's own local-max status.
	if got := DetectStars(g, 100, 5, DefaultSigmaMultiplier); len(got) != 0 {
		t.Errorf("DetectStars on a flat image found %d detections, want 0: %+v", len(got), got)
	}
}

func TestDetectStars_flatHighBackgroundYieldsNoDetections(t *testing.T) {
	g := newGrid(100, 100, 2818) // flat background at a real-world-like ADU level
	got := DetectStars(g, 0, 8, DefaultSigmaMultiplier)
	if len(got) != 0 {
		t.Fatalf("DetectStars on a flat high background found %d detections, want 0: %+v", len(got), got)
	}
}

func TestDetectStars_findsStarOnHighFlatBackground(t *testing.T) {
	g := newGrid(100, 100, 2818)
	addGaussianStar(g, 50, 50, 5200, 3) // peak ~8018
	got := DetectStars(g, 0, 8, DefaultSigmaMultiplier)
	if len(got) != 1 {
		t.Fatalf("DetectStars found %d detections, want 1: %+v", len(got), got)
	}
	if Distance(got[0].X, got[0].Y, 50, 50) > 1 {
		t.Errorf("got detection at (%v, %v), want near (50, 50)", got[0].X, got[0].Y)
	}
}

func TestEstimateBackground_constantImageHasZeroSigma(t *testing.T) {
	g := newGrid(50, 50, 2818)
	got := EstimateBackground(g)
	if got.Median != 2818 {
		t.Errorf("got Median %v, want 2818", got.Median)
	}
	if got.Sigma != 0 {
		t.Errorf("got Sigma %v, want 0", got.Sigma)
	}
}

func TestEstimateBackground_recoversKnownMedianAndSpread(t *testing.T) {
	g := newGrid(50, 50, 0)
	const base, delta = 3000.0, 20.0
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if (x+y)%2 == 0 {
				g.set(x, y, base-delta)
			} else {
				g.set(x, y, base+delta)
			}
		}
	}
	got := EstimateBackground(g)
	if math.Abs(got.Median-base) > 1e-9 {
		t.Errorf("got Median %v, want %v", got.Median, base)
	}
	wantSigma := 1.4826 * delta
	if math.Abs(got.Sigma-wantSigma) > 1e-9 {
		t.Errorf("got Sigma %v, want %v", got.Sigma, wantSigma)
	}
}

func TestEstimateBackground_robustToBrightOutliers(t *testing.T) {
	g := newGrid(100, 100, 2818)
	addGaussianStar(g, 50, 50, 60000, 3) // one bright star among 10,000 background pixels

	got := EstimateBackground(g)
	if math.Abs(got.Median-2818) > 1 {
		t.Errorf("got Median %v, want ~2818 (robust to a single bright outlier region)", got.Median)
	}
	if got.Sigma > 1 {
		t.Errorf("got Sigma %v, want ~0 (robust to a single bright outlier region)", got.Sigma)
	}
}

func TestMedianDisplacement_consensusAmongAgreeingStars(t *testing.T) {
	displacements := []Displacement{
		{DX: 1.0, DY: 2.0},
		{DX: 1.1, DY: 1.9},
		{DX: 0.9, DY: 2.1},
	}
	got := MedianDisplacement(displacements)
	if math.Abs(got.DX-1.0) > 0.2 || math.Abs(got.DY-2.0) > 0.2 {
		t.Errorf("MedianDisplacement(%v) = %+v, want close to (1.0, 2.0)", displacements, got)
	}
}

func TestMedianDisplacement_robustToOneOutlier(t *testing.T) {
	displacements := []Displacement{
		{DX: 1.0, DY: 2.0},
		{DX: 1.1, DY: 1.9},
		{DX: 0.9, DY: 2.1},
		{DX: 40.0, DY: -30.0}, // one star locked onto a wrong, distant neighbor
	}
	got := MedianDisplacement(displacements)
	if math.Abs(got.DX-1.0) > 0.2 || math.Abs(got.DY-2.0) > 0.2 {
		t.Errorf("MedianDisplacement(%v) = %+v, want close to (1.0, 2.0) despite the outlier", displacements, got)
	}
}

func TestWithinTolerance(t *testing.T) {
	cases := []struct {
		name         string
		prevX, prevY float64
		newX, newY   float64
		maxPixels    float64
		want         bool
	}{
		{"identical position", 10, 10, 10, 10, 2, true},
		{"clearly inside", 10, 10, 11, 10.5, 2, true},
		{"clearly outside", 10, 10, 20, 20, 2, false},
		{"exact boundary", 10, 10, 12, 10, 2, true},
		{"just past boundary", 10, 10, 12.01, 10, 2, false},
		{"zero tolerance, exact match", 10, 10, 10, 10, 0, true},
		{"zero tolerance, any drift fails", 10, 10, 10.01, 10, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WithinTolerance(c.prevX, c.prevY, c.newX, c.newY, c.maxPixels)
			if got != c.want {
				t.Errorf("WithinTolerance(%v,%v, %v,%v, max=%v) = %v, want %v",
					c.prevX, c.prevY, c.newX, c.newY, c.maxPixels, got, c.want)
			}
		})
	}
}
