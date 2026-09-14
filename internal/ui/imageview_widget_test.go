package ui

import (
	"image"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestStarRole_String(t *testing.T) {
	cases := []struct {
		role StarRole
		want string
	}{
		{RoleTarget, "Target"},
		{RoleComparison, "Comparison"},
		{RoleCheck, "Check"},
		{StarRole(99), "Unknown"},
	}
	for _, c := range cases {
		if got := c.role.String(); got != c.want {
			t.Errorf("StarRole(%d).String() = %q, want %q", c.role, got, c.want)
		}
	}
}

func TestMarkerColor_isDistinctPerRole(t *testing.T) {
	target := markerColor(RoleTarget)
	comp := markerColor(RoleComparison)
	check := markerColor(RoleCheck)
	if target == comp || target == check || comp == check {
		t.Error("expected each star role to have a visually distinct marker color")
	}
}

func TestImageView_setImageAndStars(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()
	w.Resize(fyne.NewSize(400, 300))

	img := image.NewGray(image.Rect(0, 0, 100, 50))
	v.SetImage(img)

	if v.imgW != 100 || v.imgH != 50 {
		t.Errorf("got imgW/imgH %d/%d, want 100/50", v.imgW, v.imgH)
	}

	v.SetStars([]Star{
		{Name: "VAR-1", Role: RoleTarget, X: 10, Y: 10},
		{Name: "CONTROL", Role: RoleComparison, X: 20, Y: 20},
	})
	if got := len(v.overlay.Objects); got != 4 { // 2 circles + 2 labels
		t.Errorf("got %d overlay objects after 2 stars, want 4", got)
	}

	v.SetStars(nil)
	if got := len(v.overlay.Objects); got != 0 {
		t.Errorf("got %d overlay objects after SetStars(nil), want 0", got)
	}
}

func TestImageView_setStarsReplacesPreviousSet(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()
	w.Resize(fyne.NewSize(400, 300))
	v.SetImage(image.NewGray(image.Rect(0, 0, 100, 50)))

	v.SetStars([]Star{
		{Name: "VAR-1", Role: RoleTarget, X: 10, Y: 10},
		{Name: "CONTROL", Role: RoleComparison, X: 20, Y: 20},
	})
	if got := len(v.overlay.Objects); got != 4 {
		t.Fatalf("got %d overlay objects after first SetStars, want 4", got)
	}

	// A second SetStars call with a single star must fully replace the
	// overlay, not add to it — this is the regression test for the old
	// AddMarker behavior, which always appended and left visually
	// orphaned circles for stars no longer tracked by the caller.
	v.SetStars([]Star{{Name: "VAR-1", Role: RoleTarget, X: 15, Y: 15}})
	if got := len(v.overlay.Objects); got != 2 {
		t.Errorf("got %d overlay objects after second SetStars, want 2 (old markers must not linger)", got)
	}
}

// TestImageView_markersRepositionOnResize is the regression test for the
// bug where marker circles kept their screen position from whenever
// SetImage/SetStars was last called, visually drifting off the real stars
// once the user resized the window afterward. It places a star at one
// widget size, resizes with NO further SetImage/SetStars call, and asserts
// the marker moves to match positionMarker's formula at the NEW size.
func TestImageView_markersRepositionOnResize(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()

	initialSize := fyne.NewSize(400, 300)
	w.Resize(initialSize)
	v.Resize(initialSize)

	v.SetImage(image.NewGray(image.Rect(0, 0, 100, 50)))
	v.SetStars([]Star{
		{Name: "VAR-1", Role: RoleTarget, X: 10, Y: 10},
	})

	findCircle := func() *canvas.Circle {
		for _, o := range v.overlay.Objects {
			if c, ok := o.(*canvas.Circle); ok {
				return c
			}
		}
		t.Fatal("no circle found in overlay")
		return nil
	}
	wantPos := func(size fyne.Size) fyne.Position {
		scale := imageDisplayScale(size, v.imgW, v.imgH)
		offsetX, offsetY := imageDisplayOffset(size, v.imgW, v.imgH, scale)
		const r = 10
		cx := offsetX + float32(10)*scale // star X=10
		cy := offsetY + float32(10)*scale // star Y=10
		return fyne.NewPos(cx-r, cy-r)
	}

	if got, want := findCircle().Position(), wantPos(initialSize); got != want {
		t.Fatalf("marker position before resize = %v, want %v", got, want)
	}

	// The regression scenario: resize AFTER markers are already placed,
	// with no further SetImage/SetStars call.
	newSize := fyne.NewSize(800, 600)
	w.Resize(newSize)
	v.Resize(newSize)

	got := findCircle().Position()
	want := wantPos(newSize)
	if got != want {
		t.Errorf("marker position after resize = %v, want %v (marker did not follow the resize)", got, want)
	}
	if oldPos := wantPos(initialSize); got == oldPos {
		t.Error("marker still at the OLD size's position after resize — overlay was never re-laid-out")
	}
}

func TestImageView_tappedReportsImagePixelCoords(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()

	// A 100x100 square image inside a 200x200 window with
	// ImageFillContain centers it and scales 1:1 (200/100 would only
	// apply if both dims matched; use a size where scale is exactly 2).
	v.SetImage(image.NewGray(image.Rect(0, 0, 100, 100)))
	w.Resize(fyne.NewSize(200, 200))
	v.Resize(fyne.NewSize(200, 200))

	var gotX, gotY float64
	called := false
	v.OnTap = func(x, y float64) {
		gotX, gotY = x, y
		called = true
	}

	// Tap the widget's center; with a 100x100 image scaled to fill a
	// 200x200 view (scale=2), the center of the screen maps to the
	// center of the image (50, 50).
	test.TapAt(v, fyne.NewPos(100, 100))

	if !called {
		t.Fatal("expected OnTap to be called")
	}
	if gotX < 49 || gotX > 51 || gotY < 49 || gotY > 51 {
		t.Errorf("got tap coords (%.1f, %.1f), want ~(50, 50)", gotX, gotY)
	}
}

func TestImageView_tappedIgnoredWithoutImage(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()
	w.Resize(fyne.NewSize(200, 200))

	called := false
	v.OnTap = func(x, y float64) { called = true }

	test.TapAt(v, fyne.NewPos(100, 100))

	if called {
		t.Error("expected OnTap not to be called when no image is loaded")
	}
}

func TestImageView_loupeFollowsCursorOverImage(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()

	v.SetImage(image.NewGray(image.Rect(0, 0, 100, 100)))
	w.Resize(fyne.NewSize(200, 200))
	v.Resize(fyne.NewSize(200, 200))

	if !v.loupe.Hidden {
		t.Fatal("expected loupe to start hidden before any hover")
	}

	v.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(100, 100)}})
	if v.loupe.Hidden {
		t.Error("expected loupe to be visible after MouseIn over the image")
	}
	if v.loupe.Image == nil {
		t.Error("expected loupe.Image to be set after MouseIn over the image")
	}

	v.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(120, 80)}})
	if v.loupe.Hidden {
		t.Error("expected loupe to remain visible after MouseMoved over the image")
	}

	v.MouseOut()
	if !v.loupe.Hidden {
		t.Error("expected loupe to hide after MouseOut")
	}
}

func TestImageView_loupeStaysHiddenWithoutImage(t *testing.T) {
	v := NewImageView()
	w := test.NewWindow(v)
	defer w.Close()
	w.Resize(fyne.NewSize(200, 200))
	v.Resize(fyne.NewSize(200, 200))

	v.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(100, 100)}})
	if !v.loupe.Hidden {
		t.Error("expected loupe to stay hidden when no image is loaded")
	}
}

func TestObserverTheme_allNamedColorsAreRedOrBlack(t *testing.T) {
	th := ObserverTheme{}

	// Every explicitly-overridden color must be a shade of red/black
	// (green and blue components at or below the red component), so the
	// theme never accidentally leaks a color that would ruin night
	// vision at the telescope.
	names := []fyne.ThemeColorName{
		theme.ColorNameBackground,
		theme.ColorNameForeground,
		theme.ColorNameButton,
		theme.ColorNameInputBackground,
		theme.ColorNamePrimary,
		theme.ColorNameHover,
		theme.ColorNameFocus,
		theme.ColorNameDisabled,
		theme.ColorNameDisabledButton,
		theme.ColorNamePlaceHolder,
		theme.ColorNameScrollBar,
		theme.ColorNameSeparator,
	}
	for _, name := range names {
		c := th.Color(name, theme.VariantDark)
		nrgba, ok := c.(color.NRGBA)
		if !ok {
			t.Errorf("Color(%s) = %T, want color.NRGBA", name, c)
			continue
		}
		if nrgba.G > nrgba.R || nrgba.B > nrgba.R {
			t.Errorf("Color(%s) = %+v is not red-dominant (green/blue exceed red)", name, nrgba)
		}
	}
}

func TestObserverTheme_unknownColorFallsBackToDarkTheme(t *testing.T) {
	th := ObserverTheme{}
	got := th.Color("some-unknown-color-name", theme.VariantDark)
	want := theme.DarkTheme().Color("some-unknown-color-name", theme.VariantDark)
	if got != want {
		t.Errorf("Color(unknown) = %v, want dark theme fallback %v", got, want)
	}
}

func TestObserverTheme_delegatesFontIconSizeToDarkTheme(t *testing.T) {
	th := ObserverTheme{}
	dark := theme.DarkTheme()

	if got, want := th.Font(fyne.TextStyle{}), dark.Font(fyne.TextStyle{}); got != want {
		t.Errorf("Font() = %v, want dark theme's %v", got, want)
	}
	if got, want := th.Icon(theme.IconNameHome), dark.Icon(theme.IconNameHome); got != want {
		t.Errorf("Icon() = %v, want dark theme's %v", got, want)
	}
	if got, want := th.Size(theme.SizeNameText), dark.Size(theme.SizeNameText); got != want {
		t.Errorf("Size() = %v, want dark theme's %v", got, want)
	}
}
