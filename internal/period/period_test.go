package period

import (
	"math"
	"testing"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

// syntheticSinusoid builds an unevenly-sampled, multi-night light curve
// with a known true period, for validating LombScargle against ground
// truth (mirrors the project's existing pattern of testing against
// synthetic data with a known answer, e.g. TestFitBaseline_perfectLine).
func syntheticSinusoid(truePeriod float64) []timeseries.Point {
	const amplitude = 0.3
	const nights = 6
	const pointsPerNight = 15

	var points []timeseries.Point
	for night := 0; night < nights; night++ {
		for i := 0; i < pointsPerNight; i++ {
			jd := float64(night) + float64(i)*(0.3/float64(pointsPerNight))
			// Small deterministic "jitter" (not math/rand) so the test is
			// fully reproducible without seed management.
			jitter := 0.01 * math.Sin(17.3*jd+0.7)
			mag := amplitude*math.Sin(2*math.Pi*jd/truePeriod) + jitter
			points = append(points, timeseries.Point{JD: jd, DiffMag: mag, DiffMagErr: 0.01})
		}
	}
	return points
}

func TestLombScargle_recoversKnownPeriod(t *testing.T) {
	const truePeriod = 0.7 // days
	points := syntheticSinusoid(truePeriod)

	gram, err := LombScargle(points, 0.1, 3.0, 2000)
	if err != nil {
		t.Fatalf("LombScargle: %v", err)
	}

	got, power := BestPeriod(gram)
	if power <= 0 {
		t.Fatalf("BestPeriod power = %v, want > 0", power)
	}
	if math.Abs(got-truePeriod)/truePeriod > 0.02 {
		t.Errorf("BestPeriod() = %v, want within 2%% of true period %v", got, truePeriod)
	}
}

func TestLombScargle_errorsOnBadInput(t *testing.T) {
	valid := []timeseries.Point{{JD: 0, DiffMag: 0}, {JD: 1, DiffMag: 1}}

	cases := []struct {
		name                 string
		points               []timeseries.Point
		minPeriod, maxPeriod float64
		numTrials            int
	}{
		{"too few points", []timeseries.Point{{JD: 0}}, 0.1, 1.0, 10},
		{"zero min period", valid, 0, 1.0, 10},
		{"negative min period", valid, -1, 1.0, 10},
		{"max <= min", valid, 1.0, 1.0, 10},
		{"zero trials", valid, 0.1, 1.0, 0},
		{"negative trials", valid, 0.1, 1.0, -5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := LombScargle(c.points, c.minPeriod, c.maxPeriod, c.numTrials); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestLombScargle_frequencyGridSpacing(t *testing.T) {
	points := syntheticSinusoid(0.5)
	gram, err := LombScargle(points, 0.2, 2.0, 50)
	if err != nil {
		t.Fatalf("LombScargle: %v", err)
	}
	if len(gram) != 50 {
		t.Fatalf("got %d periodogram points, want 50", len(gram))
	}

	// Sorted by Period ascending -> frequency (1/Period) descending; check
	// consecutive frequency gaps are uniform (evenly spaced in frequency).
	freqs := make([]float64, len(gram))
	for i, pt := range gram {
		freqs[i] = 1.0 / pt.Period
	}
	firstGap := freqs[0] - freqs[1]
	for i := 1; i < len(freqs)-1; i++ {
		gap := freqs[i] - freqs[i+1]
		if math.Abs(gap-firstGap) > 1e-9 {
			t.Errorf("frequency gap at index %d = %v, want ~%v (uniform spacing)", i, gap, firstGap)
		}
	}
}

func TestBestPeriod_picksHighestPower(t *testing.T) {
	gram := []PeriodogramPoint{
		{Period: 1.0, Power: 0.1},
		{Period: 2.0, Power: 0.9},
		{Period: 3.0, Power: 0.4},
	}
	period, power := BestPeriod(gram)
	if period != 2.0 || power != 0.9 {
		t.Errorf("BestPeriod() = (%v, %v), want (2.0, 0.9)", period, power)
	}
}

func TestBestPeriod_empty(t *testing.T) {
	period, power := BestPeriod(nil)
	if period != 0 || power != 0 {
		t.Errorf("BestPeriod(nil) = (%v, %v), want (0, 0)", period, power)
	}
}

func TestFoldPhase_wrapsIntoUnitRange(t *testing.T) {
	points := []timeseries.Point{
		{JD: 0, DiffMag: 1},
		{JD: 0.35, DiffMag: 2},
		{JD: 1.9, DiffMag: 3},
		{JD: 5.05, DiffMag: 4},
	}
	folded, err := FoldPhase(points, 1.0)
	if err != nil {
		t.Fatalf("FoldPhase: %v", err)
	}
	for _, p := range folded {
		if p.JD < 0 || p.JD >= 1.0 {
			t.Errorf("phase %v out of [0, 1) range", p.JD)
		}
	}
}

func TestFoldPhase_preservesMagAndErr(t *testing.T) {
	points := []timeseries.Point{
		{JD: 0, DiffMag: 1.5, DiffMagErr: 0.1},
		{JD: 0.5, DiffMag: -2.5, DiffMagErr: 0.2},
	}
	folded, err := FoldPhase(points, 1.0)
	if err != nil {
		t.Fatalf("FoldPhase: %v", err)
	}
	if len(folded) != len(points) {
		t.Fatalf("got %d folded points, want %d", len(folded), len(points))
	}
	wantMags := map[float64]float64{1.5: 0.1, -2.5: 0.2}
	for _, p := range folded {
		wantErr, ok := wantMags[p.DiffMag]
		if !ok {
			t.Errorf("unexpected DiffMag %v in folded output", p.DiffMag)
			continue
		}
		if p.DiffMagErr != wantErr {
			t.Errorf("DiffMag %v: got err %v, want %v", p.DiffMag, p.DiffMagErr, wantErr)
		}
	}
}

func TestFoldPhase_errorsOnBadInput(t *testing.T) {
	points := []timeseries.Point{{JD: 0, DiffMag: 1}}
	if _, err := FoldPhase(points, 0); err == nil {
		t.Fatal("expected an error for period <= 0")
	}
	if _, err := FoldPhase(nil, 1.0); err == nil {
		t.Fatal("expected an error for empty points")
	}
}
