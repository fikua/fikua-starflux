package ui

import (
	"fmt"
	"image"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// DriftPoint is one frame's tracked-star displacement, for PlotDrift.
type DriftPoint struct {
	JD         float64
	DistancePx float64
}

// driftPoints adapts []DriftPoint to plotter.XYer.
type driftPoints []DriftPoint

func (d driftPoints) Len() int { return len(d) }

func (d driftPoints) XY(i int) (x, y float64) {
	return d[i].JD, d[i].DistancePx
}

// PlotDrift renders a per-frame star-tracking displacement plot (JD vs.
// pixel distance moved since the previous frame) as a static raster image,
// mirroring PlotLightCurve's rendering approach. label is used as the plot
// title (e.g. the target star's name).
func PlotDrift(points []DriftPoint, label string) (image.Image, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("ui: PlotDrift: no points to plot")
	}

	p := plot.New()
	p.Title.Text = label + " — tracking drift"
	p.X.Label.Text = "Julian Date"
	p.Y.Label.Text = "Frame-to-frame displacement (px)"

	data := driftPoints(points)

	line, err := plotter.NewLine(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotDrift: line: %w", err)
	}
	scatter, err := plotter.NewScatter(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotDrift: scatter: %w", err)
	}
	scatter.GlyphStyle.Radius = vg.Points(2)
	p.Add(line, scatter)

	const dpi = 96
	widthIn, heightIn := 8.0, 5.0
	canvas := vgimg.NewWith(vgimg.UseWH(vg.Length(widthIn)*vg.Inch, vg.Length(heightIn)*vg.Inch), vgimg.UseDPI(dpi))
	p.Draw(draw.New(canvas))

	return canvas.Image(), nil
}
