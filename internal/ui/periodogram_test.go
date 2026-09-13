package ui

import (
	"testing"

	"github.com/fikua/fikua-starflux/internal/period"
)

func TestPlotPeriodogram_rendersImage(t *testing.T) {
	points := []period.PeriodogramPoint{
		{Period: 0.5, Power: 0.1},
		{Period: 1.0, Power: 0.8},
		{Period: 1.5, Power: 0.2},
	}

	img, err := PlotPeriodogram(points, "VAR-1")
	if err != nil {
		t.Fatalf("PlotPeriodogram: %v", err)
	}
	if img == nil {
		t.Fatal("PlotPeriodogram returned a nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("got image bounds %v, want positive width and height", b)
	}
}

func TestPlotPeriodogram_emptyPoints(t *testing.T) {
	if _, err := PlotPeriodogram(nil, "empty"); err == nil {
		t.Fatal("expected an error for an empty point set")
	}
}
