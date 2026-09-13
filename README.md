# Starflux

[![Build](https://github.com/fikua/fikua-starflux/actions/workflows/build.yml/badge.svg)](https://github.com/fikua/fikua-starflux/actions/workflows/build.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=coverage)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Bugs](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=bugs)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Duplicated Lines (%)](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=duplicated_lines_density)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Light curve photometry tool in Go — spiritual successor to [FotoDif](http://www.astrosurf.com/orodeno/fotodif/).

Native desktop app (Windows/Linux/macOS) for differential aperture photometry on FITS image series: exoplanet transits, variable stars, asteroid occultations, and other light-curve science.

## Download

Grab the [latest release](https://github.com/fikua/fikua-starflux/releases/latest):

| Platform | Download |
| --- | --- |
| 🍎 Desktop app for macOS (Apple Silicon) | `starflux-darwin-arm64.dmg` |
| 🍎 Desktop app for macOS (Intel) | `starflux-darwin-amd64.dmg` |
| 🪟 Desktop app for Windows | `starflux-windows-amd64-setup.exe` |
| 🐧 Desktop app for Linux (x86_64) | `starflux-linux-amd64.tar.gz` |
| 🐧 Desktop app for Linux (ARM64) | `starflux-linux-arm64.tar.gz` |

All [beta releases are available here](https://github.com/fikua/fikua-starflux/releases).

Binaries are not code-signed yet: macOS Gatekeeper and Windows SmartScreen will warn on first launch.
- **macOS**: right-click the app inside the mounted `.dmg` and choose "Open" instead of double-clicking (or run `xattr -cr Starflux.app` from Terminal).
- **Windows**: click "More info" → "Run anyway" on the SmartScreen prompt.

## Status

Early development. The core measurement pipeline works end to end (load FITS → mark stars → aperture photometry → differential light curve), but most of the surrounding workflow FotoDif provides is not built yet. Tracking against [FotoDif's feature set](docs/fotodif/README.md):

| Feature | Status | Priority |
| --- | --- | --- |
| Load FITS (single file / multiple files / folder) | ✅ Done | — |
| Aperture photometry (centroid, aperture, sky annulus) | ✅ Done | — |
| Differential magnitude light curve | ✅ Done | — |
| Star-picking magnifier loupe | ✅ Done | — |
| Observer (red-light) theme | ✅ Done | — |
| Named/multiple stars (beyond fixed Target/Comparison/Check) | ❌ Not started | High |
| Save/restore session state and star positions | ❌ Not started | High |
| Multi-session support ("primera serie" / resume after error) | ❌ Not started | High |
| Error tolerance control + resume-from-failure | ❌ Not started | High |
| Header metadata (MZERO/FILTER/max ADU) in star picker | ❌ Not started | High |
| Configuration (optics, photometry radii, observatory) UI | ❌ Not started | Medium |
| Airmass / transparency / FWHM / drift plots | ❌ Not started | Medium |
| Light-curve tilt correction | ❌ Not started | Medium |
| AAVSO / ALCDEF report export | ❌ Not started | Medium |
| Automatic star detection | ❌ Not started | Medium |
| Automatic (watch-folder) processing mode | ❌ Not started | Low |
| Variable-star search across a field | ❌ Not started | Low |
| Period analysis | ❌ Not started | Low |

**High**: needed for a basic real observing session end to end. **Medium**: makes results usable/shareable and the tool configurable. **Low**: advanced tools FotoDif offers on top of a working session — valuable, but not blocking day-to-day use.

See [CHANGELOG.md](CHANGELOG.md) for what shipped in each version.

## Architecture

**Desktop app, not a SaaS.** An observing session produces large volumes of FITS files (4-50 MB each, 5-20 GB per session). Accepting uploads at that volume from many users means storage, egress, and compute costs scaling linearly with usage, with no revenue model behind it — processing and storing on the user's own machine avoids that entirely. A possible future cloud "plus" would only ever share already-processed results (a light curve, an AAVSO/ALCDEF report), never the raw FITS files.

**Native GUI (Fyne), not a local web server.** Serving an HTML UI through an embedded local server was considered and dropped: it would depend on the user's default browser, need port management, and feel like a script opening a browser tab rather than an installed app. Fyne gives native OS file dialogs (via [zenity](https://github.com/ncruces/zenity)), true multi-window support, and packaging (`fyne package`) that produces a `.exe`/`.app`/`.AppImage` with its own icon.

**Modern look by default, plus a red-light Observer mode.** Fyne's default styling is modern (Material Design-like) rather than reproducing FotoDif's Windows Forms/Delphi look. Observer mode is a custom `fyne.Theme` (reds/oranges on black) meant to preserve night vision at the telescope, toggled live from the UI with no restart.

**Static, non-interactive plots.** FotoDif's plots (light curve, airmass/transparency/FWHM) are static, not interactive — this is intentional, not a limitation, and Starflux matches it: charts are rendered with [gonum/plot](https://pkg.go.dev/gonum.org/v1/plot) to an image and shown in a `canvas.Image`, avoiding a dependency on an interactive charting library (immature in the Fyne ecosystem).

**Code signing is deferred.** Windows SmartScreen warns but doesn't block unsigned binaries; macOS Gatekeeper is stricter and needs an Apple Developer Program membership (99 $/year) plus notarization. Signing is worth doing once the project has real traction, not before.

### Aperture photometry algorithm

Implemented in `internal/photometry/`, in three steps per selected star (target, comparison, check):

1. **Centroid** — refine an approximate pixel $(x_0, y_0)$ to a sub-pixel center of mass over a small box around it, using each pixel's ADU intensity (after subtracting a noise floor) as weight.
2. **Aperture sum** — sum all ADUs within radius $r_{ap}$ of the refined centroid; edge pixels are weighted by the fraction of their area inside the circle, for sub-pixel precision.
3. **Sky background subtraction** — take the median ADU (or sigma-clipped mean) of pixels in an annulus between $r_{in}$ and $r_{out}$ around the star, and subtract that background times the aperture's pixel count from the aperture sum to get the net flux.

### Cross-validation against FotoDif

Not yet done. To confirm Starflux's photometry matches FotoDif's to within rounding/median-calculation differences: process the same FITS series in both tools with identical aperture/annulus radii, compare their relative-magnitude ($\Delta m = m_{target} - m_{comp}$) output columns, and check the mean absolute residual stays under 0.002 mag.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines. Bug reports and feature requests go in [GitHub Issues](https://github.com/fikua/fikua-starflux/issues).

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
