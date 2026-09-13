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

### Changed
- `ui.Marker` renamed to `ui.Star` (adds a `Name` field); `ImageView.AddMarker`/`ClearMarkers` replaced by `SetStars([]Star)`, which redraws the full marker overlay from a single list — fixes a bug where re-tapping the same role left a visually orphaned marker no longer tracked by the app.

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
