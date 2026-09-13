// Package aavso writes light-curve results in the AAVSO Extended File
// Format (http://www.aavso.org/aavso-extended-file-format), the plain-text
// submission format for the American Association of Variable Star
// Observers. ALCDEF (the asteroid lightcurve format) is not implemented —
// no confirmed use case for it yet.
package aavso

import (
	"fmt"
	"io"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

// naPlaceholder is AAVSO's own documented convention for a field that is
// not available — used here for AIRMASS (Starflux deliberately doesn't
// compute it) and for check-star columns (KNAME/KMAG) when no Check-role
// star was measured for this report.
const naPlaceholder = "NA"

// Report holds everything needed to render one target star's light curve
// as an AAVSO Extended-format submission: fields Starflux already knows
// from measurement (Points, Filter), and fields the observer must supply
// manually (the rest).
type Report struct {
	// Known from measurement.
	Points []timeseries.Point // JD, DiffMag, DiffMagErr per observation
	Filter string             // e.g. "V", "R"; from fits.Image.Filter

	// User-entered: identity.
	StarName     string // AAVSO star designation or AUID
	ObserverCode string // AAVSO observer code

	// User-entered: comparison star.
	CompName   string
	CompMag    float64
	HasCompMag bool

	// User-entered: check star (optional — Starflux may not have measured one).
	CheckName   string
	CheckMag    float64
	HasCheckMag bool

	// User-entered: submission metadata.
	Chart string // chart ID/sequence used for comparison magnitudes
	Notes string // free-text notes, may be blank
}

// WriteExtendedFormat writes r as an AAVSO Extended File Format
// submission: one header block, then one CSV row per point.
func WriteExtendedFormat(w io.Writer, r Report) error {
	headers := []string{
		"#TYPE=EXTENDED\n",
		fmt.Sprintf("#OBSCODE=%s\n", r.ObserverCode),
		"#SOFTWARE=Starflux\n",
		"#DELIM=,\n",
		"#DATE=JD\n",
		"#OBSTYPE=CCD\n",
	}
	for _, h := range headers {
		if _, err := io.WriteString(w, h); err != nil {
			return fmt.Errorf("aavso: write header: %w", err)
		}
	}

	compMag := naPlaceholder
	if r.HasCompMag {
		compMag = fmt.Sprintf("%.3f", r.CompMag)
	}
	checkName, checkMag := naPlaceholder, naPlaceholder
	if r.CheckName != "" {
		checkName = r.CheckName
		if r.HasCheckMag {
			checkMag = fmt.Sprintf("%.3f", r.CheckMag)
		}
	}
	filter := r.Filter
	if filter == "" {
		filter = naPlaceholder
	}
	chart := chartOrNA(r.Chart)
	notes := notesOrNA(r.Notes)

	for _, p := range r.Points {
		_, err := fmt.Fprintf(w, "%s,%.5f,%.4f,%.4f,%s,NO,STD,%s,%s,%s,%s,NA,NA,%s,%s\n",
			r.StarName, p.JD, p.DiffMag, p.DiffMagErr, filter,
			r.CompName, compMag, checkName, checkMag,
			chart, notes,
		)
		if err != nil {
			return fmt.Errorf("aavso: write row: %w", err)
		}
	}
	return nil
}

func chartOrNA(s string) string {
	if s == "" {
		return naPlaceholder
	}
	return s
}

func notesOrNA(s string) string {
	if s == "" {
		return naPlaceholder
	}
	return s
}
