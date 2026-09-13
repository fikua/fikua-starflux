# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once a first tagged release is published.

## [Unreleased]

### Added
- Named, multi-star sessions: click a star to name it and assign it a role (Target/Comparison/Check) via a dialog, matching FotoDif's workflow — replaces the old single-star-per-role model.
- `photometry.CombineComparisons`: combines multiple comparison stars into one synthetic measurement by averaging flux (not magnitude), the statistically correct way to reduce noise across several comparison stars.
- Each Target star now produces its own light curve, measured against the flux-averaged combination of all Comparison stars.
- A "Remove star" control to delete an individual star from the session without clearing all of them.
- `internal/session`: save and load a session's named star positions as JSON ("Save stars..." / "Load stars..." buttons), so re-opening a series doesn't require re-marking every star by hand.
- The star-naming dialog now shows the peak ADU under the cursor (a guide against saturation), and, when the FITS header provides them, the instrumental magnitude (from `MZERO`) and the `FILTER` used — matching FotoDif's star selection window. `photometry.MaxADU` computes the peak; `fits.Image` gains `MZero`/`HasMZero`/`Filter` fields.
- Resumable processing, matching FotoDif's "primera serie" workflow: if photometry fails partway through a series (a bad frame, a field shift), the partial light curve up to that point is now shown immediately instead of discarding everything measured so far. A new "New session" checkbox (checked by default) controls whether the next load discards previous results or accumulates onto them — uncheck it, reload the remaining good files, and the new points merge with what was already measured, sorted by Julian Date. `timeseries.MergeSorted` implements the merge.
- Automatic star tracking across a series, matching FotoDif's field-drift handling: each star's position is now seeded from the previous image's refined centroid instead of always reusing the position marked on the first image, so ordinary telescope tracking drift no longer risks losing the star out of the measurement box over a long session.
- Error tolerance control, matching FotoDif's 1-9 "control de tolerancia" slider: a new slider (default level 7, strict end) sets the maximum pixel jump allowed between a tracked star's position in consecutive frames. A larger jump — a cloud, a bad recentering, a genuine field shift — now stops the series at that image via the existing resumable-processing flow instead of silently measuring the wrong thing. `photometry.WithinTolerance` implements the check; `toleranceLevelToPixels` maps the 1-9 scale to a pixel threshold.
- `internal/config`: a persistent, auto-loaded/auto-saved configuration (centroid box half-width, aperture/annulus radii, field-shift tolerance level) replacing Starflux's previously hardcoded measurement constants, matching FotoDif's "Fotometría" configuration section. A new "Configuration..." button opens an editable form; the field-shift tolerance slider has moved out of the main window into this dialog, as the single place it's now set.
- `session.SaveFull`/`session.LoadFull`: save and restore a full session — star positions AND every target's accumulated light-curve points — as one JSON file, matching FotoDif's "Guardar/Recuperar datos" ("restores the program to the same state it was saved in"). New "Save session..." / "Load session..." buttons sit alongside the existing star-positions-only "Save stars.../Load stars...", which remain unchanged for users who only want to reuse star placements.
- `photometry.Distance` (factored out of `WithinTolerance`) and a new "Show tracking drift" plot in the light-curve dialog: each target star's frame-to-frame tracking displacement is now recorded during processing and can be plotted (JD vs. pixel displacement) via `ui.PlotDrift`, surfacing tracking-quality issues that previously only showed up as an outright tolerance-exceeded stop.
- `timeseries.FitBaseline`/`timeseries.SubtractTilt`: light-curve tilt correction, matching FotoDif's "Corrección de la inclinación". A new "Correct tilt..." button in the light-curve dialog fits a line through one or two user-specified JD baseline ranges and shows a new dialog with the tilt subtracted — purely presentational, the original accumulated points are never modified.
- `internal/aavso`: writes light-curve results in the AAVSO Extended File Format. A new "Export AAVSO report..." button in the light-curve dialog collects the remaining required fields (observer code, comparison star name/magnitude, chart ID) and saves a `.txt` submission file. Airmass and unmeasured check-star fields use AAVSO's own documented `NA` placeholder, since Starflux doesn't compute airmass and check stars are optional.
- `photometry.DetectStars`: a simple bulk star-finder (local-maxima above a minimum ADU threshold, with minimum-separation de-duplication) for crowded fields, matching FotoDif's "Selección automática de estrellas". A new "Detect stars..." button scans the first loaded image and, after a confirmation showing the candidate count, adds new stars as Comparison-role candidates.
- `internal/period`: a from-scratch Lomb-Scargle periodogram implementation (`period.LombScargle`, `period.BestPeriod`, `period.FoldPhase`) for finding periodic signals in unevenly-sampled light curves, matching FotoDif's "Análisis de período" concept but using the astronomically standard Lomb-Scargle method (chosen over FotoDif's own undocumented phase-dispersion method) instead. A new "Find period..." button in the light-curve dialog opens a min/max period + trial-count form, shows the resulting periodogram (`ui.PlotPeriodogram`), and offers a follow-on phase-folded plot (`ui.PlotPhaseFold`) at the best-found period (editable, to correct for the well-known harmonic-alias ambiguity where symmetric signals can show a false-strong peak at half the true period).
- `timeseries.Dispersion`/`timeseries.MedianErr`/`timeseries.VariabilityRatio`/`timeseries.IsPossibleVariable`: a dispersion-based statistical filter for flagging possible variable stars among bulk-detected field stars, matching FotoDif's "Búsqueda de variables" design goal of being deliberately generous (tolerates false positives rather than missing real variables). A new "Search variables..." button measures every currently-detected Comparison-role star as its own target (against every other marked star as a shared baseline) and shows a sorted results list with each candidate's scatter-to-error ratio and a button to open its individual light curve.
- Automatic (watch-folder) processing mode, matching FotoDif's "Proceso automático"/"Activar AUTO": a new "Enable watch folder..." button in the light-curve dialog (available once a series has been loaded from a folder and processed at least once) polls the source folder every 5 seconds for new FITS files, measures each one using the same Target/Comparison stars and frame-to-frame tracking as ordinary processing, and appends the result to the live light curve in place. A "Stop AUTO" toggle cleanly stops polling; `fits.IsFITSExt` is now exported and a new `internal/watch` package (`watch.FindNewFiles`) supports the new-file detection.

### Changed
- `ui.Marker` renamed to `ui.Star` (adds a `Name` field); `ImageView.AddMarker`/`ClearMarkers` replaced by `SetStars([]Star)`, which redraws the full marker overlay from a single list — fixes a bug where re-tapping the same role left a visually orphaned marker no longer tracked by the app.
- `measureSeries` no longer aborts an entire target's measurement on the first image that fails photometry — it now returns everything measured up to that point, plus which image stopped it and why.
- `measureSeries`'s per-image measurement logic is now factored out into `measureOneImage`, reused by the new watch-folder polling loop; behavior is unchanged.
- `showLightCurve` now also accepts the target/comparison stars and source folder used to produce a curve (needed to offer watch-folder mode); its dialog gained "Find period..." and "Enable watch folder..." buttons alongside the existing drift/tilt/export ones.
- `photometry.WithinTolerance` is now implemented in terms of the new `photometry.Distance` helper; behavior and signature are unchanged.
- `showLightCurve` now also accepts and surfaces per-frame drift data, and its dialog gained "Show tracking drift", "Correct tilt...", and "Export AAVSO report..." buttons alongside the existing plot.
- Reorganized the main window: rarely-used actions (Open FITS/Files/Folder, Save/Load stars and sessions, "New session", Configuration, Observer mode) moved into a native menu bar (File/Session/Tools), and the sidebar — now scrollable, so it's no longer possible to lose access to controls on a small screen — is grouped into Series/Stars/Display/Process sections with separators, keeping only the controls used during active work.

## [0.1.0] - 2026-09-12

First testable build. Single-image workflow only: open one FITS file,
place target/comparison markers, and produce a single differential
magnitude point. No multi-image session batching, image alignment, or
code signing/notarization yet — expect Gatekeeper/SmartScreen warnings
on first launch.

### Added
- Project bootstrap: Go module, Fyne-based desktop app skeleton, Apache 2.0 license.
- `internal/fits`: load FITS images and extract header metadata (INSTRUME, EXPTIME, DATE-OBS).
- `internal/photometry`: centroid refinement, aperture flux summation with subpixel edge coverage, sky background via annulus median.
- `internal/timeseries`: differential magnitude (Δm) computation with propagated shot-noise error.
- `internal/ui`: FITS image viewer with clickable star markers (target/comparison/check), static light-curve rendering via gonum/plot, and a red-on-black "Observer mode" theme.
- Main window wiring: open a FITS file, adjust background/range display levels, place star markers, and run photometry to produce a light-curve plot.
- CI: build, test, and SonarCloud analysis on push to `main` and on pull requests.
- Release CI: cross-platform builds (Linux/Windows/macOS via fyne-cross) and SLSA3 provenance generation on version tags.
