package timeseries

import (
	"math"
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
