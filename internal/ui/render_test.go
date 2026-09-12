package ui

import "testing"

type testGrid struct {
	w, h int
	data []float64
}

func (g *testGrid) Width() int  { return g.w }
func (g *testGrid) Height() int { return g.h }
func (g *testGrid) At(x, y int) float64 {
	return g.data[y*g.w+x]
}

func TestRender_linearMapping(t *testing.T) {
	g := &testGrid{w: 3, h: 1, data: []float64{0, 500, 1200}}
	out := Render(g, Levels{Background: 0, Range: 1000})

	cases := []struct {
		x    int
		want uint8
	}{
		{0, 0},   // at background -> black
		{1, 127}, // mid-range -> mid-gray (500/1000*255 = 127.5, truncated)
		{2, 255}, // above background+range -> clamped white
	}
	for _, c := range cases {
		got := out.GrayAt(c.x, 0).Y
		if got != c.want {
			t.Errorf("pixel %d: got gray %d, want %d", c.x, got, c.want)
		}
	}
}

func TestRender_belowBackgroundClampsToBlack(t *testing.T) {
	g := &testGrid{w: 1, h: 1, data: []float64{-50}}
	out := Render(g, Levels{Background: 0, Range: 1000})
	if got := out.GrayAt(0, 0).Y; got != 0 {
		t.Errorf("got gray %d, want 0 (clamped)", got)
	}
}

func TestRender_zeroRangeDoesNotPanic(t *testing.T) {
	g := &testGrid{w: 1, h: 1, data: []float64{100}}
	out := Render(g, Levels{Background: 0, Range: 0})
	_ = out.GrayAt(0, 0)
}

func TestRender_dimensions(t *testing.T) {
	g := &testGrid{w: 4, h: 2, data: make([]float64, 8)}
	out := Render(g, Levels{Background: 0, Range: 100})
	b := out.Bounds()
	if b.Dx() != 4 || b.Dy() != 2 {
		t.Errorf("got dimensions %dx%d, want 4x2", b.Dx(), b.Dy())
	}
}
