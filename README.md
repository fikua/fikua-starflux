# Starflux

[![Build](https://github.com/fikua/fikua-starflux/actions/workflows/build.yml/badge.svg)](https://github.com/fikua/fikua-starflux/actions/workflows/build.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=coverage)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=fikua_fikua-starflux&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=fikua_fikua-starflux)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Light curve photometry tool in Go — spiritual successor to [FotoDif](http://www.astrosurf.com/orodeno/fotodif/).

Native desktop app (Windows/Linux/macOS) for differential aperture photometry on FITS image series: exoplanet transits, variable stars, asteroid occultations, and other light-curve science.

See [docs/roadmap.md](docs/roadmap.md) for architecture and design decisions.

## Download

Grab the [latest release](https://github.com/fikua/fikua-starflux/releases/latest):

| Platform | Download |
|---|---|
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

| Feature | Status |
|---|---|
| Load FITS (single file / multiple files / folder) | ✅ Done |
| Aperture photometry (centroid, aperture, sky annulus) | ✅ Done |
| Differential magnitude light curve | ✅ Done |
| Star-picking magnifier loupe | ✅ Done |
| Observer (red-light) theme | ✅ Done |
| Named/multiple stars (beyond fixed Target/Comparison/Check) | ❌ Not started |
| Save/restore session state and star positions | ❌ Not started |
| Multi-session support ("primera serie" / resume after error) | ❌ Not started |
| Error tolerance control + resume-from-failure | ❌ Not started |
| Header metadata (MZERO/FILTER/max ADU) in star picker | ❌ Not started |
| Configuration (optics, photometry radii, observatory) UI | ❌ Not started |
| Airmass / transparency / FWHM / drift plots | ❌ Not started |
| Light-curve tilt correction | ❌ Not started |
| AAVSO / ALCDEF report export | ❌ Not started |
| Automatic star detection | ❌ Not started |
| Variable-star search across a field | ❌ Not started |
| Period analysis | ❌ Not started |
| Automatic (watch-folder) processing mode | ❌ Not started |

See [CHANGELOG.md](CHANGELOG.md) for what shipped in each version.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines. Bug reports and feature requests go in [GitHub Issues](https://github.com/fikua/fikua-starflux/issues).

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
