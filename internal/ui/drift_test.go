package ui

import "testing"

func TestPlotDrift_rendersImage(t *testing.T) {
	points := []DriftPoint{
		{JD: 2460000.1, DistancePx: 0.3},
		{JD: 2460000.2, DistancePx: 0.5},
		{JD: 2460000.3, DistancePx: 0.2},
	}

	img, err := PlotDrift(points, "VAR-1")
	if err != nil {
		t.Fatalf("PlotDrift: %v", err)
	}
	if img == nil {
		t.Fatal("PlotDrift returned a nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("got image bounds %v, want positive width and height", b)
	}
}

func TestPlotDrift_emptyPoints(t *testing.T) {
	if _, err := PlotDrift(nil, "empty"); err == nil {
		t.Fatal("expected an error for an empty point set")
	}
}
