package ui

import (
	"testing"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

func TestPlotLightCurve_rendersImage(t *testing.T) {
	points := []timeseries.Point{
		{JD: 2460000.1, DiffMag: -0.10, DiffMagErr: 0.01},
		{JD: 2460000.2, DiffMag: -0.05, DiffMagErr: 0.01},
		{JD: 2460000.3, DiffMag: -0.15, DiffMagErr: 0.02},
	}

	img, err := PlotLightCurve(points, "VAR-1")
	if err != nil {
		t.Fatalf("PlotLightCurve: %v", err)
	}
	if img == nil {
		t.Fatal("PlotLightCurve returned a nil image")
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("got image bounds %v, want positive width and height", b)
	}
}

func TestPlotLightCurve_emptyPoints(t *testing.T) {
	if _, err := PlotLightCurve(nil, "empty"); err == nil {
		t.Fatal("expected an error for an empty point set")
	}
}

func TestAxisRangeInverted_bracketsData(t *testing.T) {
	points := []timeseries.Point{
		{DiffMag: -0.2},
		{DiffMag: 0.1},
		{DiffMag: -0.05},
	}
	max, min := axisRangeInverted(points)
	if max <= -0.2+ /*margin*/ 0 || max < 0.1 {
		t.Errorf("got max %v, want it above the brightest (most negative) point with margin", max)
	}
	if min > -0.2 {
		t.Errorf("got min %v, want it below the faintest point with margin", min)
	}
	if max <= min {
		t.Errorf("got max %v <= min %v, want max > min (inverted axis)", max, min)
	}
}
