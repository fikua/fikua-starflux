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
	"github.com/fikua/fikua-starflux/internal/session"
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

var starsFileFilter = zenity.FileFilter{
	Name:     "Starflux star positions",
	Patterns: []string{"*.json"},
	CaseFold: true,
}

// appState holds the session's mutable state, shared across the window's
// widgets.
type appState struct {
	win    fyne.Window
	view   *ui.ImageView
	levels ui.Levels

	series []*fits.Image // the loaded session; stars are placed on series[0] and applied to all
	stars  []ui.Star     // open list of named stars, multiple per role

	seriesLabel *widget.Label
	starsSelect *widget.Select // populated with current star names, for removal
}

func newAppState(win fyne.Window) *appState {
	return &appState{
		win:         win,
		view:        ui.NewImageView(),
		levels:      ui.Levels{Background: 0, Range: 65535},
		seriesLabel: widget.NewLabel("No images loaded"),
		starsSelect: widget.NewSelect(nil, nil),
	}
}

func (s *appState) redraw() {
	if len(s.series) == 0 {
		return
	}
	s.view.SetImage(ui.Render(s.series[0], s.levels))
}

func (s *appState) clearStars() {
	s.stars = nil
	s.view.SetStars(nil)
	s.refreshStarsSelect()
}

// refreshStarsSelect keeps the star-removal dropdown's options in sync with
// the current star list.
func (s *appState) refreshStarsSelect() {
	names := make([]string, len(s.stars))
	for i, st := range s.stars {
		names[i] = fmt.Sprintf("%s (%s)", st.Name, st.Role)
	}
	s.starsSelect.Options = names
	s.starsSelect.ClearSelected()
	s.starsSelect.Refresh()
}

// hasStarNamed reports whether a star with the given name already exists
// in the session (names must be unique).
func (s *appState) hasStarNamed(name string) bool {
	for _, st := range s.stars {
		if st.Name == name {
			return true
		}
	}
	return false
}

// addStar appends a new named star and refreshes the view and removal list.
func (s *appState) addStar(star ui.Star) {
	s.stars = append(s.stars, star)
	s.view.SetStars(s.stars)
	s.refreshStarsSelect()
}

// removeStarAt removes the star at the given index (as shown in
// starsSelect) and refreshes the view and removal list.
func (s *appState) removeStarAt(index int) {
	if index < 0 || index >= len(s.stars) {
		return
	}
	s.stars = append(s.stars[:index], s.stars[index+1:]...)
	s.view.SetStars(s.stars)
	s.refreshStarsSelect()
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
	s.clearStars()
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
		showStarDialog(s.win, func(name string, role ui.StarRole) {
			if s.hasStarNamed(name) {
				dialog.ShowInformation("Starflux", fmt.Sprintf("A star named %q already exists.", name), s.win)
				return
			}
			s.addStar(ui.Star{Name: name, Role: role, X: x, Y: y})
		})
	}

	removeButton := widget.NewButton("Remove star", func() {
		idx := s.starsSelect.SelectedIndex()
		if idx < 0 {
			dialog.ShowInformation("Starflux", "Select a star to remove first.", s.win)
			return
		}
		s.removeStarAt(idx)
	})

	controls := container.NewVBox(
		widget.NewButton("Open FITS...", s.openFileAction),
		widget.NewButton("Open Files...", s.openFilesAction),
		widget.NewButton("Open Folder...", s.openFolderAction),
		s.seriesLabel,
		widget.NewLabel("Stars:"),
		s.starsSelect,
		removeButton,
		widget.NewButton("Clear all stars", s.clearStars),
		widget.NewButton("Save stars...", s.saveStarsAction),
		widget.NewButton("Load stars...", s.loadStarsAction),
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

// showStarDialog prompts for a star's name and role after a tap on the
// image, following FotoDif's workflow: clicking a star opens a window to
// name it and mark it as Target ("Variable"), Comparison ("Calibrado"), or
// Check. onConfirm is called only if the user confirms with a non-empty
// name.
func showStarDialog(parent fyne.Window, onConfirm func(name string, role ui.StarRole)) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("e.g. VAR-1, CONTROL")

	roleGroup := widget.NewRadioGroup(
		[]string{ui.RoleTarget.String(), ui.RoleComparison.String(), ui.RoleCheck.String()},
		nil,
	)
	roleGroup.SetSelected(ui.RoleTarget.String())

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Role", roleGroup),
	}

	dialog.NewForm("New star", "Add", "Cancel", items, func(confirmed bool) {
		name := strings.TrimSpace(nameEntry.Text)
		if !confirmed || name == "" {
			return
		}
		onConfirm(name, parseRole(roleGroup.Selected))
	}, parent).Show()
}

func parseRole(text string) ui.StarRole {
	switch text {
	case ui.RoleComparison.String():
		return ui.RoleComparison
	case ui.RoleCheck.String():
		return ui.RoleCheck
	default:
		return ui.RoleTarget
	}
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

func (s *appState) saveStarsAction() {
	go func() {
		path, err := zenity.SelectFileSave(zenity.FileFilters{starsFileFilter}, zenity.ConfirmOverwrite())
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			if err := session.Save(s.stars, path); err != nil {
				dialog.ShowError(err, s.win)
			}
		})
	}()
}

func (s *appState) loadStarsAction() {
	go func() {
		path, err := zenity.SelectFile(zenity.FileFilters{starsFileFilter})
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			stars, err := session.Load(path)
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			s.stars = stars
			s.view.SetStars(s.stars)
			s.refreshStarsSelect()
		})
	}()
}

// starsWithRole returns the subset of stars matching role, in the order
// they were added.
func starsWithRole(stars []ui.Star, role ui.StarRole) []ui.Star {
	var out []ui.Star
	for _, st := range stars {
		if st.Role == role {
			out = append(out, st)
		}
	}
	return out
}

func (s *appState) processAction() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", "Open a FITS image or folder first.", s.win)
		return
	}

	targets := starsWithRole(s.stars, ui.RoleTarget)
	comps := starsWithRole(s.stars, ui.RoleComparison)
	if len(targets) == 0 || len(comps) == 0 {
		dialog.ShowInformation("Starflux", "Place at least one Target and one Comparison star first.", s.win)
		return
	}

	for _, target := range targets {
		obs, err := s.measureSeries(target, comps)
		if err != nil {
			dialog.ShowError(err, s.win)
			continue
		}

		points, errs := timeseries.Series(obs)
		if len(points) == 0 {
			dialog.ShowError(fmt.Errorf("%s: no valid measurements across %d image(s): %v", target.Name, len(s.series), errs), s.win)
			continue
		}

		showLightCurve(s.win, points, target.Name)
	}
}

// measureSeries runs aperture photometry for one target star and combines
// all comparison stars (by averaged flux, see photometry.CombineComparisons)
// into a synthetic comparison measurement, for every image in the series.
func (s *appState) measureSeries(target ui.Star, comps []ui.Star) ([]timeseries.Observation, error) {
	obs := make([]timeseries.Observation, 0, len(s.series))
	for _, img := range s.series {
		targetRes, err := photometry.Measure(img, int(target.X), int(target.Y), centroidHalfWidth, defaultAperture)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: target: %w", img.Path, target.Name, err)
		}

		compResults := make([]photometry.Result, 0, len(comps))
		for _, c := range comps {
			r, err := photometry.Measure(img, int(c.X), int(c.Y), centroidHalfWidth, defaultAperture)
			if err != nil {
				return nil, fmt.Errorf("%s: comparison %s: %w", img.Path, c.Name, err)
			}
			compResults = append(compResults, r)
		}
		combinedComp, err := photometry.CombineComparisons(compResults)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", img.Path, err)
		}

		obs = append(obs, timeseries.Observation{
			JD:     img.JD,
			Target: targetRes,
			Comp:   combinedComp,
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
