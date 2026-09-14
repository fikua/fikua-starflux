package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/fikua/fikua-starflux/internal/aavso"
	"github.com/fikua/fikua-starflux/internal/config"
	"github.com/fikua/fikua-starflux/internal/fits"
	"github.com/fikua/fikua-starflux/internal/period"
	"github.com/fikua/fikua-starflux/internal/photometry"
	"github.com/fikua/fikua-starflux/internal/session"
	"github.com/fikua/fikua-starflux/internal/timeseries"
	"github.com/fikua/fikua-starflux/internal/ui"
	"github.com/fikua/fikua-starflux/internal/watch"
)

// toleranceLevelToPixels maps FotoDif's 1 (most relaxed) .. 9 (most strict)
// tolerance scale to a maximum allowed centroid displacement between
// consecutive frames, in pixels. Level 9 anchors to FotoDif's one
// documented figure (its "radio de seguimiento automático" of 2px); level 1
// is a permissive end with no direct FotoDif source.
func toleranceLevelToPixels(level int) float64 {
	const strictPx, relaxedPx = 2.0, 5.0
	t := float64(level-9) / float64(1-9) // 0 at level 9, 1 at level 1
	return strictPx + t*(relaxedPx-strictPx)
}

const noSeriesLoadedMsg = "Open a FITS image or folder first."

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

// allFilesFilter lets Load stars... surface files saved without a .json
// extension (e.g. the save dialog wasn't given one explicitly), which
// starsFileFilter alone would hide.
var allFilesFilter = zenity.FileFilter{
	Name:     "All files",
	Patterns: []string{"*"},
}

var sessionFileFilter = zenity.FileFilter{
	Name:     "Starflux session (positions + results)",
	Patterns: []string{"*.json"},
	CaseFold: true,
}

var aavsoFileFilter = zenity.FileFilter{
	Name:     "AAVSO Extended Format report",
	Patterns: []string{"*.txt"},
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

	// accumulatedPoints holds previously computed light-curve points per
	// target star name, carried across loads when "New session" is
	// unchecked, so an interrupted series can be resumed instead of
	// losing everything measured so far.
	accumulatedPoints map[string][]timeseries.Point
	newSessionCheck   *widget.Check

	// cfg holds the persistent, user-editable measurement parameters
	// (centroid box, aperture/annulus radii, field-shift tolerance),
	// loaded at startup and edited via showConfigDialog.
	cfg config.Config

	seriesLabel *widget.Label
	starsSelect *widget.Select // populated with current star names, for removal

	// watchStop is non-nil while watch-folder mode is running (at most one
	// watch at a time); closing it signals the polling goroutine to stop.
	watchStop chan struct{}

	// imageStatus records each loaded image's outcome from the most recent
	// Process run, keyed by img.Path, so the Series inspector dialog can
	// show accurate status any time it's opened — not just immediately
	// after processing. Reset to nil whenever a new series is loaded.
	imageStatus map[string]imageStatus
}

func newAppState(win fyne.Window) *appState {
	newSessionCheck := widget.NewCheck("New session (discard previous results)", nil)
	newSessionCheck.SetChecked(true)

	return &appState{
		win:             win,
		view:            ui.NewImageView(),
		levels:          ui.Levels{Background: 0, Range: 65535},
		seriesLabel:     widget.NewLabel("No images loaded"),
		starsSelect:     widget.NewSelect(nil, nil),
		newSessionCheck: newSessionCheck,
		cfg:             config.LoadOrDefault(),
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
	for _, img := range loaded {
		img.Included = true
	}
	s.series = loaded
	s.imageStatus = nil
	s.clearStars()
	if s.newSessionCheck.Checked {
		s.accumulatedPoints = nil
	}
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
		if len(s.series) == 0 {
			return
		}
		showStarDialog(s.win, s.series[0], x, y, s.cfg.CentroidHalfWidth, func(name string, role ui.StarRole) {
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

	w.SetMainMenu(newMainMenu(a, s))

	sectionHeader := func(text string) fyne.CanvasObject {
		return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}

	controls := container.NewVBox(
		sectionHeader("Series"),
		s.seriesLabel,
		widget.NewButton("Series...", s.showSeriesInspectorDialog),

		widget.NewSeparator(),
		sectionHeader("Stars"),
		widget.NewLabel("Stars:"),
		s.starsSelect,
		removeButton,
		widget.NewButton("Clear all stars", s.clearStars),
		widget.NewButton("Detect stars...", s.detectStarsAction),
		widget.NewButton("Search variables...", s.searchVariablesAction),

		widget.NewSeparator(),
		sectionHeader("Display"),
		widget.NewLabel("Background:"),
		newLevelSlider(0, 65535, s.levels.Background, func(v float64) { s.levels.Background = v; s.redraw() }),
		widget.NewLabel("Range:"),
		newLevelSlider(1, 65535, s.levels.Range, func(v float64) { s.levels.Range = v; s.redraw() }),

		widget.NewSeparator(),
		sectionHeader("Process"),
		widget.NewButton("Process", s.processAction),
		widget.NewButton("Clear results...", s.clearResultsAction),
	)

	sidebar := container.NewVScroll(controls)
	sidebar.SetMinSize(fyne.NewSize(220, 0))

	content := container.NewBorder(nil, nil, nil, sidebar, s.view)
	w.SetContent(content)
	w.Resize(fyne.NewSize(1000, 700))
	w.ShowAndRun()
}

// newMainMenu builds Starflux's menu bar: File (opening FITS data), Session
// (star/session persistence, and the "New session" toggle that used to be
// a sidebar checkbox), and Tools (configuration and the observer-mode
// theme toggle, also formerly a sidebar checkbox) — actions that are set
// up once per session or touched only occasionally, kept out of the
// always-visible sidebar so it stays focused on active-session controls.
func newMainMenu(a fyne.App, s *appState) *fyne.MainMenu {
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Open FITS...", s.openFileAction),
		fyne.NewMenuItem("Open Files...", s.openFilesAction),
		fyne.NewMenuItem("Open Folder...", s.openFolderAction),
	)

	newSessionItem := fyne.NewMenuItem("New session", nil)
	newSessionItem.Checked = s.newSessionCheck.Checked
	sessionMenu := fyne.NewMenu("Session")
	newSessionItem.Action = func() {
		s.newSessionCheck.SetChecked(!s.newSessionCheck.Checked)
		newSessionItem.Checked = s.newSessionCheck.Checked
		sessionMenu.Refresh()
	}
	sessionMenu.Items = []*fyne.MenuItem{
		newSessionItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Save stars...", s.saveStarsAction),
		fyne.NewMenuItem("Load stars...", s.loadStarsAction),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Save session...", s.saveSessionAction),
		fyne.NewMenuItem("Load session...", s.loadSessionAction),
	}

	observerItem := fyne.NewMenuItem("Observer mode (red light)", nil)
	toolsMenu := fyne.NewMenu("Tools")
	observerItem.Action = func() {
		observerItem.Checked = !observerItem.Checked
		if observerItem.Checked {
			a.Settings().SetTheme(ui.ObserverTheme{})
		} else {
			a.Settings().SetTheme(theme.DefaultTheme())
		}
		toolsMenu.Refresh()
	}
	toolsMenu.Items = []*fyne.MenuItem{
		fyne.NewMenuItem("Configuration...", s.showConfigDialog),
		observerItem,
	}

	return fyne.NewMainMenu(fileMenu, sessionMenu, toolsMenu)
}

// showStarDialog prompts for a star's name and role after a tap on the
// image, following FotoDif's workflow: clicking a star opens a window to
// name it and mark it as Target ("Variable"), Comparison ("Calibrado"), or
// Check. It also shows the peak ADU under the cursor (a guide against
// saturation) and, when the header provides them, the instrumental
// magnitude (from MZERO) and the FILTER used — matching FotoDif's star
// selection window. onConfirm is called only if the user confirms with a
// non-empty name.
func showStarDialog(parent fyne.Window, img *fits.Image, x, y float64, centroidHalfWidth int, onConfirm func(name string, role ui.StarRole)) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("e.g. VAR-1, CONTROL")

	roleGroup := widget.NewRadioGroup(
		[]string{ui.RoleTarget.String(), ui.RoleComparison.String(), ui.RoleCheck.String()},
		nil,
	)
	roleGroup.SetSelected(ui.RoleTarget.String())

	maxADU := photometry.MaxADU(img, int(x), int(y), centroidHalfWidth)

	items := []*widget.FormItem{
		widget.NewFormItem("Name", nameEntry),
		widget.NewFormItem("Role", roleGroup),
		widget.NewFormItem("Max ADU", widget.NewLabel(fmt.Sprintf("%.0f", maxADU))),
	}
	if img.HasMZero {
		mag := img.MZero - 2.5*math.Log10(maxADU)
		items = append(items, widget.NewFormItem("Magnitude", widget.NewLabel(fmt.Sprintf("%.2f", mag))))
	}
	if img.Filter != "" {
		items = append(items, widget.NewFormItem("Filter", widget.NewLabel(img.Filter)))
	}

	dialog.NewForm("New star", "Add", "Cancel", items, func(confirmed bool) {
		name := strings.TrimSpace(nameEntry.Text)
		if !confirmed || name == "" {
			return
		}
		onConfirm(name, parseRole(roleGroup.Selected))
	}, parent).Show()
}

// showConfigDialog opens a modal form to edit the app's persistent
// measurement configuration (centroid box, aperture/annulus radii, and
// field-shift tolerance), matching FotoDif's "Fotometría" configuration
// section. Confirming saves the new config to disk immediately and applies
// it to future Process runs; canceling discards the edits.
func (s *appState) showConfigDialog() {
	halfWidthEntry := widget.NewEntry()
	halfWidthEntry.SetText(strconv.Itoa(s.cfg.CentroidHalfWidth))

	rEntry := widget.NewEntry()
	rEntry.SetText(formatConfigFloat(s.cfg.Aperture.R))
	rInEntry := widget.NewEntry()
	rInEntry.SetText(formatConfigFloat(s.cfg.Aperture.RIn))
	rOutEntry := widget.NewEntry()
	rOutEntry.SetText(formatConfigFloat(s.cfg.Aperture.ROut))

	toleranceSlider := newToleranceSlider(s.cfg.ToleranceLevel, nil)

	items := []*widget.FormItem{
		widget.NewFormItem("Centroid box half-width (px)", halfWidthEntry),
		widget.NewFormItem("Aperture radius R (px)", rEntry),
		widget.NewFormItem("Sky annulus inner radius (px)", rInEntry),
		widget.NewFormItem("Sky annulus outer radius (px)", rOutEntry),
		widget.NewFormItem("Field-shift tolerance (1=relaxed – 9=strict)", toleranceSlider),
	}

	dialog.NewForm("Configuration", "Save", "Cancel", items, func(confirmed bool) {
		if !confirmed {
			return
		}
		newCfg, err := parseConfigForm(halfWidthEntry.Text, rEntry.Text, rInEntry.Text, rOutEntry.Text, int(math.Round(toleranceSlider.Value)))
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		s.cfg = newCfg
		if path, err := config.Path(); err == nil {
			if err := config.Save(s.cfg, path); err != nil {
				dialog.ShowError(err, s.win)
			}
		}
	}, s.win).Show()
}

// formatConfigFloat renders a configuration float without a trailing ".0"
// noise for whole numbers, keeping the form's default text easy to re-edit.
func formatConfigFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// parseConfigForm validates and builds a config.Config from the
// Configuration dialog's raw text entries.
func parseConfigForm(halfWidthText, rText, rInText, rOutText string, toleranceLevel int) (config.Config, error) {
	halfWidth, err := strconv.Atoi(strings.TrimSpace(halfWidthText))
	if err != nil || halfWidth < 1 {
		return config.Config{}, fmt.Errorf("centroid box half-width must be a positive integer")
	}

	r, err := strconv.ParseFloat(strings.TrimSpace(rText), 64)
	if err != nil || r <= 0 {
		return config.Config{}, fmt.Errorf("aperture radius R must be a positive number")
	}
	rIn, err := strconv.ParseFloat(strings.TrimSpace(rInText), 64)
	if err != nil || rIn <= 0 {
		return config.Config{}, fmt.Errorf("sky annulus inner radius must be a positive number")
	}
	rOut, err := strconv.ParseFloat(strings.TrimSpace(rOutText), 64)
	if err != nil || rOut <= 0 {
		return config.Config{}, fmt.Errorf("sky annulus outer radius must be a positive number")
	}
	if !(r < rIn && rIn < rOut) {
		return config.Config{}, fmt.Errorf("radii must satisfy R < inner radius < outer radius")
	}

	return config.Config{
		CentroidHalfWidth: halfWidth,
		Aperture:          photometry.Aperture{R: r, RIn: rIn, ROut: rOut},
		ToleranceLevel:    toleranceLevel,
	}, nil
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

func newToleranceSlider(initial int, onChanged func(level int)) *widget.Slider {
	slider := widget.NewSlider(1, 9)
	slider.Step = 1
	slider.Value = float64(initial)
	slider.OnChanged = func(v float64) { onChanged(int(math.Round(v))) }
	return slider
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
		path, err := zenity.SelectFile(zenity.FileFilters{starsFileFilter, allFilesFilter})
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

// saveSessionAction saves star positions AND every target's accumulated
// light-curve points to one file, matching FotoDif's "Guardar datos"
// ("save now, recover later without re-measuring").
func (s *appState) saveSessionAction() {
	go func() {
		path, err := zenity.SelectFileSave(zenity.FileFilters{sessionFileFilter}, zenity.ConfirmOverwrite())
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			if err := session.SaveFull(s.stars, s.accumulatedPoints, path); err != nil {
				dialog.ShowError(err, s.win)
			}
		})
	}()
}

// loadSessionAction restores star positions and accumulated points from a
// file written by saveSessionAction, matching FotoDif's "Recuperar datos".
// It does not re-render any light curve on its own — restored points
// become visible the next time Process runs.
func (s *appState) loadSessionAction() {
	go func() {
		path, err := zenity.SelectFile(zenity.FileFilters{sessionFileFilter})
		if errors.Is(err, zenity.ErrCanceled) {
			return
		}
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			stars, points, err := session.LoadFull(path)
			if err != nil {
				dialog.ShowError(err, s.win)
				return
			}
			s.stars = stars
			s.view.SetStars(s.stars)
			s.refreshStarsSelect()
			s.accumulatedPoints = points
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

// detectStarsAction runs bulk star detection on the first image of the
// loaded series and, after confirmation, adds newly found stars as
// Comparison-role candidates. Matches FotoDif's documented workflow: the
// user is expected to have already manually marked Target and at least one
// Comparison star before running bulk detection — a target star's identity
// is a scientific choice the tool shouldn't guess.
func (s *appState) detectStarsAction() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", noSeriesLoadedMsg, s.win)
		return
	}

	const minCounts = 1000.0  // absolute floor only; DetectStars combines this with an adaptive per-image threshold. TODO: consider exposing as a config.Config field in a future release
	const minSeparation = 8.0 // px; larger than a typical aperture radius to avoid double-detecting one star

	detections := photometry.DetectStars(s.series[0], minCounts, minSeparation, photometry.DefaultSigmaMultiplier)
	var toAdd []photometry.Detection
	for _, d := range detections {
		if !s.nearExistingStar(d.X, d.Y, minSeparation) {
			toAdd = append(toAdd, d)
		}
	}

	if len(toAdd) == 0 {
		dialog.ShowInformation("Starflux", "No new stars found.", s.win)
		return
	}

	dialog.ShowConfirm("Detect stars", fmt.Sprintf("Found %d candidate star(s). Add them all as Comparison stars?", len(toAdd)), func(confirmed bool) {
		if !confirmed {
			return
		}
		for _, d := range toAdd {
			name := fmt.Sprintf("AUTO-%d-%d", int(d.X), int(d.Y))
			if s.hasStarNamed(name) {
				continue // extremely unlikely coordinate collision; skip rather than error
			}
			s.addStar(ui.Star{Name: name, Role: ui.RoleComparison, X: d.X, Y: d.Y})
		}
	}, s.win)
}

// nearExistingStar reports whether (x, y) is within minSeparation pixels of
// any already-marked star, so detectStarsAction doesn't re-propose stars
// the user already placed by hand (including the Target itself).
func (s *appState) nearExistingStar(x, y, minSeparation float64) bool {
	for _, st := range s.stars {
		if photometry.Distance(st.X, st.Y, x, y) < minSeparation {
			return true
		}
	}
	return false
}

// variableCandidateResult bundles one scanned star's outcome for the
// variable-search results dialog.
type variableCandidateResult struct {
	star   ui.Star
	ratio  float64
	points []timeseries.Point
}

// otherStars returns every star except the one named exclude, used as the
// comparison baseline when scanning a candidate star as if it were a
// target.
func otherStars(stars []ui.Star, exclude string) []ui.Star {
	var out []ui.Star
	for _, st := range stars {
		if st.Name != exclude {
			out = append(out, st)
		}
	}
	return out
}

// searchVariablesAction measures every currently-marked Comparison-role
// star (typically populated by "Detect stars..." bulk detection) as if
// each were its own target against every OTHER marked star used as a
// shared comparison baseline, then flags which ones look like they vary
// more than their own measurement noise can explain. This is a distinct
// measurement pass from processAction: ordinary Process only measures
// actual RoleTarget stars, since which star IS the science target is a
// decision the tool shouldn't make — variable-star search instead treats
// "every candidate, one at a time" as a bulk scan, matching FotoDif's
// "Búsqueda de variables" workflow (mark Target+Comparison, bulk-detect
// the rest, Process everything, then flag which look variable).
func (s *appState) searchVariablesAction() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", noSeriesLoadedMsg, s.win)
		return
	}

	candidates := starsWithRole(s.stars, ui.RoleComparison)
	realTarget := starsWithRole(s.stars, ui.RoleTarget)
	if len(candidates) == 0 {
		dialog.ShowInformation("Starflux", "No Comparison-role stars to scan. Run \"Detect stars...\" first to bulk-add field stars as candidates.", s.win)
		return
	}
	if len(realTarget) == 0 {
		dialog.ShowInformation("Starflux", "Mark at least one Target star first — it's used as part of the comparison baseline for every scanned candidate.", s.win)
		return
	}

	var results []variableCandidateResult
	for _, candidate := range candidates {
		comps := otherStars(s.stars, candidate.Name)
		result := s.measureSeries(candidate, comps)
		points, _ := timeseries.Series(result.obs)
		if len(points) == 0 {
			continue
		}
		results = append(results, variableCandidateResult{
			star:   candidate,
			ratio:  timeseries.VariabilityRatio(points),
			points: points,
		})
	}

	showVariableSearchResults(s.win, results, s.series[0].Filter)
}

// showVariableSearchResults lists every scanned candidate, sorted by how
// strongly it exceeds the variability threshold (most-variable first),
// with a button per row to open that star's own light curve via the
// existing showLightCurve dialog. This is a v1 simplification of FotoDif's
// multi-curve overlay/stacking "Variables?" tab — a sorted list answering
// "which stars look interesting" is the core signal; a combined-plot
// overlay view is a nice-to-have left for later.
func showVariableSearchResults(parent fyne.Window, results []variableCandidateResult, filter string) {
	if len(results) == 0 {
		dialog.ShowInformation("Starflux", "No candidates could be measured.", parent)
		return
	}

	sort.Slice(results, func(i, j int) bool { return results[i].ratio > results[j].ratio })

	rows := container.NewVBox()
	for _, r := range results {
		r := r // capture
		flagged := " "
		if timeseries.IsPossibleVariable(r.points, timeseries.VariableThresholdDefault) {
			flagged = "*"
		}
		label := widget.NewLabel(fmt.Sprintf("%s %s — scatter/error ratio %.2f", flagged, r.star.Name, r.ratio))
		openButton := widget.NewButton("Open light curve", func() {
			showLightCurve(nil, parent, r.points, nil, filter, r.star.Name, ui.Star{}, nil, "")
		})
		rows.Add(container.NewHBox(label, openButton))
	}

	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(500, 400))

	legend := widget.NewLabel(fmt.Sprintf("* = possible variable (ratio > %.0fx expected noise). Sorted by ratio, most extreme first. This filter is deliberately generous — verify a flagged star isn't actually contamination from a close neighbor before trusting it.", timeseries.VariableThresholdDefault))

	dialog.NewCustom("Variable-star search results", "Close", container.NewVBox(legend, scroll), parent).Show()
}

func (s *appState) processAction() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", noSeriesLoadedMsg, s.win)
		return
	}

	targets := starsWithRole(s.stars, ui.RoleTarget)
	comps := starsWithRole(s.stars, ui.RoleComparison)
	if len(targets) == 0 || len(comps) == 0 {
		dialog.ShowInformation("Starflux", "Place at least one Target and one Comparison star first.", s.win)
		return
	}

	if s.imageStatus == nil {
		s.imageStatus = make(map[string]imageStatus)
	}

	for _, target := range targets {
		result := s.measureSeries(target, comps)

		newPoints, skipped := timeseries.SeriesWithObservations(result.obs)
		s.recordSkippedObservations(target.Name, result.attempted, skipped)
		allPoints := timeseries.MergeSorted(s.accumulatedPoints[target.Name], newPoints)

		if len(allPoints) == 0 {
			dialog.ShowError(fmt.Errorf("%s: no valid measurements across %d image(s)", target.Name, len(s.series)), s.win)
			continue
		}

		// Persist for a future "continue previous session" run — today's
		// clean run can be tomorrow's "previous session" if a bad frame
		// shows up later.
		if s.accumulatedPoints == nil {
			s.accumulatedPoints = make(map[string][]timeseries.Point)
		}
		s.accumulatedPoints[target.Name] = allPoints

		drift := make([]ui.DriftPoint, 0, len(result.drift))
		for i, d := range result.drift {
			drift = append(drift, ui.DriftPoint{JD: result.obs[i].JD, DistancePx: d})
		}
		watchedDir := ""
		if len(s.series) > 1 {
			// Watch-folder mode only makes sense for a series loaded from a
			// folder — a single-file load has nowhere new to poll for.
			watchedDir = filepath.Dir(s.series[0].Path)
		}
		showLightCurve(s, s.win, allPoints, drift, s.series[0].Filter, target.Name, target, comps, watchedDir)

		if len(skipped) > 0 {
			dialog.ShowInformation("Starflux", fmt.Sprintf("%s: %d epoch(s) skipped. Open \"Series...\" to see which images and why.", target.Name, len(skipped)), s.win)
		}
	}
}

// clearResultsAction discards every accumulated light-curve point across
// all targets (s.accumulatedPoints), without touching the loaded series or
// marked stars. This is the only way to reset accumulated results without
// reloading the whole series from disk — the "New session" menu toggle
// only takes effect on the NEXT series load (loadSeries), so re-running
// Process after changing which star is the Target (or after a tracking
// fix changes measured positions) would otherwise silently merge the new
// run's points with stale ones from a previous run at the same JDs,
// producing a light curve with multiple disagreeing bands (a real
// reported case: re-processing after a bug fix showed 3 separate Δm
// bands instead of one curve, from old and new points at the same
// epochs both being kept by MergeSorted, which does not deduplicate).
func (s *appState) clearResultsAction() {
	if len(s.accumulatedPoints) == 0 {
		dialog.ShowInformation("Starflux", "No accumulated results to clear.", s.win)
		return
	}
	dialog.ShowConfirm("Clear results", "Discard all accumulated light-curve results? This cannot be undone; the loaded series and marked stars are kept.", func(confirmed bool) {
		if !confirmed {
			return
		}
		s.accumulatedPoints = nil
	}, s.win)
}

// recordSkippedObservations overwrites s.imageStatus for every image whose
// DifferentialMagnitude computation failed (e.g. non-positive net flux),
// correlating each skipped Observation back to its source image by exact
// JD match against attempted — safe here because attempted is exactly the
// slice result.obs was built from, so JDs are copied verbatim with no
// intervening float arithmetic on either side.
func (s *appState) recordSkippedObservations(targetName string, attempted []*fits.Image, skipped []timeseries.SkippedObservation) {
	if len(skipped) == 0 {
		return
	}
	byJD := make(map[float64]*fits.Image, len(attempted))
	for _, img := range attempted {
		byJD[img.JD] = img
	}
	for _, sk := range skipped {
		if img, ok := byJD[sk.Obs.JD]; ok {
			s.imageStatus[img.Path] = imageStatus{kind: statusError, target: targetName, message: sk.Err.Error()}
		}
	}
}

// measureSeriesResult is measureSeries's outcome: obs holds every included
// image successfully measured, in order; attempted is the index-aligned
// slice of *fits.Image each entry in obs came from, so callers can recover
// which file produced which point without JD matching.
type measureSeriesResult struct {
	obs       []timeseries.Observation
	attempted []*fits.Image // index-aligned with obs
	drift     []float64     // per-frame target displacement in pixels, index-aligned with obs
}

// measureSeries runs aperture photometry for one target star and combines
// all comparison stars (by averaged flux, see photometry.CombineComparisons)
// into a synthetic comparison measurement, for every INCLUDED image in the
// series. Each star's position is tracked frame to frame: the first
// attempted image seeds from the star's manually marked position, and every
// following attempted image seeds its centroid search from the previous
// successfully-measured image's refined position instead of the original
// fixed coordinates, matching FotoDif's handling of small field drift over
// a session. A frame-to-frame jump larger than the configured tolerance
// allows (see toleranceLevelToPixels), or any other photometry failure,
// marks that one image as an error in s.imageStatus and moves on to the
// next image — it does not stop the whole series. An excluded image
// (img.Included == false) is skipped without even attempting measurement,
// recorded as statusExcluded, and does not disturb tracking continuity:
// the tracker's position simply carries over unchanged to the next
// attempted image, identical to how a measurement failure already leaves
// it unchanged.
func (s *appState) measureSeries(target ui.Star, comps []ui.Star) measureSeriesResult {
	tracked := newStarTracker(target, comps)
	tracked.cadenceJD = medianCadenceJD(s.series)

	obs := make([]timeseries.Observation, 0, len(s.series))
	attempted := make([]*fits.Image, 0, len(s.series))
	for _, img := range s.series {
		if !img.Included {
			s.imageStatus[img.Path] = imageStatus{kind: statusExcluded}
			tracked.framesSinceSuccess++
			tracked.lastJD = img.JD
			continue
		}
		ob, err := measureOneImage(tracked, img, s.cfg)
		if err != nil {
			// measureFrame already advanced tracked.lastJD to img.JD before
			// returning the error, regardless of outcome.
			s.imageStatus[img.Path] = imageStatus{kind: statusError, target: target.Name, message: err.Error(), trackedStars: tracked.snapshotStars()}
			tracked.framesSinceSuccess++
			continue
		}
		s.imageStatus[img.Path] = imageStatus{kind: statusOK, target: target.Name, recoveredAfterGap: tracked.lastReacquireGap, trackedStars: tracked.snapshotStars()}
		tracked.lastReacquireGap = 0
		obs = append(obs, ob)
		attempted = append(attempted, img)
	}
	return measureSeriesResult{obs: obs, attempted: attempted, drift: tracked.targetDrift}
}

// measureOneImage measures target and combined comparison flux for a
// single image using tracker's current tracked position, advancing tracker
// on success. This is the per-image body factored out of measureSeries's
// loop so watch-folder mode (startWatchFolder) can reuse the exact same
// tracking/tolerance logic one new image at a time, instead of re-deriving
// it — tracker is deliberately long-lived across calls in that case.
func measureOneImage(tracker *starTracker, img *fits.Image, cfg config.Config) (timeseries.Observation, error) {
	targetRes, compResults, drift, err := tracker.measureFrame(img, cfg)
	if err != nil {
		return timeseries.Observation{}, err
	}
	combinedComp, err := photometry.CombineComparisons(compResults)
	if err != nil {
		return timeseries.Observation{}, err
	}
	tracker.targetDrift = append(tracker.targetDrift, drift)
	return timeseries.Observation{JD: img.JD, Target: targetRes, Comp: combinedComp}, nil
}

// starTracker holds each star's current tracked position for one
// measureSeries run, seeded from the stars' manually marked positions and
// updated to each frame's refined centroid as the series is measured.
type starTracker struct {
	targetName       string
	targetX, targetY float64
	targetFlux       float64 // most recently accepted NetFlux for the target; 0 until first success
	targetDrift      []float64 // per-frame photometry.Distance(prev, new) for the target, one entry per successfully measured frame

	compNames []string
	compX     []float64
	compY     []float64
	compFlux  []float64 // most recently accepted NetFlux per comparison star; 0 until first success

	// framesSinceSuccess counts every skipped attempt (excluded or
	// errored) since the tracked position was last advanced; reset to 0
	// on any success, whether ordinary or via reacquireStar. Gates
	// whether a tolerance failure gets a fallback re-acquisition attempt:
	// a failure with framesSinceSuccess == 0 (two back-to-back attempted
	// frames) isn't explained by seed staleness, so it's reported exactly
	// as before with no fallback.
	framesSinceSuccess int
	// lastReacquireGap records the gap size a successful reacquisition
	// just bridged, purely for Series-inspector display; 0 after an
	// ordinary first-attempt success.
	lastReacquireGap int

	// lastJD is the Julian Date of the most recently attempted image
	// (included and measured, whether it succeeded or failed) — 0 before
	// the first frame. Used with cadenceJD to detect a missing-file gap
	// in the series (e.g. v_032.fit then v_034.fit with no v_033.fit on
	// disk): unlike an excluded image, a missing file leaves no trace in
	// s.series for framesSinceSuccess to count, so the frame-index-based
	// gap tracking alone would treat it as an ordinary single-frame step
	// and under-tolerate the correspondingly larger real-world
	// displacement.
	lastJD float64
	// cadenceJD is the series' typical (median) time interval between
	// consecutive images, computed once up front by medianCadenceJD. 0 if
	// it couldn't be determined (fewer than 2 images with usable JDs),
	// in which case JD-based gap detection is skipped entirely.
	cadenceJD float64
}

func newStarTracker(target ui.Star, comps []ui.Star) *starTracker {
	t := &starTracker{
		targetName: target.Name,
		targetX:    target.X,
		targetY:    target.Y,
		compNames:  make([]string, len(comps)),
		compX:      make([]float64, len(comps)),
		compY:      make([]float64, len(comps)),
		compFlux:   make([]float64, len(comps)),
	}
	for i, c := range comps {
		t.compNames[i] = c.Name
		t.compX[i] = c.X
		t.compY[i] = c.Y
	}
	return t
}

// medianCadenceJD returns the median JD gap between consecutive images in
// series (assumed already in acquisition order), as the series' typical
// single-frame interval — used to detect when a frame-to-frame JD gap is
// actually several intervals wide (e.g. a missing file on disk) rather
// than one. Returns 0 if fewer than 2 images have a usable (non-zero) JD,
// in which case callers should skip JD-based gap detection entirely.
func medianCadenceJD(series []*fits.Image) float64 {
	var gaps []float64
	for i := 1; i < len(series); i++ {
		prev, cur := series[i-1].JD, series[i].JD
		if prev <= 0 || cur <= 0 || cur <= prev {
			continue
		}
		gaps = append(gaps, cur-prev)
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2]
}

// jdCadenceGap returns how many typical frame intervals elapsed between
// lastJD and currentJD, rounded to the nearest whole interval and floored
// at 1 — e.g. 1 for an ordinary consecutive frame, 2 if one frame's worth
// of time was skipped (such as a missing file between two present ones),
// and so on. Returns 1 (no adjustment) whenever gap detection isn't
// possible: no prior frame yet (lastJD == 0), no usable cadence, or a
// non-positive/unreasonable elapsed time.
func jdCadenceGap(lastJD, currentJD, cadenceJD float64) int {
	if lastJD <= 0 || currentJD <= 0 || cadenceJD <= 0 || currentJD <= lastJD {
		return 1
	}
	intervals := int(math.Round((currentJD - lastJD) / cadenceJD))
	if intervals < 1 {
		return 1
	}
	return intervals
}

// fieldShiftSearchHalfWidth bounds how far detectFieldShift looks for a
// shared displacement, in pixels each direction — generous enough to
// cover a deliberate telescope recentering (tens of pixels), but bounded
// so the scan stays cheap and doesn't match across an entire large image.
// fieldShiftBinPx is the bucket size candidate displacement vectors are
// rounded to before voting, coarse enough that independently-noisy
// per-star centroids on a genuinely shared shift still land in the same
// bucket, but fine enough not to conflate two genuinely different shifts.
// fieldShiftMatchRadiusPx is how close a shifted tracked position must
// land to a real detection to count as matching it, when scoring the
// winning bucket's vector against every tracked star (not just the ones
// that happened to generate votes for it).
// fieldShiftMinMatchFraction is the minimum fraction of tracked stars
// that must match under the winning vector for it to be accepted as a
// real field shift, guarding against a coincidental small-cluster
// agreement in a crowded field being mistaken for the whole group moving
// together.
const (
	fieldShiftSearchHalfWidth  = 60.0
	fieldShiftBinPx            = 4.0
	fieldShiftMatchRadiusPx    = 3.0
	fieldShiftMinMatchFraction = 0.5
)

// detectFieldShift looks for one displacement vector (dx, dy) that
// explains most of names' tracked stars moving together between the
// tracker's last-known positions (lastX, lastY) and img — the signature
// of the observer recentering or re-slewing the telescope between
// exposures, rather than each star independently drifting.
//
// A per-star "closest candidate within range" vote (tried first, and
// simpler) does NOT work in a crowded field: with hundreds of faint
// detections scattered throughout the search radius, each tracked star's
// single closest candidate is essentially a random nearby star, not
// necessarily the one it actually shifted to — the resulting per-star
// vectors are mostly noise with no shared vector to vote for, even when a
// real uniform shift exists (confirmed against this exact reported case:
// 873 candidates within 60px of each tracked star made every "nearest
// neighbor" a false match).
//
// Instead, this generates displacement HYPOTHESES from every (tracked
// star, nearby candidate) pair — not just the single closest one — since
// the true shift vector is guaranteed to be among these pairs' vectors
// for at least the stars it actually explains. Each hypothesis is
// rounded into a coarse bucket (fieldShiftBinPx) and buckets are ranked
// by how many DISTINCT tracked stars contributed a hypothesis to them
// (one vote per star, so one crowded star can't stuff a bucket). The
// winning bucket's mean vector is then scored for real: apply it to
// EVERY tracked star (not just the ones that voted for this bucket) and
// count how many land within fieldShiftMatchRadiusPx of an actual
// detection. Only a vector that explains at least
// fieldShiftMinMatchFraction of all tracked stars this way is accepted.
func detectFieldShift(img *fits.Image, names []string, lastX, lastY []float64, cfg config.Config) (dx, dy float64, ok bool) {
	if len(names) == 0 {
		return 0, 0, false
	}
	candidates := photometry.DetectStars(img, reacquireMinCounts, 1, photometry.DefaultSigmaMultiplier)
	if len(candidates) == 0 {
		return 0, 0, false
	}

	type bucket struct {
		sumDX, sumDY float64
		n            int
		starsSeen    map[int]bool
	}
	buckets := map[[2]int]*bucket{}
	for i := range names {
		seenInStar := map[[2]int]bool{}
		for ci := range candidates {
			c := &candidates[ci]
			ddx := c.X - lastX[i]
			ddy := c.Y - lastY[i]
			if math.Hypot(ddx, ddy) > fieldShiftSearchHalfWidth {
				continue
			}
			key := [2]int{int(math.Round(ddx / fieldShiftBinPx)), int(math.Round(ddy / fieldShiftBinPx))}
			if seenInStar[key] {
				continue // this star already voted for this bucket via a closer candidate
			}
			seenInStar[key] = true
			b, exists := buckets[key]
			if !exists {
				b = &bucket{starsSeen: map[int]bool{}}
				buckets[key] = b
			}
			b.sumDX += ddx
			b.sumDY += ddy
			b.n++
			b.starsSeen[i] = true
		}
	}

	var winner *bucket
	for _, b := range buckets {
		if winner == nil || len(b.starsSeen) > len(winner.starsSeen) {
			winner = b
		}
	}
	if winner == nil || winner.n == 0 {
		return 0, 0, false
	}
	candidateDX := winner.sumDX / float64(winner.n)
	candidateDY := winner.sumDY / float64(winner.n)

	matches := 0
	for i := range names {
		px, py := lastX[i]+candidateDX, lastY[i]+candidateDY
		for ci := range candidates {
			if photometry.Distance(px, py, candidates[ci].X, candidates[ci].Y) <= fieldShiftMatchRadiusPx {
				matches++
				break
			}
		}
	}
	if float64(matches) < fieldShiftMinMatchFraction*float64(len(names)) {
		return 0, 0, false
	}
	return candidateDX, candidateDY, true
}

// snapshotStars returns the tracker's current per-star positions (target
// and every comparison) as []ui.Star, for surfacing in diagnostic UI (the
// Series inspector's per-image preview) — not used in the measurement
// path itself.
func (t *starTracker) snapshotStars() []ui.Star {
	stars := make([]ui.Star, 0, 1+len(t.compNames))
	stars = append(stars, ui.Star{Name: t.targetName, Role: ui.RoleTarget, X: t.targetX, Y: t.targetY})
	for i, name := range t.compNames {
		stars = append(stars, ui.Star{Name: name, Role: ui.RoleComparison, X: t.compX[i], Y: t.compY[i]})
	}
	return stars
}

// patternToleranceMultiplier scales toleranceLevelToPixels to get the max
// allowed deviation between one star's own frame-to-frame displacement and
// the group's consensus displacement (photometry.MedianDisplacement).
//
// This is TIGHTER than the per-star tolerance (< 1), not looser — by
// design: a star's own tolerance check compares its new position against
// its own single, possibly-noisy last position, but the pattern check
// compares against the consensus of several independently-tracked stars,
// a materially more precise reference. A looser (>1) multiplier would be
// nearly unable to catch the failure mode this check exists for: by the
// triangle inequality, a star that passes its OWN tolerance (moved at
// most maxJump from its last position) can only deviate from a
// near-stationary group consensus by at most ~maxJump — so a multiplier
// >= 1 would only ever trigger when the rest of the group is ALSO moving
// by more than a full maxJump, i.e. only during large, obvious drift,
// exactly the case that does NOT need this check (ordinary tolerance
// already handles it). The reported real-world failure is the opposite:
// small, unremarkable individual displacements, with one star pointed in
// a different direction than everyone else — a tighter threshold is what
// makes that catchable.
const patternToleranceMultiplier = 0.6

// trackedStarMeasurement is one star's outcome from a single independent
// measurement attempt within measureFrame: its measured Result, its
// displacement from its previous tracked position, and whether it passed
// its own per-star tolerance check (see measureFrame for how this feeds
// the group consistency check).
type trackedStarMeasurement struct {
	name               string
	lastX              float64 // this star's tracked position BEFORE this frame
	lastY              float64
	res                photometry.Result
	disp               photometry.Displacement
	withinOwnTolerance bool
}

// measureFrame measures the target and every comparison star for one
// image. Each star is first measured independently (ordinary per-star
// tolerance + reacquire); every star that passes its own check then has
// its displacement cross-checked against
// the group's consensus displacement (photometry.MedianDisplacement) —
// this catches a star that has quietly locked onto the wrong, but real
// and nearby, star: a wrong lock still produces a small, "normal-looking"
// frame-to-frame delta that would pass ordinary per-star tolerance, but
// disagrees with how every OTHER tracked star moved this frame, since
// real field drift (imperfect mount tracking) shifts the whole frame by
// approximately one common vector. A star disagreeing with the consensus
// by more than patternToleranceMultiplier*maxJump gets one more
// reacquireStar attempt, this time seeded at the CONSENSUS-predicted
// position (its own last position plus the consensus displacement)
// instead of its own possibly-wrong measurement, since the consensus is a
// better estimate of where it should actually be this frame. A star that
// still can't produce a consistent candidate fails the whole frame, with
// an error naming the pattern mismatch distinctly from an ordinary
// tolerance failure.
//
// The returned drift distance is the target's own displacement, NOT yet
// appended to t.targetDrift — the caller (measureOneImage) only commits
// it once the whole frame (target AND comparisons) succeeds, since a
// later comparison-star failure means this frame contributes no
// observation at all, and an unconditionally appended drift entry would
// silently desync t.targetDrift from the eventual obs/attempted slices
// built in measureSeries (they're meant to stay index-aligned, per
// measureSeriesResult's doc comment).
func (t *starTracker) measureFrame(img *fits.Image, cfg config.Config) (targetRes photometry.Result, compResults []photometry.Result, targetDrift float64, err error) {
	// cadenceGap counts how many typical frame intervals actually elapsed
	// since the last attempted image, per the images' own timestamps —
	// normally 1, but larger when a file is missing from the series on
	// disk (an interval framesSinceSuccess can't see, since a missing
	// file has no entry in s.series to count at all). Scaling maxJump and
	// the reacquire search by this real elapsed time, not just by
	// skipped-attempt count, keeps tolerance matched to how far the star
	// could plausibly have actually moved.
	cadenceGap := jdCadenceGap(t.lastJD, img.JD, t.cadenceJD)
	t.lastJD = img.JD
	maxJump := toleranceLevelToPixels(cfg.ToleranceLevel) * float64(cadenceGap)

	names := make([]string, 0, 1+len(t.compNames))
	lastX := make([]float64, 0, 1+len(t.compNames))
	lastY := make([]float64, 0, 1+len(t.compNames))
	lastFlux := make([]float64, 0, 1+len(t.compNames))
	names = append(names, t.targetName)
	lastX = append(lastX, t.targetX)
	lastY = append(lastY, t.targetY)
	lastFlux = append(lastFlux, t.targetFlux)
	names = append(names, t.compNames...)
	lastX = append(lastX, t.compX...)
	lastY = append(lastY, t.compY...)
	lastFlux = append(lastFlux, t.compFlux...)

	measureAt := func(i int, x, y float64) (photometry.Result, error) {
		return photometry.Measure(img, int(x), int(y), cfg.CentroidHalfWidth, cfg.Aperture)
	}

	// attempt runs the ordinary per-star tolerance + group-pattern-consensus
	// pass seeded at seedX/seedY (searching from there, but still measuring
	// each star's displacement against its real lastX/lastY), and returns
	// the resolved measurements. It fails with patternMismatchErr set when
	// a star fails both its own tolerance AND the group consensus with no
	// successful reacquisition — the signal measureFrame uses below to
	// retry once via detectFieldShift before giving up for real.
	attempt := func(seedX, seedY []float64) (result []trackedStarMeasurement, reacquired bool, patternMismatchErr error, hardErr error) {
		measurements := make([]trackedStarMeasurement, len(names))
		var reacquiredAny bool
		for i, name := range names {
			res, measureErr := measureAt(i, seedX[i], seedY[i])
			// A position that passes WithinTolerance is not, by itself,
			// proof the tracker still has the right star: a slow, gradual
			// drift toward a nearby patch of pure background produces a
			// small, individually-unremarkable frame-to-frame displacement
			// at every step, so it never trips WithinTolerance OR (with
			// few comparison stars — even just 1 — giving the group
			// consensus little discriminating power) the pattern check
			// either. A real reported case had exactly this: a bright
			// comparison star's tracked position quietly drifted onto an
			// empty patch over several frames, each step innocuous on its
			// own, until its NetFlux (dominated by background, since the
			// real star was no longer inside the aperture) went slightly
			// negative. Cross-checking flux catches this even when
			// position alone can't: a real star's flux is stable
			// frame-to-frame (that IS what's being measured), so a
			// separately-drifting flux alongside "fine" position is the
			// signature of tracking something other than the real star.
			fluxOK := lastFlux[i] <= 0 || fluxWithinTolerance(lastFlux[i], res.NetFlux)
			if measureErr == nil && photometry.WithinTolerance(lastX[i], lastY[i], res.X, res.Y, maxJump) && fluxOK {
				measurements[i] = trackedStarMeasurement{
					name:  name,
					lastX: lastX[i], lastY: lastY[i],
					res:                res,
					disp:               photometry.Displacement{DX: res.X - lastX[i], DY: res.Y - lastY[i]},
					withinOwnTolerance: true,
				}
				continue
			}
			if measureErr != nil {
				// A hard Measure failure (e.g. no signal in the search box at
				// all) is not something the ordinary group-pattern check
				// (below, which needs a displacement to compare) can
				// rescue directly — but it always gets a local
				// reacquireStar attempt first, even on the very first
				// failure after a success (gapFrames floored at 1): the
				// star may simply be sitting just outside the search box,
				// which reacquireStar can resolve immediately rather than
				// only after a gap has already accumulated. If THAT also
				// fails, this is reported as a patternMismatchErr (not a
				// hard, unretryable error): a large enough shared field
				// shift can push a star's search box onto a patch with no
				// signal at all, not just background noise, and that's
				// still exactly the case detectFieldShift exists to
				// recover from — the caller gets one retry with a
				// wide-area scan before giving up for real.
				reacq, reacqErr := reacquireStar(img, lastX[i], lastY[i], reacquireGapFrames(t.framesSinceSuccess, cadenceGap), lastFlux[i], cfg)
				if reacqErr != nil {
					return nil, false, fmt.Errorf("%s: %w", name, measureErr), nil
				}
				reacquiredAny = true
				measurements[i] = trackedStarMeasurement{
					name:  name,
					lastX: lastX[i], lastY: lastY[i],
					res:                reacq,
					disp:               photometry.Displacement{DX: reacq.X - lastX[i], DY: reacq.Y - lastY[i]},
					withinOwnTolerance: true,
				}
				continue
			}
			// Measure succeeded but exceeded this star's own tolerance. This
			// is NOT failed immediately (regardless of t.framesSinceSuccess):
			// the direct measurement's displacement is recorded as a
			// pending/unconfirmed candidate, and the group-pattern phase
			// below gets the chance to either confirm it belongs with the
			// group after all (a real, if unusually large, shared field
			// shift) or reacquire it near the consensus position instead.
			measurements[i] = trackedStarMeasurement{
				name:  name,
				lastX: lastX[i], lastY: lastY[i],
				res:                res,
				disp:               photometry.Displacement{DX: res.X - lastX[i], DY: res.Y - lastY[i]},
				withinOwnTolerance: false,
			}
		}

		// Group-pattern phase: every star not already confirmed within its own
		// tolerance (including ones that failed it outright above) is
		// evaluated against the group's consensus displacement. At least 2
		// stars are needed for a consensus to mean anything; with only 1 (no
		// comparison stars at all — not the normal case, since Process
		// requires at least one, but handled correctly regardless), a
		// tolerance failure falls back to reacquireStar directly, since
		// there's no group to check against.
		if len(measurements) < 2 {
			for i, m := range measurements {
				if m.withinOwnTolerance {
					continue
				}
				reacq, reacqErr := reacquireStar(img, m.lastX, m.lastY, reacquireGapFrames(t.framesSinceSuccess, cadenceGap), lastFlux[i], cfg)
				if reacqErr != nil {
					return nil, false, nil, fieldShiftError(m.name, m.lastX, m.lastY, m.res.X, m.res.Y, cfg.ToleranceLevel, maxJump)
				}
				reacquiredAny = true
				measurements[i] = trackedStarMeasurement{
					name:  m.name,
					lastX: m.lastX, lastY: m.lastY,
					res:                reacq,
					disp:               photometry.Displacement{DX: reacq.X - m.lastX, DY: reacq.Y - m.lastY},
					withinOwnTolerance: true,
				}
			}
			return measurements, reacquiredAny, nil, nil
		}

		disps := make([]photometry.Displacement, len(measurements))
		for i, m := range measurements {
			disps[i] = m.disp
		}
		consensus := photometry.MedianDisplacement(disps)
		patternMaxJump := patternToleranceMultiplier * maxJump
		// With exactly 2 tracked stars (target + 1 comparison — the
		// smallest group this branch ever runs for, since len<2 is
		// handled separately above), MedianDisplacement of 2 values is
		// just their average: the very star being checked always pulls
		// its own "consensus" halfway toward itself, so its deviation
		// from that consensus is mathematically always exactly HALF its
		// true error — never enough to exceed patternMaxJump on its own.
		// A real reported case: a comparison star slowly drifted onto an
		// empty patch of background over several frames (individually
		// unremarkable steps that never tripped its own tolerance, until
		// its flux — now checked above — finally revealed the drift);
		// with only 1 other tracked star, this consensus check could
		// never have caught it even if flux hadn't. reliableConsensus
		// gates the "failed own tolerance but agrees with consensus, so
		// accept it anyway" shortcut on there being at least 3 tracked
		// stars, where the median is a genuine majority vote that 1
		// outlier can't drag along with it.
		reliableConsensus := len(measurements) >= 3

		for i, m := range measurements {
			deviation := photometry.Distance(m.disp.DX, m.disp.DY, consensus.DX, consensus.DY)
			if m.withinOwnTolerance && deviation <= patternMaxJump {
				continue
			}
			if !m.withinOwnTolerance && reliableConsensus && deviation <= patternMaxJump {
				// Failed its own tolerance, but agrees with the group —
				// treat as a real, shared field shift rather than a
				// wrong lock, and accept the direct measurement as-is.
				continue
			}
			// Re-running photometry.Measure here would not help: it just
			// centroids on whatever signal is nearest the search box,
			// which is exactly the wrong star that got this star flagged
			// in the first place — reseeding the search center doesn't
			// change WHICH star is inside the box, only where the search
			// starts looking. reacquireStar is different: it runs
			// DetectStars over a local region and explicitly picks the
			// candidate closest to the expected position (here, the
			// consensus-predicted one), so it can reject a nearer-but-
			// wrong star in favor of a farther-but-correct one — which is
			// exactly the discrimination needed. Always eligible
			// (reacquireGapFrames floors at 1) regardless of
			// t.framesSinceSuccess, since a pattern mismatch is its own
			// independent justification for a reacquisition attempt; still
			// scaled up by cadenceGap when a missing file widened the real
			// elapsed time, same as the other two reacquireStar call sites.
			seedX := m.lastX + consensus.DX
			seedY := m.lastY + consensus.DY
			reacquired, reacqErr := reacquireStar(img, seedX, seedY, reacquireGapFrames(0, cadenceGap), lastFlux[i], cfg)
			if reacqErr != nil {
				if m.withinOwnTolerance {
					// The direct measurement already passed its OWN
					// tolerance check — it's a plausible position for this
					// star by itself, just one that happens to disagree
					// with this frame's group consensus by a bit more than
					// patternMaxJump allows. reacquireStar failing to find
					// something even better near the consensus-predicted
					// spot isn't evidence the direct measurement is wrong,
					// only that it couldn't be improved on. Falling back
					// to it (instead of failing the whole frame) avoids
					// discarding a good measurement over noise-level
					// pattern disagreement, and — critically — still
					// advances this star's tracked position, so a later
					// frame isn't left comparing against an increasingly
					// stale one. A star that failed its own tolerance
					// AND the pattern check has no such fallback: nothing
					// here vouches for its direct measurement, so a failed
					// reacquire still fails the frame via patternMismatchErr,
					// letting the caller retry with detectFieldShift.
					measurements[i] = m
					continue
				}
				return nil, false, patternMismatchError(m.name, deviation, patternMaxJump), nil
			}
			reacquiredAny = true
			measurements[i] = trackedStarMeasurement{
				name:  m.name,
				lastX: m.lastX, lastY: m.lastY,
				res:                reacquired,
				disp:               photometry.Displacement{DX: reacquired.X - m.lastX, DY: reacquired.Y - m.lastY},
				withinOwnTolerance: true,
			}
		}
		return measurements, reacquiredAny, nil, nil
	}

	measurements, reacquiredAny, patternMismatchErr, hardErr := attempt(lastX, lastY)
	if hardErr != nil {
		return photometry.Result{}, nil, 0, hardErr
	}
	if patternMismatchErr != nil {
		// The ordinary pass (seeded at last-known positions) couldn't
		// reconcile every star with the group consensus. Before giving up,
		// try ONE wide-area detectFieldShift scan — expensive (a
		// DetectStars pass over the whole image) but only paid on this
		// failure path, not every frame — to check whether a shared
		// field shift (e.g. the observer recentering the telescope)
		// rather than an ordinary wrong-star lock explains the mismatch.
		// If found, retry the whole attempt seeded from the shifted
		// positions; a real shared shift should then let every star pass
		// its own tolerance directly, without needing the pattern-check's
		// per-star reacquire fallback at all.
		if shiftDX, shiftDY, ok := detectFieldShift(img, names, lastX, lastY, cfg); ok {
			seedX := make([]float64, len(lastX))
			seedY := make([]float64, len(lastY))
			for i := range lastX {
				seedX[i] = lastX[i] + shiftDX
				seedY[i] = lastY[i] + shiftDY
			}
			retried, retriedReacquired, retriedMismatchErr, retriedHardErr := attempt(seedX, seedY)
			if retriedHardErr != nil {
				return photometry.Result{}, nil, 0, retriedHardErr
			}
			if retriedMismatchErr == nil {
				measurements, reacquiredAny = retried, retriedReacquired
			} else {
				return photometry.Result{}, nil, 0, patternMismatchErr
			}
		} else {
			return photometry.Result{}, nil, 0, patternMismatchErr
		}
	}

	// All stars resolved: commit tracked positions and build the result.
	targetRes = measurements[0].res
	targetDrift = photometry.Distance(measurements[0].lastX, measurements[0].lastY, targetRes.X, targetRes.Y)
	t.targetX, t.targetY = targetRes.X, targetRes.Y
	t.targetFlux = targetRes.NetFlux

	compResults = make([]photometry.Result, len(t.compNames))
	for i, m := range measurements[1:] {
		compResults[i] = m.res
		t.compX[i], t.compY[i] = m.res.X, m.res.Y
		t.compFlux[i] = m.res.NetFlux
	}

	if reacquiredAny {
		t.lastReacquireGap = t.framesSinceSuccess
	}
	t.framesSinceSuccess = 0
	return targetRes, compResults, targetDrift, nil
}

// patternMismatchError reports that a tracked star's frame-to-frame
// displacement disagreed with the rest of the group's consensus
// displacement by more than the pattern tolerance allows, even after a
// consensus-seeded reacquisition attempt — distinct from an ordinary
// fieldShiftError so the Series inspector can show the real cause: a
// likely lock onto the wrong (but real, nearby) star, not a genuine
// field shift affecting the whole frame.
func patternMismatchError(name string, deviation, maxDeviation float64) error {
	return fmt.Errorf("%s: displacement disagrees with the rest of the group (%.1fpx from consensus, max %.1fpx) — wrong-star lock suspected", name, deviation, maxDeviation)
}

// fieldShiftError reports that a tracked star moved farther between two
// consecutive frames than the current tolerance level allows.
func fieldShiftError(who string, prevX, prevY, newX, newY float64, level int, maxPixels float64) error {
	dist := math.Hypot(newX-prevX, newY-prevY)
	return fmt.Errorf("%s: field shift of %.1fpx exceeds tolerance (level %d, max %.1fpx)", who, dist, level, maxPixels)
}

const (
	// reacquireGrowthPxPerFrame is how many extra pixels of local search
	// radius (beyond the ordinary centroid box) are allowed per skipped
	// frame, and reacquireToleranceGrowthPxPerFrame is the matching
	// per-frame growth applied to the acceptance check on the reacquired
	// position — both capped at reacquireMaxGapFrames worth of growth so
	// an extremely long unattended gap doesn't grow the search area
	// without bound. Deliberately modest: the goal is to bridge ordinary
	// short exclusion gaps, not to paper over long stretches where the
	// star should really be re-marked by hand.
	reacquireGrowthPxPerFrame          = 2.0
	reacquireToleranceGrowthPxPerFrame = 1.5
	reacquireMaxGapFrames              = 15
	reacquireMinCounts                 = 1000.0 // absolute floor only, same as detectStarsAction; DetectStars combines it with an adaptive per-region threshold

	// fluxToleranceRatio is how many times brighter OR fainter than
	// expected a reacquired candidate's NetFlux is allowed to be. Wide on
	// purpose: NetFlux legitimately varies frame to frame with seeing,
	// transparency, and airmass (that's the signal Starflux measures), and
	// this check only needs to catch a categorically different star, not
	// ordinary photometric scatter. A real reported case had a wrong
	// candidate at ~1/7th (~0.15x) the expected flux; 3x in either
	// direction stays comfortably clear of normal variation while still
	// rejecting that kind of wrong-star mismatch.
	fluxToleranceRatio = 3.0
)

// fluxWithinTolerance reports whether candidateFlux is within
// fluxToleranceRatio of expectedFlux in either direction (brighter or
// fainter). Both non-positive fluxes are treated as within tolerance
// (nothing meaningful to compare); a non-positive candidateFlux against a
// positive expectedFlux is always out of tolerance.
func fluxWithinTolerance(expectedFlux, candidateFlux float64) bool {
	if expectedFlux <= 0 {
		return true
	}
	if candidateFlux <= 0 {
		return false
	}
	ratio := candidateFlux / expectedFlux
	return ratio >= 1/fluxToleranceRatio && ratio <= fluxToleranceRatio
}

// reacquireGapFrames combines framesSinceSuccess (skipped/errored attempts
// counted by index within s.series) with cadenceGap (elapsed time between
// this and the last attempted image, in units of the series' typical
// frame interval — see jdCadenceGap) into the gapFrames value passed to
// reacquireStar, taking whichever signal indicates the larger real gap.
// framesSinceSuccess alone floored at 1 ensures the very first
// tolerance/measure failure after a success still gets a (minimally)
// grown search radius instead of none at all (it's 0 at that point since
// it's only incremented after the failure is recorded, but the failing
// star is already effectively one frame out of date). cadenceGap alone
// catches the complementary case framesSinceSuccess can't see at all: a
// file missing from disk between two present, back-to-back-in-s.series
// images, which advances no skip counter but still means real time (and
// real possible displacement) elapsed.
func reacquireGapFrames(framesSinceSuccess, cadenceGap int) int {
	gap := framesSinceSuccess
	if gap < 1 {
		gap = 1
	}
	if cadenceGap > gap {
		gap = cadenceGap
	}
	return gap
}

// reacquireStar attempts to relocate a star that drifted more than the
// ordinary per-frame tolerance allows, after gapFrames frames were skipped
// (excluded or errored) since it was last successfully tracked. It scans a
// LOCALLY BOUNDED region (photometry.DetectStars over a boundedPixelSource,
// radius growing with gapFrames up to reacquireMaxGapFrames) centered on
// the star's last-known-good position (lastX, lastY) — deliberately NOT
// the star's original manually-marked position, since the last-known-good
// position is the best available estimate after a short gap.
//
// Among DetectStars' candidates, it picks the one CLOSEST to
// (lastX, lastY) — not the brightest — since the goal is reacquiring one
// specific known star, not bulk field detection; picking by brightness
// risks locking onto an unrelated brighter neighbor. The closest candidate
// must still fall within a gap-scaled tolerance allowance; if no candidate
// exists in the region, or the closest one still exceeds the gap-scaled
// allowance, this returns an error and the caller falls back to its
// existing fieldShiftError path — reacquisition never silently accepts a
// candidate it can't justify by proximity to where the star was expected.
// This is what keeps a genuine field-shift/cloud/mount-slew event failing
// exactly as before: DetectStars won't find a plausible candidate near an
// uncorrupted last-known position if the star truly isn't there anymore.
//
// If expectedFlux is positive, the refined candidate's own NetFlux is also
// checked against it (within fluxToleranceRatio) before being accepted.
// Proximity alone isn't sufficient discrimination in a crowded field: a
// real reported case had reacquireStar geometrically accept a candidate
// within its gap-scaled position tolerance that was actually a different,
// ~7x fainter star sitting close to where the tracked (and much brighter)
// star was expected — proximity to the last-known position doesn't imply
// it's the same star, since a genuinely large gap (e.g. a file missing
// from the series) can put a wrong-but-nearby neighbor closer to that
// stale position than the real star's new one. expectedFlux == 0 (no
// prior successful measurement yet for this star) skips the check
// entirely, since there's nothing yet to compare against.
func reacquireStar(img *fits.Image, lastX, lastY float64, gapFrames int, expectedFlux float64, cfg config.Config) (photometry.Result, error) {
	growthFrames := gapFrames
	if growthFrames > reacquireMaxGapFrames {
		growthFrames = reacquireMaxGapFrames
	}
	searchHalfWidth := float64(cfg.CentroidHalfWidth) + float64(growthFrames)*reacquireGrowthPxPerFrame
	gapMaxJump := toleranceLevelToPixels(cfg.ToleranceLevel) + float64(growthFrames)*reacquireToleranceGrowthPxPerFrame

	region := boundedRegion(img, lastX, lastY, searchHalfWidth)
	candidates := photometry.DetectStars(region, reacquireMinCounts, 1, photometry.DefaultSigmaMultiplier)
	if len(candidates) == 0 {
		return photometry.Result{}, fmt.Errorf("reacquireStar: no candidate found within %.1fpx of (%.1f, %.1f)", searchHalfWidth, lastX, lastY)
	}

	best := closestDetection(candidates, lastX-float64(region.x0), lastY-float64(region.y0))
	bestX := best.X + float64(region.x0)
	bestY := best.Y + float64(region.y0)
	if photometry.Distance(lastX, lastY, bestX, bestY) > gapMaxJump {
		return photometry.Result{}, fmt.Errorf("reacquireStar: closest candidate at (%.1f, %.1f) exceeds gap-scaled tolerance %.1fpx", bestX, bestY, gapMaxJump)
	}

	// The final sub-pixel refinement below must not use a search box wide
	// enough to pull in a DIFFERENT candidate DetectStars already
	// resolved as a separate source — otherwise this step silently undoes
	// the discrimination DetectStars/closestDetection just did, biasing
	// the centroid back toward a nearby-but-wrong neighbor (this was a
	// real bug: a crowded pair of stars correctly told apart by
	// DetectStars, then re-blended by an unrestricted refinement pass).
	// Cap the refinement half-width at half the distance to the nearest
	// OTHER candidate, so its search box can never reach that neighbor.
	refineHalfWidth := float64(cfg.CentroidHalfWidth)
	if nearest, ok := nearestOtherCandidateDistance(candidates, best); ok {
		if maxSafe := nearest / 2; maxSafe < refineHalfWidth {
			refineHalfWidth = maxSafe
		}
	}
	if refineHalfWidth < 1 {
		refineHalfWidth = 1
	}

	res, err := photometry.Measure(img, int(bestX), int(bestY), int(refineHalfWidth), cfg.Aperture)
	if err != nil {
		return photometry.Result{}, fmt.Errorf("reacquireStar: %w", err)
	}
	if expectedFlux > 0 && !fluxWithinTolerance(expectedFlux, res.NetFlux) {
		return photometry.Result{}, fmt.Errorf("reacquireStar: candidate at (%.1f, %.1f) has flux %.0f, too far from expected %.0f — likely a different, wrong star", res.X, res.Y, res.NetFlux, expectedFlux)
	}
	return res, nil
}

// nearestOtherCandidateDistance returns the distance from best to the
// closest OTHER detection in candidates, and whether any other candidate
// exists at all (false if best is the only one).
func nearestOtherCandidateDistance(candidates []photometry.Detection, best photometry.Detection) (float64, bool) {
	found := false
	var nearest float64
	for _, c := range candidates {
		if c == best {
			continue
		}
		d := photometry.Distance(best.X, best.Y, c.X, c.Y)
		if !found || d < nearest {
			nearest = d
			found = true
		}
	}
	return nearest, found
}

// closestDetection returns the candidate nearest (x, y). candidates must
// be non-empty.
func closestDetection(candidates []photometry.Detection, x, y float64) photometry.Detection {
	best := candidates[0]
	bestDist := photometry.Distance(x, y, best.X, best.Y)
	for _, c := range candidates[1:] {
		if d := photometry.Distance(x, y, c.X, c.Y); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

// boundedPixelSource restricts a photometry.PixelSource to a rectangular
// sub-region, presenting local (0,0)-origin coordinates to a wrapped scan
// (photometry.DetectStars). Callers must translate returned Detection
// coordinates back to the source's coordinate space by adding x0/y0.
type boundedPixelSource struct {
	src            photometry.PixelSource
	x0, y0, x1, y1 int // inclusive bounds in src's coordinate space
}

func boundedRegion(src photometry.PixelSource, cx, cy, halfWidth float64) boundedPixelSource {
	x0 := clampInt(int(cx-halfWidth), 0, src.Width()-1)
	y0 := clampInt(int(cy-halfWidth), 0, src.Height()-1)
	x1 := clampInt(int(cx+halfWidth), 0, src.Width()-1)
	y1 := clampInt(int(cy+halfWidth), 0, src.Height()-1)
	return boundedPixelSource{src: src, x0: x0, y0: y0, x1: x1, y1: y1}
}

func (b boundedPixelSource) Width() int  { return b.x1 - b.x0 + 1 }
func (b boundedPixelSource) Height() int { return b.y1 - b.y0 + 1 }
func (b boundedPixelSource) At(x, y int) float64 {
	return b.src.At(b.x0+x, b.y0+y)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// showSeriesInspectorDialog lists every image in the loaded series with its
// most recent processing status, an "Inspect" button to preview it, and a
// checkbox to include/exclude it from the next Process run. It is rebuilt
// from scratch every time it's opened (Fyne dialogs aren't kept alive
// across closes), so it always reflects the current appState — including
// updated statuses immediately after a Process run, and included/excluded
// toggles set in a previous opening of this same dialog, since toggling a
// check here writes straight back into the *fits.Image in s.series.
func (s *appState) showSeriesInspectorDialog() {
	if len(s.series) == 0 {
		dialog.ShowInformation("Starflux", noSeriesLoadedMsg, s.win)
		return
	}

	rows := container.NewVBox()
	okCount, errCount, excludedCount, pendingCount := 0, 0, 0, 0

	for _, img := range s.series {
		img := img // capture

		st := s.imageStatus[img.Path] // zero value == statusPending if absent
		switch {
		case !img.Included:
			excludedCount++
		case st.kind == statusOK:
			okCount++
		case st.kind == statusError:
			errCount++
		default:
			pendingCount++
		}

		statusText := st.kind.String()
		if !img.Included {
			statusText = statusExcluded.String()
		}
		if st.kind == statusError && st.message != "" {
			statusText = fmt.Sprintf("Error: %s", st.message)
		}
		if st.kind == statusOK && st.recoveredAfterGap > 0 {
			statusText = fmt.Sprintf("%s (re-acquired after %d skipped frame(s))", statusText, st.recoveredAfterGap)
		}

		nameLabel := widget.NewLabel(filepath.Base(img.Path))
		statusLabel := widget.NewLabel(statusText)

		inspectButton := widget.NewButton("Inspect", func() {
			previewStars := st.trackedStars
			if previewStars == nil {
				previewStars = s.stars
			}
			showImagePreviewDialog(s.win, img, s.levels, previewStars)
		})

		includeCheck := widget.NewCheck("Include", func(checked bool) {
			img.Included = checked
		})
		includeCheck.SetChecked(img.Included)

		rows.Add(container.NewHBox(nameLabel, statusLabel, inspectButton, includeCheck))
	}

	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(560, 420))

	summary := widget.NewLabel(fmt.Sprintf(
		"%d image(s): %d OK, %d error(s), %d excluded, %d not yet processed.",
		len(s.series), okCount, errCount, excludedCount, pendingCount,
	))

	dialog.NewCustom("Series inspector", "Close", container.NewVBox(summary, scroll), s.win).Show()
}

// showImagePreviewDialog renders a single image (using the app's current
// display levels, matching what the main view would show), with the
// current Target/Comparison/Check markers overlaid at their marked
// positions, in a small read-only dialog — so a user can visually confirm
// whether an image is flagged as an error because of a genuine field
// shift (markers land off the real stars) or something else (markers
// still sit correctly on the real stars), without disturbing the main
// window's own view/star state.
func showImagePreviewDialog(parent fyne.Window, img *fits.Image, levels ui.Levels, stars []ui.Star) {
	preview := ui.NewImageView()
	preview.SetImage(ui.Render(img, levels))
	preview.SetStars(stars)
	preview.Resize(fyne.NewSize(480, 480))

	d := dialog.NewCustom(filepath.Base(img.Path), "Close", preview, parent)
	d.Resize(fyne.NewSize(520, 560)) // leave room for the dialog chrome around the 480x480 view
	d.Show()
}

// showLightCurve renders a differential-magnitude light curve and displays
// it in a new dialog window, alongside buttons to view per-frame tracking
// drift, correct light-curve tilt, find a period, and export an AAVSO
// report. s/target/comps/watchedDir enable the watch-folder ("Enable watch
// folder...") button, matching FotoDif's placement of "Activar AUTO" in
// its own Graphs view — pass s as nil (with target/comps/watchedDir zero
// values) for curves that shouldn't offer live watching (e.g. a derived
// tilt-corrected view, or a bulk variable-search candidate's curve).
func showLightCurve(s *appState, parent fyne.Window, points []timeseries.Point, drift []ui.DriftPoint, filter, label string, target ui.Star, comps []ui.Star, watchedDir string) {
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

	driftButton := widget.NewButton("Show tracking drift", func() {
		showDriftDialog(parent, drift, label)
	})
	tiltButton := widget.NewButton("Correct tilt...", func() {
		showTiltDialog(parent, points, drift, filter, label)
	})
	periodButton := widget.NewButton("Find period...", func() {
		showPeriodDialog(parent, points, label)
	})
	exportButton := widget.NewButton("Export AAVSO report...", func() {
		showAAVSOExportDialog(parent, points, filter, label)
	})

	buttons := []fyne.CanvasObject{driftButton, tiltButton, periodButton, exportButton}
	if s != nil {
		if watchedDir == "" {
			buttons = append(buttons, widget.NewButton("Enable watch folder...", func() {
				dialog.ShowInformation("Starflux", "Watch-folder mode needs a series loaded from a folder (\"Open Folder...\"), not a single file or file list.", parent)
			}))
		} else {
			watchButton := widget.NewButton("Enable watch folder...", nil)
			watchButton.OnTapped = func() {
				s.toggleWatchFolder(parent, watchButton, watchedDir, target, comps, points, plotImg, drift, filter, label)
			}
			buttons = append(buttons, watchButton)
		}
	}

	content := append([]fyne.CanvasObject{summary, plotImg}, buttons...)
	d := dialog.NewCustom("Light curve", "Close", container.NewVBox(content...), parent)
	d.Show()
}

// toggleWatchFolder starts or stops watch-folder ("Activar AUTO"-style)
// mode: the same button both enables and disables watching, matching
// FotoDif's single-toggle UX. Starting is only allowed once — at most one
// watch runs at a time.
func (s *appState) toggleWatchFolder(parent fyne.Window, button *widget.Button, watchedDir string, target ui.Star, comps []ui.Star, points []timeseries.Point, plotImg *canvas.Image, drift []ui.DriftPoint, filter, label string) {
	if s.watchStop != nil {
		close(s.watchStop)
		s.watchStop = nil
		button.SetText("Enable watch folder...")
		return
	}

	s.watchStop = s.startWatchFolder(watchedDir, target, comps, points, plotImg, filter, label)
	button.SetText("Stop AUTO")
}

// startWatchFolder begins polling watchedDir every pollInterval for new
// FITS files not yet measured, appending each new point to a running copy
// of points (via a long-lived starTracker so a newly arrived file's
// centroid search continues from wherever the star was last tracked, not
// from its original marked position) and re-rendering plotImg in place.
// Returns the channel to close to stop polling.
func (s *appState) startWatchFolder(watchedDir string, target ui.Star, comps []ui.Star, points []timeseries.Point, plotImg *canvas.Image, filter, label string) chan struct{} {
	stopCh := make(chan struct{})

	seen := make(map[string]bool, len(s.series))
	for _, img := range s.series {
		seen[img.Path] = true
	}
	tracker := newStarTracker(target, comps)
	current := append([]timeseries.Point(nil), points...)

	go func() {
		const pollInterval = 5 * time.Second
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				newFiles, err := watch.FindNewFiles(watchedDir, seen)
				if err != nil {
					continue // transient dir-read failure; try again next tick
				}
				for _, path := range newFiles {
					seen[path] = true
					img, loadErr := fits.Load(path)
					if loadErr != nil {
						continue // one bad file shouldn't stop the watch; skip and keep polling
					}
					obs, measureErr := measureOneImage(tracker, img, s.cfg)
					if measureErr != nil {
						continue // tracking/tolerance failure on this file; skip, keep watching
					}
					pt, ptErr := timeseries.DifferentialMagnitude(obs)
					if ptErr != nil {
						continue
					}
					current = append(current, pt)
					updated := current
					fyne.Do(func() {
						newImg, plotErr := ui.PlotLightCurve(updated, label)
						if plotErr != nil {
							return
						}
						plotImg.Image = newImg
						plotImg.Refresh()
						if s.accumulatedPoints == nil {
							s.accumulatedPoints = make(map[string][]timeseries.Point)
						}
						s.accumulatedPoints[target.Name] = updated
					})
				}
			}
		}
	}()

	return stopCh
}

// showTiltDialog lets the user mark 1-2 JD baseline ranges, fits a line
// through the points in those ranges, and shows a NEW light-curve dialog
// with the correction subtracted. s.accumulatedPoints (the caller's
// `points`) is never modified here — reopening or reprocessing this target
// always shows the original, uncorrected data, matching FotoDif's
// presentational-only tilt-correction semantics. There is deliberately no
// explicit "Undo" button (unlike FotoDif's Deshacer): since nothing is
// mutated in place, the original dialog this was opened from is still
// there, untouched — undo is simply "look at the other dialog."
func showTiltDialog(parent fyne.Window, points []timeseries.Point, drift []ui.DriftPoint, filter, label string) {
	start1, end1 := widget.NewEntry(), widget.NewEntry()
	start2, end2 := widget.NewEntry(), widget.NewEntry()
	start2.SetPlaceHolder("optional")
	end2.SetPlaceHolder("optional")

	items := []*widget.FormItem{
		widget.NewFormItem("Baseline 1 start (JD)", start1),
		widget.NewFormItem("Baseline 1 end (JD)", end1),
		widget.NewFormItem("Baseline 2 start (JD, optional)", start2),
		widget.NewFormItem("Baseline 2 end (JD, optional)", end2),
	}

	dialog.NewForm("Correct tilt", "Correct", "Cancel", items, func(confirmed bool) {
		if !confirmed {
			return
		}
		ranges, err := parseTiltRanges(start1.Text, end1.Text, start2.Text, end2.Text)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		slope, intercept, err := timeseries.FitBaseline(points, ranges)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		corrected := timeseries.SubtractTilt(points, slope, intercept)
		showLightCurve(nil, parent, corrected, drift, filter, label+" (tilt-corrected)", ui.Star{}, nil, "")
	}, parent).Show()
}

// parseTiltRanges builds the JD range list FitBaseline expects from the
// tilt dialog's 2 or 4 text entries; the second range is optional (blank
// start2/end2 means one-range mode).
func parseTiltRanges(start1Text, end1Text, start2Text, end2Text string) ([][2]float64, error) {
	start1, err := strconv.ParseFloat(strings.TrimSpace(start1Text), 64)
	if err != nil {
		return nil, fmt.Errorf("baseline 1 start must be a number")
	}
	end1, err := strconv.ParseFloat(strings.TrimSpace(end1Text), 64)
	if err != nil {
		return nil, fmt.Errorf("baseline 1 end must be a number")
	}
	ranges := [][2]float64{{start1, end1}}

	start2Text, end2Text = strings.TrimSpace(start2Text), strings.TrimSpace(end2Text)
	if start2Text == "" && end2Text == "" {
		return ranges, nil
	}
	start2, err := strconv.ParseFloat(start2Text, 64)
	if err != nil {
		return nil, fmt.Errorf("baseline 2 start must be a number")
	}
	end2, err := strconv.ParseFloat(end2Text, 64)
	if err != nil {
		return nil, fmt.Errorf("baseline 2 end must be a number")
	}
	return append(ranges, [2]float64{start2, end2}), nil
}

// showPeriodDialog lets the user pick a min/max trial period range and a
// number of trial frequencies, runs a Lomb-Scargle periodogram, shows it,
// and auto-picks the strongest peak as a starting point for phase-folding
// — which the user can override, since a symmetric (double-humped) signal
// can produce a spuriously strong peak at exactly half the true period.
func showPeriodDialog(parent fyne.Window, points []timeseries.Point, label string) {
	minP, maxP, trials := defaultPeriodRange(points)

	minEntry := widget.NewEntry()
	minEntry.SetText(formatConfigFloat(minP))
	maxEntry := widget.NewEntry()
	maxEntry.SetText(formatConfigFloat(maxP))
	trialsEntry := widget.NewEntry()
	trialsEntry.SetText(strconv.Itoa(trials))

	items := []*widget.FormItem{
		widget.NewFormItem("Min period (days)", minEntry),
		widget.NewFormItem("Max period (days)", maxEntry),
		widget.NewFormItem("Number of trial periods", trialsEntry),
		widget.NewFormItem("", widget.NewLabel("Caution: symmetric (double-humped) signals\ncan show a false-strong peak at HALF the\ntrue period — check the phase fold looks\nclean before trusting the result.")),
	}

	dialog.NewForm("Find period (Lomb-Scargle)", "Compute", "Cancel", items, func(confirmed bool) {
		if !confirmed {
			return
		}
		minPeriod, maxPeriod, numTrials, err := parsePeriodForm(minEntry.Text, maxEntry.Text, trialsEntry.Text)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		gram, err := period.LombScargle(points, minPeriod, maxPeriod, numTrials)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		bestPeriod, bestPower := period.BestPeriod(gram)
		showPeriodogramDialog(parent, gram, points, label, bestPeriod, bestPower)
	}, parent).Show()
}

// defaultPeriodRange picks a sensible default trial-period window from the
// data's own sampling: min period is twice the median gap between
// consecutive JDs (roughly the shortest period statistically resolvable
// given the data's own cadence, a Nyquist-like floor), max period is the
// full time baseline (matching FotoDif's own "provisional" max-period
// initialization, "el tiempo... entre la primera medida y la última").
func defaultPeriodRange(points []timeseries.Point) (minPeriod, maxPeriod float64, numTrials int) {
	const defaultTrials = 1000
	if len(points) < 2 {
		return 0.01, 1.0, defaultTrials
	}
	sorted := append([]timeseries.Point(nil), points...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].JD < sorted[j].JD })

	gaps := make([]float64, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		gaps = append(gaps, sorted[i].JD-sorted[i-1].JD)
	}
	sort.Float64s(gaps)
	medianGap := gaps[len(gaps)/2]
	if medianGap <= 0 {
		medianGap = 0.001
	}

	baseline := sorted[len(sorted)-1].JD - sorted[0].JD
	if baseline <= 0 {
		baseline = 1.0
	}

	return 2 * medianGap, baseline, defaultTrials
}

// parsePeriodForm validates the period-search form's raw text entries.
func parsePeriodForm(minText, maxText, trialsText string) (minPeriod, maxPeriod float64, numTrials int, err error) {
	minPeriod, err = strconv.ParseFloat(strings.TrimSpace(minText), 64)
	if err != nil || minPeriod <= 0 {
		return 0, 0, 0, fmt.Errorf("min period must be a positive number")
	}
	maxPeriod, err = strconv.ParseFloat(strings.TrimSpace(maxText), 64)
	if err != nil || maxPeriod <= minPeriod {
		return 0, 0, 0, fmt.Errorf("max period must be a number greater than the min period")
	}
	numTrials, err = strconv.Atoi(strings.TrimSpace(trialsText))
	if err != nil || numTrials < 1 {
		return 0, 0, 0, fmt.Errorf("number of trial periods must be a positive integer")
	}
	return minPeriod, maxPeriod, numTrials, nil
}

// showPeriodogramDialog displays the computed periodogram plot and offers a
// follow-on phase-fold at the auto-picked best period (editable, since the
// harmonic-alias problem can mean the true period is actually half or
// double the automatically found peak).
func showPeriodogramDialog(parent fyne.Window, gram []period.PeriodogramPoint, points []timeseries.Point, label string, bestPeriod, bestPower float64) {
	img, err := ui.PlotPeriodogram(gram, label)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}
	plotImg := canvas.NewImageFromImage(img)
	plotImg.FillMode = canvas.ImageFillContain
	plotImg.SetMinSize(fyne.NewSize(500, 320))

	summary := widget.NewLabel(fmt.Sprintf("Best period found: %.6f days (power %.3f)", bestPeriod, bestPower))

	periodEntry := widget.NewEntry()
	periodEntry.SetText(formatConfigFloat(bestPeriod))

	foldButton := widget.NewButton("Show phase-folded curve", func() {
		p, err := strconv.ParseFloat(strings.TrimSpace(periodEntry.Text), 64)
		if err != nil || p <= 0 {
			dialog.ShowError(fmt.Errorf("period must be a positive number"), parent)
			return
		}
		folded, err := period.FoldPhase(points, p)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		showPhaseFoldDialog(parent, folded, p, label)
	})

	content := container.NewVBox(
		summary, plotImg,
		widget.NewLabel("Period to fold (days, editable — try P/2 or 2P if the fold above looks wrong):"),
		periodEntry, foldButton,
	)
	dialog.NewCustom("Periodogram", "Close", content, parent).Show()
}

// showPhaseFoldDialog displays a phase-folded light curve at a given period.
func showPhaseFoldDialog(parent fyne.Window, folded []timeseries.Point, periodDays float64, label string) {
	img, err := ui.PlotPhaseFold(folded, periodDays, label)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}
	plotImg := canvas.NewImageFromImage(img)
	plotImg.FillMode = canvas.ImageFillContain
	plotImg.SetMinSize(fyne.NewSize(500, 320))
	dialog.NewCustom("Phase-folded curve", "Close", plotImg, parent).Show()
}

// showAAVSOExportDialog collects the fields Starflux doesn't already know
// (star name, observer code, comparison/check star magnitudes, chart,
// notes) and, on confirm, saves the light curve as an AAVSO Extended
// Format report.
func showAAVSOExportDialog(parent fyne.Window, points []timeseries.Point, filter, starName string) {
	nameEntry := widget.NewEntry()
	nameEntry.SetText(starName)
	obsCodeEntry := widget.NewEntry()
	compNameEntry := widget.NewEntry()
	compMagEntry := widget.NewEntry()
	checkNameEntry := widget.NewEntry()
	checkMagEntry := widget.NewEntry()
	chartEntry := widget.NewEntry()
	notesEntry := widget.NewEntry()

	items := []*widget.FormItem{
		widget.NewFormItem("Star name / AUID", nameEntry),
		widget.NewFormItem("Observer code", obsCodeEntry),
		widget.NewFormItem("Comparison star name", compNameEntry),
		widget.NewFormItem("Comparison magnitude", compMagEntry),
		widget.NewFormItem("Check star name (optional)", checkNameEntry),
		widget.NewFormItem("Check magnitude (optional)", checkMagEntry),
		widget.NewFormItem("Chart ID", chartEntry),
		widget.NewFormItem("Notes (optional)", notesEntry),
	}

	dialog.NewForm("Export AAVSO report", "Next", "Cancel", items, func(confirmed bool) {
		if !confirmed {
			return
		}
		fields := aavsoFormFields{
			name:         nameEntry.Text,
			obsCode:      obsCodeEntry.Text,
			compName:     compNameEntry.Text,
			compMagText:  compMagEntry.Text,
			checkName:    checkNameEntry.Text,
			checkMagText: checkMagEntry.Text,
			chart:        chartEntry.Text,
			notes:        notesEntry.Text,
		}
		report, err := buildAAVSOReport(points, filter, fields)
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		go func() {
			path, err := zenity.SelectFileSave(zenity.FileFilters{aavsoFileFilter}, zenity.ConfirmOverwrite())
			if errors.Is(err, zenity.ErrCanceled) {
				return
			}
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, parent)
					return
				}
				if err := writeAAVSOFile(report, path); err != nil {
					dialog.ShowError(err, parent)
				}
			})
		}()
	}, parent).Show()
}

// aavsoFormFields bundles the AAVSO export dialog's raw text entries, so
// buildAAVSOReport doesn't need one parameter per field.
type aavsoFormFields struct {
	name, obsCode           string
	compName, compMagText   string
	checkName, checkMagText string
	chart, notes            string
}

// buildAAVSOReport validates and assembles an aavso.Report from the export
// dialog's raw text entries. Validation is deliberately minimal (only star
// name and observer code are required) rather than replicating FotoDif's
// fuller internal consistency engine — a v1 simplification.
func buildAAVSOReport(points []timeseries.Point, filter string, fields aavsoFormFields) (aavso.Report, error) {
	name := strings.TrimSpace(fields.name)
	obsCode := strings.TrimSpace(fields.obsCode)
	if name == "" {
		return aavso.Report{}, fmt.Errorf("star name / AUID is required")
	}
	if obsCode == "" {
		return aavso.Report{}, fmt.Errorf("observer code is required")
	}

	report := aavso.Report{
		Points:       points,
		Filter:       filter,
		StarName:     name,
		ObserverCode: obsCode,
		CompName:     strings.TrimSpace(fields.compName),
		CheckName:    strings.TrimSpace(fields.checkName),
		Chart:        strings.TrimSpace(fields.chart),
		Notes:        strings.TrimSpace(fields.notes),
	}

	if compMagText := strings.TrimSpace(fields.compMagText); compMagText != "" {
		mag, err := strconv.ParseFloat(compMagText, 64)
		if err != nil {
			return aavso.Report{}, fmt.Errorf("comparison magnitude must be a number")
		}
		report.CompMag, report.HasCompMag = mag, true
	}
	if checkMagText := strings.TrimSpace(fields.checkMagText); checkMagText != "" {
		mag, err := strconv.ParseFloat(checkMagText, 64)
		if err != nil {
			return aavso.Report{}, fmt.Errorf("check magnitude must be a number")
		}
		report.CheckMag, report.HasCheckMag = mag, true
	}

	return report, nil
}

// writeAAVSOFile creates path and writes report to it in AAVSO Extended
// Format.
func writeAAVSOFile(report aavso.Report, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("aavso: create %s: %w", path, err)
	}
	defer f.Close()
	return aavso.WriteExtendedFormat(f, report)
}

// showDriftDialog renders and displays the target's per-frame tracking
// displacement in a separate dialog, opened on demand from the light-curve
// dialog rather than always rendered up front (most sessions have well-
// behaved tracking and don't need this diagnostic).
func showDriftDialog(parent fyne.Window, drift []ui.DriftPoint, label string) {
	img, err := ui.PlotDrift(drift, label)
	if err != nil {
		dialog.ShowError(err, parent)
		return
	}
	plotImg := canvas.NewImageFromImage(img)
	plotImg.FillMode = canvas.ImageFillContain
	plotImg.SetMinSize(fyne.NewSize(500, 320))
	dialog.NewCustom("Tracking drift", "Close", plotImg, parent).Show()
}
