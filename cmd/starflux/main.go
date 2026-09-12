package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/fikua/fikua-starflux/internal/fits"
	"github.com/fikua/fikua-starflux/internal/ui"
)

func main() {
	a := app.New()
	w := a.NewWindow("Starflux")

	view := ui.NewImageView()
	levels := ui.Levels{Background: 0, Range: 65535}
	var current *fits.Image
	role := ui.RoleTarget

	redraw := func() {
		if current == nil {
			return
		}
		view.SetImage(ui.Render(current, levels))
	}

	view.OnTap = func(x, y float64) {
		view.AddMarker(ui.Marker{Role: role, X: x, Y: y})
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

	openButton := widget.NewButton("Open FITS...", func() {
		d := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // user canceled
			}
			defer reader.Close()

			path := reader.URI().Path()
			img, err := fits.Load(path)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			current = img
			view.ClearMarkers()
			redraw()
		}, w)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".fits", ".fit", ".fts"}))
		d.Show()
	})

	controls := container.NewVBox(
		openButton,
		widget.NewLabel("Star role:"),
		roleSelect,
		clearButton,
		widget.NewLabel("Background:"),
		bgSlider,
		widget.NewLabel("Range:"),
		rangeSlider,
	)

	content := container.NewBorder(nil, nil, nil, controls, view)
	w.SetContent(content)
	w.Resize(fyne.NewSize(1000, 700))
	w.ShowAndRun()
}
