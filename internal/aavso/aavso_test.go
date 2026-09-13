package aavso

import (
	"bytes"
	"testing"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

func TestWriteExtendedFormat_exactOutput(t *testing.T) {
	report := Report{
		Points: []timeseries.Point{
			{JD: 2460000.12345, DiffMag: -0.1234, DiffMagErr: 0.0056},
			{JD: 2460000.23456, DiffMag: -0.1189, DiffMagErr: 0.0061},
		},
		Filter:       "V",
		StarName:     "VAR-1",
		ObserverCode: "ABC01",
		CompName:     "COMP-1",
		CompMag:      10.523,
		HasCompMag:   true,
		Chart:        "X29876ABC",
	}

	var buf bytes.Buffer
	if err := WriteExtendedFormat(&buf, report); err != nil {
		t.Fatalf("WriteExtendedFormat: %v", err)
	}

	want := "#TYPE=EXTENDED\n" +
		"#OBSCODE=ABC01\n" +
		"#SOFTWARE=Starflux\n" +
		"#DELIM=,\n" +
		"#DATE=JD\n" +
		"#OBSTYPE=CCD\n" +
		"VAR-1,2460000.12345,-0.1234,0.0056,V,NO,STD,COMP-1,10.523,NA,NA,NA,NA,X29876ABC,NA\n" +
		"VAR-1,2460000.23456,-0.1189,0.0061,V,NO,STD,COMP-1,10.523,NA,NA,NA,NA,X29876ABC,NA\n"

	if buf.String() != want {
		t.Errorf("WriteExtendedFormat() =\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteExtendedFormat_missingFilterUsesNA(t *testing.T) {
	report := Report{
		Points:       []timeseries.Point{{JD: 1, DiffMag: 0.5, DiffMagErr: 0.01}},
		StarName:     "VAR-1",
		ObserverCode: "ABC01",
		CompName:     "COMP-1",
	}

	var buf bytes.Buffer
	if err := WriteExtendedFormat(&buf, report); err != nil {
		t.Fatalf("WriteExtendedFormat: %v", err)
	}

	want := "#TYPE=EXTENDED\n" +
		"#OBSCODE=ABC01\n" +
		"#SOFTWARE=Starflux\n" +
		"#DELIM=,\n" +
		"#DATE=JD\n" +
		"#OBSTYPE=CCD\n" +
		"VAR-1,1.00000,0.5000,0.0100,NA,NO,STD,COMP-1,NA,NA,NA,NA,NA,NA,NA\n"

	if buf.String() != want {
		t.Errorf("WriteExtendedFormat() =\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteExtendedFormat_noCheckStarUsesNA(t *testing.T) {
	report := Report{
		Points:       []timeseries.Point{{JD: 1, DiffMag: 0.5, DiffMagErr: 0.01}},
		Filter:       "R",
		StarName:     "VAR-1",
		ObserverCode: "ABC01",
		CompName:     "COMP-1",
		CompMag:      9.1,
		HasCompMag:   true,
	}

	var buf bytes.Buffer
	if err := WriteExtendedFormat(&buf, report); err != nil {
		t.Fatalf("WriteExtendedFormat: %v", err)
	}

	if got := buf.String(); !bytes.Contains([]byte(got), []byte(",NA,NA,NA,NA,")) {
		t.Errorf("WriteExtendedFormat() = %q, want check-star columns (KNAME,KMAG) as NA,NA", got)
	}
}

func TestWriteExtendedFormat_emptyPointsWritesHeaderOnly(t *testing.T) {
	report := Report{StarName: "VAR-1", ObserverCode: "ABC01"}

	var buf bytes.Buffer
	if err := WriteExtendedFormat(&buf, report); err != nil {
		t.Fatalf("WriteExtendedFormat: %v", err)
	}

	want := "#TYPE=EXTENDED\n#OBSCODE=ABC01\n#SOFTWARE=Starflux\n#DELIM=,\n#DATE=JD\n#OBSTYPE=CCD\n"
	if buf.String() != want {
		t.Errorf("WriteExtendedFormat() =\n%s\nwant header-only:\n%s", buf.String(), want)
	}
}
