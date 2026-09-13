package ui

import (
	"fmt"
	"image"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"github.com/fikua/fikua-starflux/internal/period"
)

// periodogramPoints adapts []period.PeriodogramPoint to plotter.XYer.
type periodogramPoints []period.PeriodogramPoint

func (p periodogramPoints) Len() int { return len(p) }

func (p periodogramPoints) XY(i int) (x, y float64) {
	return p[i].Period, p[i].Power
}

// PlotPeriodogram renders a Lomb-Scargle periodogram (trial period vs.
// normalized power) as a static raster image, mirroring PlotLightCurve's
// rendering approach. X is period, not frequency, since users think in
// physically meaningful time units. label is used as the plot title (e.g.
// the target star's name).
func PlotPeriodogram(points []period.PeriodogramPoint, label string) (image.Image, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("ui: PlotPeriodogram: no points to plot")
	}

	p := plot.New()
	p.Title.Text = label + " — Lomb-Scargle periodogram"
	p.X.Label.Text = "Trial period (days)"
	p.Y.Label.Text = "Normalized power"

	data := periodogramPoints(points)

	line, err := plotter.NewLine(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotPeriodogram: line: %w", err)
	}
	p.Add(line)

	const dpi = 96
	widthIn, heightIn := 8.0, 5.0
	canvas := vgimg.NewWith(vgimg.UseWH(vg.Length(widthIn)*vg.Inch, vg.Length(heightIn)*vg.Inch), vgimg.UseDPI(dpi))
	p.Draw(draw.New(canvas))

	return canvas.Image(), nil
}
