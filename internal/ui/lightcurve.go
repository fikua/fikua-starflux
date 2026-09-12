package ui

import (
	"fmt"
	"image"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

// pointErrs adapts a []timeseries.Point to plotter.XYer + plotter.YErrorer,
// so gonum/plot can draw the differential-magnitude light curve with error
// bars in one pass, mirroring FotoDif's "Magnitudes" tab.
type pointErrs []timeseries.Point

func (p pointErrs) Len() int { return len(p) }

func (p pointErrs) XY(i int) (x, y float64) {
	return p[i].JD, p[i].DiffMag
}

func (p pointErrs) YError(i int) (low, high float64) {
	return p[i].DiffMagErr, p[i].DiffMagErr
}

// PlotLightCurve renders a differential-magnitude light curve (JD vs. Δm,
// with error bars) as a static raster image, matching FotoDif's behavior
// of non-interactive plots. label is used as the plot title (e.g. the
// target star's name).
func PlotLightCurve(points []timeseries.Point, label string) (image.Image, error) {
	if len(points) == 0 {
		return nil, fmt.Errorf("ui: PlotLightCurve: no points to plot")
	}

	p := plot.New()
	p.Title.Text = label
	p.X.Label.Text = "Julian Date"
	p.Y.Label.Text = "Differential magnitude (Δm)"
	// Brighter (more negative magnitude) should read as "up"; invert the
	// axis so the plot reads the way astronomers expect.
	p.Y.Min, p.Y.Max = axisRangeInverted(points)

	data := pointErrs(points)

	scatter, err := plotter.NewScatter(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotLightCurve: scatter: %w", err)
	}
	scatter.GlyphStyle.Radius = vg.Points(2)

	errBars, err := plotter.NewYErrorBars(data)
	if err != nil {
		return nil, fmt.Errorf("ui: PlotLightCurve: error bars: %w", err)
	}

	p.Add(scatter, errBars)

	const dpi = 96
	widthIn, heightIn := 8.0, 5.0
	canvas := vgimg.NewWith(vgimg.UseWH(vg.Length(widthIn)*vg.Inch, vg.Length(heightIn)*vg.Inch), vgimg.UseDPI(dpi))
	p.Draw(draw.New(canvas))

	return canvas.Image(), nil
}

// axisRangeInverted returns (max, min) instead of (min, max) so the Y axis
// runs from faint (bottom) to bright (top) when used as p.Y.Min/p.Y.Max.
func axisRangeInverted(points []timeseries.Point) (min, max float64) {
	lo, hi := points[0].DiffMag, points[0].DiffMag
	for _, pt := range points {
		if pt.DiffMag < lo {
			lo = pt.DiffMag
		}
		if pt.DiffMag > hi {
			hi = pt.DiffMag
		}
	}
	margin := (hi - lo) * 0.1
	if margin == 0 {
		margin = 0.01
	}
	return hi + margin, lo - margin
}
