package ui

import (
	"testing"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

func TestPlotPhaseFold_rendersImage(t *testing.T) {
	points := []timeseries.Point{
		{JD: 0.1, DiffMag: -0.10, DiffMagErr: 0.01},
		{JD: 0.4, DiffMag: -0.05, DiffMagErr: 0.01},
		{JD: 0.8, DiffMag: -0.15, DiffMagErr: 0.02},
	}

	img, err := PlotPhaseFold(points, 0.7, "VAR-1")
	if err != nil {
		t.Fatalf("PlotPhaseFold: %v", err)
	}
	if img == nil {
		t.Fatal("PlotPhaseFold returned a nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("got image bounds %v, want positive width and height", b)
	}
}

func TestPlotPhaseFold_emptyPoints(t *testing.T) {
	if _, err := PlotPhaseFold(nil, 1.0, "empty"); err == nil {
		t.Fatal("expected an error for an empty point set")
	}
}
