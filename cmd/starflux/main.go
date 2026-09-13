package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

var fitsFileFilter = zenity.FileFilter{
	Name:     "FITS images",
	Patterns: []string{"*.fits", "*.fit", "*.fts"},
	CaseFold: true,
}

// appState holds the session's mutable state, shared across the window's
// widgets.
type appState struct {
	win    fyne.Window
	view   *ui.ImageView
	levels ui.Levels

	series  []*fits.Image // the loaded session; markers are placed on series[0] and applied to all
	role    ui.StarRole
	markers map[ui.StarRole]ui.Marker

	seriesLabel *widget.Label
}

func newAppState(win fyne.Window) *appState {
	return &appState{
		win:         win,
		view:        ui.NewImageView(),
		levels:      ui.Levels{Background: 0, Range: 65535},
		role:        ui.RoleTarget,
		markers:     map[ui.StarRole]ui.Marker{},
		seriesLabel: widget.NewLabel("No images loaded"),
	}
}

func (s *appState) redraw() {
	if len(s.series) == 0 {
		return
	}
	s.view.SetImage(ui.Render(s.series[0], s.levels))
}

func (s *appState) clearMarkers() {
	s.view.ClearMarkers()
	for k := range s.markers {
		delete(s.markers, k)
	}
}

// loadSeries replaces the loaded session. err is a fatal, load-aborting
// error (e.g. no files found); loadErrs are per-file failures within an
// otherwise successful batch, which are reported but don't block using the
// files that did load.
func (s *appState) loadSeries(loaded []*fits.Image, loadErrs []fits.LoadError, err error) {
	if err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	s.series = loaded
	s.clearMarkers()
	if len(s.series) == 1 {
		s.seriesLabel.SetText(fmt.Sprintf("Loaded 1 image: %s", filepath.Base(s.series[0].Path)))
	} else {
		s.seriesLabel.SetText(fmt.Sprintf("Loaded %d images, showing %s", len(s.series), filepath.Base(s.series[0].Path)))
	}
	s.redraw()

	if len(loadErrs) > 0 {
		showLoadErrors(s.win, loadErrs)
	}
}

// showLoadErrors lists the files that failed to load within a batch, so the
// user knows exactly which ones to check instead of guessing whether a
// series loaded completely.
func showLoadErrors(parent fyne.Window, loadErrs []fits.LoadError) {
	lines := make([]string, len(loadErrs))
	for i, le := range loadErrs {
		lines[i] = fmt.Sprintf("%s: %v", filepath.Base(le.Path), le.Err)
	}
	msg := fmt.Sprintf("%d file(s) failed to load and were skipped:\n\n%s", len(loadErrs), strings.Join(lines, "\n"))
	dialog.ShowInformation("Some files failed to load", msg, parent)
}

func main() {
	a := app.New()
	w := a.NewWindow("Starflux")
	s := newAppState(w)

	s.view.OnTap = func(x, y float64) {
		m := ui.Marker{Role: s.role, X: x, Y: y}
		s.markers[s.role] = m
		s.view.AddMarker(m)
	}

	controls := container.NewVBox(
		widget.NewButton("Open FITS...", s.openFileAction),
		widget.NewButton("Open Files...", s.openFilesAction),
		widget.NewButton("Open Folder...", s.openFolderAction),
		s.seriesLabel,
		widget.NewLabel("Star role:"),
		newRoleSelect(s),
		widget.NewButton("Clear markers", s.clearMarkers),
		widget.NewLabel("Background:"),
		newLevelSlider(0, 65535, s.levels.Background, func(v float64) { s.levels.Background = v; s.redraw() }),
		widget.NewLabel("Range:"),
		newLevelSlider(1, 65535, s.levels.Range, func(v float64) { s.levels.Range = v; s.redraw() }),
		widget.NewButton("Process", s.processAction),
		newObserverModeCheck(a),
	)

	content := container.NewBorder(nil, nil, nil, controls, s.view)
	w.SetContent(content)
	w.Resize(fyne.NewSize(1000, 700))
	w.ShowAndRun()
}

func newRoleSelect(s *appState) *widget.Select {
	sel := widget.NewSelect(
		[]string{ui.RoleTarget.String(), ui.RoleComparison.String(), ui.RoleCheck.String()},
		func(text string) {
			switch text {
			case ui.RoleComparison.String():
				s.role = ui.RoleComparison
			case ui.RoleCheck.String():
				s.role = ui.RoleCheck
			default:
				s.role = ui.RoleTarget
			}
		},
	)
	sel.SetSelectedIndex(0)
	return sel
}

func newLevelSlider(min, max, initial float64, onChanged func(float64)) *widget.Slider {
	slider := widget.NewSlider(min, max)
	slider.Value = initial
	slider.OnChanged = onChanged
	return slider
}

func newObserverModeCheck(a fyne.App) *widget.Check {
	return widget.NewCheck("Observer mode (red light)", func(on bool) {
		if on {
			a.Settings().SetTheme(ui.ObserverTheme{})
		} else {
			a.Settings().SetTheme(theme.DefaultTheme())
		}
	})
}

func (s *appState) openFileAction() {
	go func() {
		path, err := zenity.SelectFile(zenity.FileFilters{fitsFileFilter})
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				s.loadSeries(nil, nil, err)
				return
			}
			img, err := fits.Load(path)
			if err != nil {
				s.loadSeries(nil, nil, err)
				return
			}
			s.loadSeries([]*fits.Image{img}, nil, nil)
		})
	}()
}

func (s *appState) openFilesAction() {
	go func() {
		paths, err := zenity.SelectFileMultiple(zenity.FileFilters{fitsFileFilter})
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				s.loadSeries(nil, nil, err)
				return
			}
			s.loadSeries(fits.LoadFiles(paths))
		})
	}()
}

func (s *appState) openFolderAction() {
	go func() {
		path, err := zenity.SelectFile(zenity.Directory())
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				s.loadSeries(nil, nil, err)
				return
			}
			s.loadSeries(fits.LoadDir(path))
		})
	}()
}

func (s *appState) processAction() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", "Open a FITS image or folder first.", s.win)
		return
	}
	target, hasTarget := s.markers[ui.RoleTarget]
	comp, hasComp := s.markers[ui.RoleComparison]
	if !hasTarget || !hasComp {
		dialog.ShowInformation("Starflux", "Place both a Target and a Comparison marker first.", s.win)
		return
	}

	obs, err := s.measureSeries(target, comp)
	if err != nil {
		dialog.ShowError(err, s.win)
		return
	}

	points, errs := timeseries.Series(obs)
	if len(points) == 0 {
		dialog.ShowError(fmt.Errorf("no valid measurements across %d image(s): %v", len(s.series), errs), s.win)
		return
	}

	showLightCurve(s.win, points, "Target")
}

// measureSeries runs aperture photometry for the target and comparison
// markers on every image in the loaded series.
func (s *appState) measureSeries(target, comp ui.Marker) ([]timeseries.Observation, error) {
	obs := make([]timeseries.Observation, 0, len(s.series))
	for _, img := range s.series {
		targetRes, err := photometry.Measure(img, int(target.X), int(target.Y), centroidHalfWidth, defaultAperture)
		if err != nil {
			return nil, fmt.Errorf("%s: target: %w", img.Path, err)
		}
		compRes, err := photometry.Measure(img, int(comp.X), int(comp.Y), centroidHalfWidth, defaultAperture)
		if err != nil {
			return nil, fmt.Errorf("%s: comparison: %w", img.Path, err)
		}
		obs = append(obs, timeseries.Observation{
			JD:     img.JD,
			Target: targetRes,
			Comp:   compRes,
		})
	}
	return obs, nil
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
