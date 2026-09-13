package timeseries

import (
	"math"
	"reflect"
	"testing"

	"github.com/fikua/fikua-starflux/internal/photometry"
)

func TestMagnitude(t *testing.T) {
	// m = -2.5*log10(flux); flux=1 -> mag=0; flux=100 -> mag=-5.
	if got := Magnitude(1); math.Abs(got) > 1e-9 {
		t.Errorf("Magnitude(1) = %v, want 0", got)
	}
	if got := Magnitude(100); math.Abs(got-(-5)) > 1e-9 {
		t.Errorf("Magnitude(100) = %v, want -5", got)
	}
	if got := Magnitude(0); !math.IsInf(got, 1) {
		t.Errorf("Magnitude(0) = %v, want +Inf", got)
	}
	if got := Magnitude(-10); !math.IsInf(got, 1) {
		t.Errorf("Magnitude(-10) = %v, want +Inf", got)
	}
}

func TestDifferentialMagnitude_equalFlux(t *testing.T) {
	obs := Observation{
		JD:     2460000.5,
		Target: photometry.Result{NetFlux: 5000},
		Comp:   photometry.Result{NetFlux: 5000},
	}
	p, err := DifferentialMagnitude(obs)
	if err != nil {
		t.Fatalf("DifferentialMagnitude: %v", err)
	}
	if math.Abs(p.DiffMag) > 1e-9 {
		t.Errorf("got DiffMag %v, want 0 for equal flux", p.DiffMag)
	}
	if p.JD != obs.JD {
		t.Errorf("got JD %v, want %v", p.JD, obs.JD)
	}
	if p.DiffMagErr != 0 {
		t.Errorf("got DiffMagErr %v, want 0 when GainEPerADU is unset", p.DiffMagErr)
	}
}

func TestDifferentialMagnitude_fainterTarget(t *testing.T) {
	// Target fainter than comparison -> larger (less negative / more
	// positive) magnitude -> positive DiffMag.
	obs := Observation{
		Target: photometry.Result{NetFlux: 1000},
		Comp:   photometry.Result{NetFlux: 5000},
	}
	p, err := DifferentialMagnitude(obs)
	if err != nil {
		t.Fatalf("DifferentialMagnitude: %v", err)
	}
	if p.DiffMag <= 0 {
		t.Errorf("got DiffMag %v, want > 0 (target fainter than comp)", p.DiffMag)
	}
}

func TestDifferentialMagnitude_withGain(t *testing.T) {
	obs := Observation{
		Target:      photometry.Result{NetFlux: 20000, ApPixels: 113, SkyPerPx: 50},
		Comp:        photometry.Result{NetFlux: 20000, ApPixels: 113, SkyPerPx: 50},
		GainEPerADU: 1.0,
	}
	p, err := DifferentialMagnitude(obs)
	if err != nil {
		t.Fatalf("DifferentialMagnitude: %v", err)
	}
	if p.DiffMagErr <= 0 {
		t.Errorf("got DiffMagErr %v, want > 0 when gain is set", p.DiffMagErr)
	}
	if p.DiffMagErr > 0.1 {
		t.Errorf("got DiffMagErr %v, want a small value for a high-SNR measurement", p.DiffMagErr)
	}
}

func TestDifferentialMagnitude_nonPositiveFlux(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
	}{
		{"target", Observation{Target: photometry.Result{NetFlux: 0}, Comp: photometry.Result{NetFlux: 100}}},
		{"comp", Observation{Target: photometry.Result{NetFlux: 100}, Comp: photometry.Result{NetFlux: -5}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DifferentialMagnitude(c.obs); err == nil {
				t.Fatal("expected an error for non-positive net flux")
			}
		})
	}
}

func TestSeries_skipsBadEpochsAndReportsErrors(t *testing.T) {
	obs := []Observation{
		{JD: 1, Target: photometry.Result{NetFlux: 1000}, Comp: photometry.Result{NetFlux: 1000}},
		{JD: 2, Target: photometry.Result{NetFlux: 0}, Comp: photometry.Result{NetFlux: 1000}}, // bad
		{JD: 3, Target: photometry.Result{NetFlux: 900}, Comp: photometry.Result{NetFlux: 1000}},
	}

	points, errs := Series(obs)

	if len(points) != 2 {
		t.Fatalf("got %d points, want 2 (one epoch should be skipped)", len(points))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1", len(errs))
	}
	if points[0].JD != 1 || points[1].JD != 3 {
		t.Errorf("got JDs %v, %v, want 1, 3", points[0].JD, points[1].JD)
	}
}

func TestMergeSorted_concatenatesAndSortsByJD(t *testing.T) {
	prev := []Point{{JD: 1}, {JD: 3}}
	next := []Point{{JD: 2}, {JD: 4}} // deliberately interleaved with prev

	got := MergeSorted(prev, next)

	if len(got) != 4 {
		t.Fatalf("got %d points, want 4", len(got))
	}
	wantJD := []float64{1, 2, 3, 4}
	for i, want := range wantJD {
		if got[i].JD != want {
			t.Errorf("got[%d].JD = %v, want %v", i, got[i].JD, want)
		}
	}
}

func TestMergeSorted_emptyPrev(t *testing.T) {
	next := []Point{{JD: 5}, {JD: 6}}
	got := MergeSorted(nil, next)
	if len(got) != 2 || got[0].JD != 5 || got[1].JD != 6 {
		t.Errorf("MergeSorted(nil, next) = %+v, want next unchanged", got)
	}
}

func TestMergeSorted_emptyNext(t *testing.T) {
	prev := []Point{{JD: 5}, {JD: 6}}
	got := MergeSorted(prev, nil)
	if len(got) != 2 || got[0].JD != 5 || got[1].JD != 6 {
		t.Errorf("MergeSorted(prev, nil) = %+v, want prev unchanged", got)
	}
}

func TestMergeSorted_bothEmpty(t *testing.T) {
	got := MergeSorted(nil, nil)
	if len(got) != 0 {
		t.Errorf("MergeSorted(nil, nil) = %+v, want empty", got)
	}
}

func TestFitBaseline_perfectLine(t *testing.T) {
	// DiffMag = 0.001*JD - 2000, sampled at a few JDs; recover slope/intercept.
	const slope, intercept = 0.001, -2000.0
	points := []Point{
		{JD: 100, DiffMag: slope*100 + intercept},
		{JD: 200, DiffMag: slope*200 + intercept},
		{JD: 300, DiffMag: slope*300 + intercept},
	}

	gotSlope, gotIntercept, err := FitBaseline(points, [][2]float64{{100, 300}})
	if err != nil {
		t.Fatalf("FitBaseline: %v", err)
	}
	if math.Abs(gotSlope-slope) > 1e-9 {
		t.Errorf("got slope %v, want %v", gotSlope, slope)
	}
	if math.Abs(gotIntercept-intercept) > 1e-6 {
		t.Errorf("got intercept %v, want %v", gotIntercept, intercept)
	}
}

func TestFitBaseline_ignoresPointsOutsideRange(t *testing.T) {
	points := []Point{
		{JD: 1, DiffMag: 0},
		{JD: 2, DiffMag: 0},
		{JD: 100, DiffMag: 999}, // way outside the range, would ruin the fit if included
	}

	slope, intercept, err := FitBaseline(points, [][2]float64{{1, 2}})
	if err != nil {
		t.Fatalf("FitBaseline: %v", err)
	}
	if math.Abs(slope) > 1e-9 || math.Abs(intercept) > 1e-9 {
		t.Errorf("got slope=%v intercept=%v, want ~0,0 (outlier outside range should be ignored)", slope, intercept)
	}
}

func TestFitBaseline_twoRanges(t *testing.T) {
	// Two flat baseline zones (e.g. before/after a transit dip), all on the
	// same line; a dip in between is excluded by not being in either range.
	const slope, intercept = 0.0, -0.05
	points := []Point{
		{JD: 1, DiffMag: intercept},
		{JD: 2, DiffMag: intercept},
		{JD: 5, DiffMag: -0.5}, // the "transit," excluded by range selection
		{JD: 8, DiffMag: intercept},
		{JD: 9, DiffMag: intercept},
	}

	gotSlope, gotIntercept, err := FitBaseline(points, [][2]float64{{1, 2}, {8, 9}})
	if err != nil {
		t.Fatalf("FitBaseline: %v", err)
	}
	if math.Abs(gotSlope-slope) > 1e-9 {
		t.Errorf("got slope %v, want %v", gotSlope, slope)
	}
	if math.Abs(gotIntercept-intercept) > 1e-9 {
		t.Errorf("got intercept %v, want %v", gotIntercept, intercept)
	}
}

func TestFitBaseline_tooFewPoints(t *testing.T) {
	points := []Point{{JD: 1, DiffMag: 0}}
	if _, _, err := FitBaseline(points, [][2]float64{{0, 2}}); err == nil {
		t.Fatal("expected an error with fewer than 2 points in range")
	}
}

func TestSubtractTilt_leavesInputUnmodified(t *testing.T) {
	original := []Point{
		{JD: 1, DiffMag: 1.0},
		{JD: 2, DiffMag: 2.0},
	}
	// Keep a copy to compare against after the call.
	before := append([]Point(nil), original...)

	got := SubtractTilt(original, 1.0, 0.0) // DiffMag -= JD

	if !reflect.DeepEqual(original, before) {
		t.Errorf("SubtractTilt mutated its input: got %+v, want unchanged %+v", original, before)
	}
	want := []Point{
		{JD: 1, DiffMag: 0.0},
		{JD: 2, DiffMag: 0.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubtractTilt() = %+v, want %+v", got, want)
	}
}

func TestDispersion_constantSeries(t *testing.T) {
	points := []Point{{DiffMag: 1.0}, {DiffMag: 1.0}, {DiffMag: 1.0}}
	if got := Dispersion(points); got != 0 {
		t.Errorf("Dispersion(constant) = %v, want 0", got)
	}
}

func TestDispersion_knownSpread(t *testing.T) {
	// Values 2, 4, 4, 4, 5, 5, 7, 9 have a well-known sample stddev of 2.13
	// (n-1 denominator) - classic textbook example.
	points := []Point{
		{DiffMag: 2}, {DiffMag: 4}, {DiffMag: 4}, {DiffMag: 4},
		{DiffMag: 5}, {DiffMag: 5}, {DiffMag: 7}, {DiffMag: 9},
	}
	got := Dispersion(points)
	want := 2.1380899
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("Dispersion() = %v, want ~%v", got, want)
	}
}

func TestDispersion_fewerThanTwoPoints(t *testing.T) {
	if got := Dispersion(nil); got != 0 {
		t.Errorf("Dispersion(nil) = %v, want 0", got)
	}
	if got := Dispersion([]Point{{DiffMag: 5}}); got != 0 {
		t.Errorf("Dispersion(1 point) = %v, want 0", got)
	}
}

func TestMedianErr_oddAndEvenCounts(t *testing.T) {
	cases := []struct {
		points []Point
		want   float64
	}{
		{[]Point{{DiffMagErr: 1}}, 1},
		{[]Point{{DiffMagErr: 1}, {DiffMagErr: 3}}, 2},
		{[]Point{{DiffMagErr: 3}, {DiffMagErr: 1}, {DiffMagErr: 2}}, 2},
		{nil, 0},
	}
	for _, c := range cases {
		if got := MedianErr(c.points); got != c.want {
			t.Errorf("MedianErr(%v) = %v, want %v", c.points, got, c.want)
		}
	}
}

func TestVariabilityRatio_zeroMedianErr(t *testing.T) {
	points := []Point{{DiffMag: 1, DiffMagErr: 0}, {DiffMag: 2, DiffMagErr: 0}}
	if got := VariabilityRatio(points); got != 0 {
		t.Errorf("VariabilityRatio() = %v, want 0 when median error is 0", got)
	}
}

func TestVariabilityRatio_knownRatio(t *testing.T) {
	// Dispersion of {0, 2} is sqrt(2) (mean 1, sumSq 2, /1 = 2 -> sqrt(2)).
	// MedianErr of {0.1, 0.1} is 0.1. Ratio = sqrt(2)/0.1.
	points := []Point{
		{DiffMag: 0, DiffMagErr: 0.1},
		{DiffMag: 2, DiffMagErr: 0.1},
	}
	got := VariabilityRatio(points)
	want := math.Sqrt(2) / 0.1
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("VariabilityRatio() = %v, want %v", got, want)
	}
}

func TestIsPossibleVariable_aboveAndBelowThreshold(t *testing.T) {
	// Ratio here is sqrt(2)/0.1 ~= 14.14.
	points := []Point{
		{DiffMag: 0, DiffMagErr: 0.1},
		{DiffMag: 2, DiffMagErr: 0.1},
	}
	if !IsPossibleVariable(points, 3.0) {
		t.Error("IsPossibleVariable() = false, want true for a ratio well above threshold 3.0")
	}
	if IsPossibleVariable(points, 100.0) {
		t.Error("IsPossibleVariable() = true, want false for a ratio well below threshold 100.0")
	}
}
