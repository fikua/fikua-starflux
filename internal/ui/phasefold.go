package ui

import (
	"fmt"
	"image"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"gonum.org/v1/plot/plotter"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

// PlotPhaseFold renders a phase-folded light curve (phase in [0,1) vs.
// differential magnitude, with error bars), mirroring PlotLightCurve's
// exact scatter + error-bar approach and inverted Y-axis convention. points
// must already be phase-folded (e.g. via period.FoldPhase, whose JD field
// holds phase, not a Julian Date) — this function does not fold.
func PlotPhaseFold(points []timeseries.Point, periodDays float64, label string) (image.Image, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("ui: PlotPhaseFold: no points to plot")
	}

	p := plot.New()
	p.Title.Text = fmt.Sprintf("%s — phase-folded at P=%.6f d", label, periodDays)
	p.X.Label.Text = "Phase"
	p.Y.Label.Text = "Differential magnitude (Δm)"
	p.Y.Min, p.Y.Max = axisRangeInverted(points)

	data := pointErrs(points)

	scatter, err := plotter.NewScatter(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotPhaseFold: scatter: %w", err)
	}
	scatter.GlyphStyle.Radius = vg.Points(2)

	errBars, err := plotter.NewYErrorBars(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotPhaseFold: error bars: %w", err)
	}

	p.Add(scatter, errBars)

	const dpi = 96
	widthIn, heightIn := 8.0, 5.0
	canvas := vgimg.NewWith(vgimg.UseWH(vg.Length(widthIn)*vg.Inch, vg.Length(heightIn)*vg.Inch), vgimg.UseDPI(dpi))
	p.Draw(draw.New(canvas))

	return canvas.Image(), nil
}
