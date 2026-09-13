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
	s.series = loaded
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
		dialog.ShowInformation("Starflux", "Open a FITS image or folder first.", s.win)
		return
	}

	const minCounts = 1000.0  // TODO: consider exposing as a config.Config field in a future release
	const minSeparation = 8.0 // px; larger than a typical aperture radius to avoid double-detecting one star

	detections := photometry.DetectStars(s.series[0], minCounts, minSeparation)
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
		dialog.ShowInformation("Starflux", "Open a FITS image or folder first.", s.win)
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
		result := s.measureSeries(target, comps)

		newPoints, seriesErrs := timeseries.Series(result.obs)
		allPoints := timeseries.MergeSorted(s.accumulatedPoints[target.Name], newPoints)

		if len(allPoints) == 0 {
			dialog.ShowError(fmt.Errorf("%s: no valid measurements across %d image(s): %v", target.Name, len(s.series), seriesErrs), s.win)
			continue
		}

		// Persist for a future "continue previous session" run, whether or
		// not this run stopped early — today's clean run can be tomorrow's
		// "previous session" if a bad frame shows up later.
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

		if result.stopErr != nil {
			dialog.ShowError(seriesStoppedError(result.stoppedAt, result.stopErr, len(allPoints)), s.win)
		} else if len(seriesErrs) > 0 {
			dialog.ShowInformation("Starflux", fmt.Sprintf("%s: %d epoch(s) skipped: %v", target.Name, len(seriesErrs), seriesErrs), s.win)
		}
	}
}

// seriesStoppedError formats the message shown when a series measurement
// stops partway through, naming the file that failed and how to resume.
func seriesStoppedError(stoppedAt string, cause error, pointsSoFar int) error {
	return fmt.Errorf(
		"stopped at %s: %v\n\n%d point(s) measured so far are shown above.\nFix or remove this image, then reload the remaining files with \"New session\" unchecked to continue this run.",
		filepath.Base(stoppedAt), cause, pointsSoFar,
	)
}

// measureSeriesResult is measureSeries's outcome: obs holds every image
// successfully measured, in order; stoppedAt/stopErr describe the first
// image that failed, if any. A full success has stopErr == nil.
type measureSeriesResult struct {
	obs       []timeseries.Observation
	drift     []float64 // per-frame target displacement in pixels, index-aligned with obs
	stoppedAt string    // img.Path of the first image that failed to measure; "" if none did
	stopErr   error
}

// measureSeries runs aperture photometry for one target star and combines
// all comparison stars (by averaged flux, see photometry.CombineComparisons)
// into a synthetic comparison measurement, for every image in the series.
// Each star's position is tracked frame to frame: the first image seeds
// from the star's manually marked position, and every following image
// seeds its centroid search from the previous image's refined position
// instead of the original fixed coordinates, matching FotoDif's handling
// of small field drift over a session. A frame-to-frame jump larger than
// s.toleranceLevel allows (see toleranceLevelToPixels) is treated as a
// real problem — a cloud, a bad recentering — rather than ordinary drift.
// measureSeries stops at the first image that fails to measure or whose
// tracked star jumped too far, rather than aborting the whole run, so
// everything measured up to that point is preserved and can be shown to
// the user (and later resumed) instead of discarded.
func (s *appState) measureSeries(target ui.Star, comps []ui.Star) measureSeriesResult {
	tracked := newStarTracker(target, comps)

	obs := make([]timeseries.Observation, 0, len(s.series))
	for _, img := range s.series {
		ob, err := measureOneImage(tracked, img, s.cfg)
		if err != nil {
			return measureSeriesResult{obs: obs, drift: tracked.targetDrift, stoppedAt: img.Path, stopErr: err}
		}
		obs = append(obs, ob)
	}
	return measureSeriesResult{obs: obs, drift: tracked.targetDrift}
}

// measureOneImage measures target and combined comparison flux for a
// single image using tracker's current tracked position, advancing tracker
// on success. This is the per-image body factored out of measureSeries's
// loop so watch-folder mode (startWatchFolder) can reuse the exact same
// tracking/tolerance logic one new image at a time, instead of re-deriving
// it — tracker is deliberately long-lived across calls in that case.
func measureOneImage(tracker *starTracker, img *fits.Image, cfg config.Config) (timeseries.Observation, error) {
	targetRes, err := tracker.measureTarget(img, cfg)
	if err != nil {
		return timeseries.Observation{}, err
	}
	compResults, err := tracker.measureComps(img, cfg)
	if err != nil {
		return timeseries.Observation{}, err
	}
	combinedComp, err := photometry.CombineComparisons(compResults)
	if err != nil {
		return timeseries.Observation{}, err
	}
	return timeseries.Observation{JD: img.JD, Target: targetRes, Comp: combinedComp}, nil
}

// starTracker holds each star's current tracked position for one
// measureSeries run, seeded from the stars' manually marked positions and
// updated to each frame's refined centroid as the series is measured.
type starTracker struct {
	targetName       string
	targetX, targetY float64
	targetDrift      []float64 // per-frame photometry.Distance(prev, new) for the target, one entry per successfully measured frame

	compNames []string
	compX     []float64
	compY     []float64
}

func newStarTracker(target ui.Star, comps []ui.Star) *starTracker {
	t := &starTracker{
		targetName: target.Name,
		targetX:    target.X,
		targetY:    target.Y,
		compNames:  make([]string, len(comps)),
		compX:      make([]float64, len(comps)),
		compY:      make([]float64, len(comps)),
	}
	for i, c := range comps {
		t.compNames[i] = c.Name
		t.compX[i] = c.X
		t.compY[i] = c.Y
	}
	return t
}

// measureTarget measures the target star at its currently tracked
// position, advancing that position on success.
func (t *starTracker) measureTarget(img *fits.Image, cfg config.Config) (photometry.Result, error) {
	maxJump := toleranceLevelToPixels(cfg.ToleranceLevel)
	res, err := photometry.Measure(img, int(t.targetX), int(t.targetY), cfg.CentroidHalfWidth, cfg.Aperture)
	if err != nil {
		return photometry.Result{}, fmt.Errorf("%s: target: %w", t.targetName, err)
	}
	dist := photometry.Distance(t.targetX, t.targetY, res.X, res.Y)
	if dist > maxJump {
		return photometry.Result{}, fieldShiftError(t.targetName, t.targetX, t.targetY, res.X, res.Y, cfg.ToleranceLevel, maxJump)
	}
	t.targetDrift = append(t.targetDrift, dist)
	t.targetX, t.targetY = res.X, res.Y
	return res, nil
}

// measureComps measures every comparison star at its currently tracked
// position, advancing each on success, stopping at the first failure.
func (t *starTracker) measureComps(img *fits.Image, cfg config.Config) ([]photometry.Result, error) {
	maxJump := toleranceLevelToPixels(cfg.ToleranceLevel)
	results := make([]photometry.Result, 0, len(t.compNames))
	for i, name := range t.compNames {
		r, err := photometry.Measure(img, int(t.compX[i]), int(t.compY[i]), cfg.CentroidHalfWidth, cfg.Aperture)
		if err != nil {
			return nil, fmt.Errorf("comparison %s: %w", name, err)
		}
		if !photometry.WithinTolerance(t.compX[i], t.compY[i], r.X, r.Y, maxJump) {
			return nil, fieldShiftError(name, t.compX[i], t.compY[i], r.X, r.Y, cfg.ToleranceLevel, maxJump)
		}
		t.compX[i], t.compY[i] = r.X, r.Y
		results = append(results, r)
	}
	return results, nil
}

// fieldShiftError reports that a tracked star moved farther between two
// consecutive frames than the current tolerance level allows.
func fieldShiftError(who string, prevX, prevY, newX, newY float64, level int, maxPixels float64) error {
	dist := math.Hypot(newX-prevX, newY-prevY)
	return fmt.Errorf("%s: field shift of %.1fpx exceeds tolerance (level %d, max %.1fpx)", who, dist, level, maxPixels)
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
