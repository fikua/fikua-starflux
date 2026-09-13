package ui

import (
	"image"
	"image/color"
	"testing"
)

func TestCropImage_withinBounds(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 100))
	crop, ok := cropImage(src, 10, 10, 30, 30)
	if !ok {
		t.Fatal("expected ok=true for a region within bounds")
	}
	b := crop.Bounds()
	if b.Dx() != 20 || b.Dy() != 20 {
		t.Errorf("got crop size %dx%d, want 20x20", b.Dx(), b.Dy())
	}
}

func TestCropImage_clampsToBounds(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 100))
	// Region straddling the top-left corner should be clamped, not error.
	crop, ok := cropImage(src, -10, -10, 10, 10)
	if !ok {
		t.Fatal("expected ok=true for a region straddling the image edge")
	}
	b := crop.Bounds()
	if b.Dx() != 10 || b.Dy() != 10 {
		t.Errorf("got clamped crop size %dx%d, want 10x10", b.Dx(), b.Dy())
	}
}

func TestCropImage_entirelyOutside(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 100))
	if _, ok := cropImage(src, 200, 200, 220, 220); ok {
		t.Error("expected ok=false for a region entirely outside the image")
	}
}

func TestCropImage_unsupportedType(t *testing.T) {
	// image.Uniform has no SubImage method.
	src := image.NewUniform(color.Black)
	if _, ok := cropImage(src, 0, 0, 10, 10); ok {
		t.Error("expected ok=false for a source type without SubImage")
	}
}

func TestMagnify_producesRequestedSize(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 10, 10))
	out := magnify(src, 160)
	b := out.Bounds()
	if b.Dx() != 160 || b.Dy() != 160 {
		t.Errorf("got magnified size %dx%d, want 160x160", b.Dx(), b.Dy())
	}
}
