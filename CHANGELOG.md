# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once a first tagged release is published.

## [Unreleased]

### Added
- Project bootstrap: Go module, Fyne-based desktop app skeleton, Apache 2.0 license.
- `internal/fits`: load FITS images and extract header metadata (INSTRUME, EXPTIME, DATE-OBS).
- `internal/photometry`: centroid refinement, aperture flux summation with subpixel edge coverage, sky background via annulus median.
- `internal/timeseries`: differential magnitude (Δm) computation with propagated shot-noise error.
- `internal/ui`: FITS image viewer with clickable star markers (target/comparison/check), static light-curve rendering via gonum/plot, and a red-on-black "Observer mode" theme.
- Main window wiring: open a FITS file, adjust background/range display levels, place star markers, and run photometry to produce a light-curve plot.
- CI: build, test, and SonarCloud analysis on push to `main` and on pull requests.
- Release CI: cross-platform builds (Linux/Windows/macOS via fyne-cross) and SLSA3 provenance generation on version tags.
