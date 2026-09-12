package ui

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestImageDisplayScale_widerWidget(t *testing.T) {
	// A 400x200 image in a 800x300 widget: height is the limiting
	// dimension (300/200 = 1.5) vs width (800/400 = 2), so scale = 1.5.
	got := imageDisplayScale(fyne.NewSize(800, 300), 400, 200)
	if got != 1.5 {
		t.Errorf("got scale %v, want 1.5", got)
	}
}

func TestImageDisplayScale_tallerWidget(t *testing.T) {
	// Width is the limiting dimension (100/200 = 0.5) vs height
	// (400/200 = 2), so scale = 0.5.
	got := imageDisplayScale(fyne.NewSize(100, 400), 200, 200)
	if got != 0.5 {
		t.Errorf("got scale %v, want 0.5", got)
	}
}

func TestImageDisplayOffset_centersImage(t *testing.T) {
	// 400x200 image at scale 1.5 -> displayed 600x300, inside an 800x300
	// widget: horizontal offset (800-600)/2=100, vertical offset 0.
	x, y := imageDisplayOffset(fyne.NewSize(800, 300), 400, 200, 1.5)
	if x != 100 || y != 0 {
		t.Errorf("got offset (%v, %v), want (100, 0)", x, y)
	}
}

func TestImageDisplayScale_zeroImageDimensionsIsSafe(t *testing.T) {
	if got := imageDisplayScale(fyne.NewSize(100, 100), 0, 0); got != 1 {
		t.Errorf("got scale %v, want 1 (safe default)", got)
	}
}
