# Test fixtures

Reference data from [FotoDif](../docs/fotodif/README.md), used to validate Starflux's photometry against a known-good implementation and to onboard new contributors.

Files under `fits/` are tracked with [Git LFS](https://git-lfs.com) — run `git lfs install` once, then clone/pull normally.

- `fits/` — 109 FITS frames (`v_000.fit`–`v_108.fit`) from a real differential photometry session, the same series FotoDif's own manual uses as its worked example.
- `period-analysis/` — magnitude/Julian-date series (`i1.txt`–`i6.txt`) for validating period-analysis calculations.
- `tilt-correction/` — a light curve with atmospheric-extinction tilt, for validating tilt-correction calculations.
