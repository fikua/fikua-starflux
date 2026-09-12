package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/fikua/fikua-starflux/internal/fits"
	"github.com/fikua/fikua-starflux/internal/photometry"
	"github.com/fikua/fikua-starflux/internal/timeseries"
	"github.com/fikua/fikua-starflux/internal/ui"
)

const centroidHalfWidth = 10

var defaultAperture = photometry.Aperture{R: 6, RIn: 10, ROut: 15}

func main() {
	a := app.New()
	w := a.NewWindow("Starflux")

	view := ui.NewImageView()
	levels := ui.Levels{Background: 0, Range: 65535}
	var series []*fits.Image // the loaded session; markers are placed on series[0] and applied to all
	role := ui.RoleTarget
	markers := map[ui.StarRole]ui.Marker{}

	redraw := func() {
		if len(series) == 0 {
			return
		}
		view.SetImage(ui.Render(series[0], levels))
	}

	view.OnTap = func(x, y float64) {
		m := ui.Marker{Role: role, X: x, Y: y}
		markers[role] = m
		view.AddMarker(m)
	}

	roleSelect := widget.NewSelect(
		[]string{ui.RoleTarget.String(), ui.RoleComparison.String(), ui.RoleCheck.String()},
		func(s string) {
			switch s {
			case ui.RoleComparison.String():
				role = ui.RoleComparison
			case ui.RoleCheck.String():
				role = ui.RoleCheck
			default:
				role = ui.RoleTarget
			}
		},
	)
	roleSelect.SetSelectedIndex(0)

	clearButton := widget.NewButton("Clear markers", func() {
		view.ClearMarkers()
		for k := range markers {
			delete(markers, k)
		}
	})

	bgSlider := widget.NewSlider(0, 65535)
	bgSlider.Value = levels.Background
	bgSlider.OnChanged = func(v float64) {
		levels.Background = v
		redraw()
	}

	rangeSlider := widget.NewSlider(1, 65535)
	rangeSlider.Value = levels.Range
	rangeSlider.OnChanged = func(v float64) {
		levels.Range = v
		redraw()
	}

	loadSeries := func(loaded []*fits.Image, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		series = loaded
		view.ClearMarkers()
		for k := range markers {
			delete(markers, k)
		}
		redraw()
	}

	fitsFilter := zenity.FileFilter{
		Name:     "FITS images",
		Patterns: []string{"*.fits", "*.fit", "*.fts"},
		CaseFold: true,
	}

	openFileButton := widget.NewButton("Open FITS...", func() {
		go func() {
			path, err := zenity.SelectFile(zenity.FileFilters{fitsFilter})
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() {
				if err != nil {
					loadSeries(nil, err)
					return
				}
				img, err := fits.Load(path)
				if err != nil {
					loadSeries(nil, err)
					return
				}
				loadSeries([]*fits.Image{img}, nil)
			})
		}()
	})

	openFilesButton := widget.NewButton("Open Files...", func() {
		go func() {
			paths, err := zenity.SelectFileMultiple(zenity.FileFilters{fitsFilter})
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() {
				if err != nil {
					loadSeries(nil, err)
					return
				}
				loadSeries(fits.LoadFiles(paths))
			})
		}()
	})

	openFolderButton := widget.NewButton("Open Folder...", func() {
		go func() {
			path, err := zenity.SelectFile(zenity.Directory())
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() {
				if err != nil {
					loadSeries(nil, err)
					return
				}
				loadSeries(fits.LoadDir(path))
			})
		}()
	})

	processButton := widget.NewButton("Process", func() {
		if len(series) == 0 {
			dialog.ShowInformation("Starflux", "Open a FITS image or folder first.", w)
			return
		}
		target, hasTarget := markers[ui.RoleTarget]
		comp, hasComp := markers[ui.RoleComparison]
		if !hasTarget || !hasComp {
			dialog.ShowInformation("Starflux", "Place both a Target and a Comparison marker first.", w)
			return
		}

		obs := make([]timeseries.Observation, 0, len(series))
		for _, img := range series {
			targetRes, err := photometry.Measure(img, int(target.X), int(target.Y), centroidHalfWidth, defaultAperture)
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s: target: %w", img.Path, err), w)
				return
			}
			compRes, err := photometry.Measure(img, int(comp.X), int(comp.Y), centroidHalfWidth, defaultAperture)
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s: comparison: %w", img.Path, err), w)
				return
			}
			obs = append(obs, timeseries.Observation{
				JD:     img.JD,
				Target: targetRes,
				Comp:   compRes,
			})
		}

		points, errs := timeseries.Series(obs)
		if len(points) == 0 {
			dialog.ShowError(fmt.Errorf("no valid measurements across %d image(s): %v", len(series), errs), w)
			return
		}

		showLightCurve(w, points, "Target")
	})

	observerMode := widget.NewCheck("Observer mode (red light)", func(on bool) {
		if on {
			a.Settings().SetTheme(ui.ObserverTheme{})
		} else {
			a.Settings().SetTheme(theme.DefaultTheme())
		}
	})

	controls := container.NewVBox(
		openFileButton,
		openFilesButton,
		openFolderButton,
		widget.NewLabel("Star role:"),
		roleSelect,
		clearButton,
		widget.NewLabel("Background:"),
		bgSlider,
		widget.NewLabel("Range:"),
		rangeSlider,
		processButton,
		observerMode,
	)

	content := container.NewBorder(nil, nil, nil, controls, view)
	w.SetContent(content)
	w.Resize(fyne.NewSize(1000, 700))
	w.ShowAndRun()
}

// showLightCurve renders a differential-magnitude light curve and displays
// it in a new dialog window.
func showLightCurve(parent fyne.Window, points []timeseries.Point, label string) {
	img, err := ui.PlotLightCurve(points, label)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}

	last := points[len(points)-1]
	summary := widget.NewLabel(fmt.Sprintf("Δm = %.4f ± %.4f", last.DiffMag, last.DiffMagErr))
	plotImg := canvas.NewImageFromImage(img)
	plotImg.FillMode = canvas.ImageFillContain
	plotImg.SetMinSize(fyne.NewSize(500, 320))

	d := dialog.NewCustom("Light curve", "Close", container.NewVBox(summary, plotImg), parent)
	d.Show()
}
