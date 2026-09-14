package main

import (
	"math"
	"testing"

	"github.com/fikua/fikua-starflux/internal/config"
	"github.com/fikua/fikua-starflux/internal/fits"
	"github.com/fikua/fikua-starflux/internal/ui"
)

// newTestImage builds a synthetic flat-background *fits.Image, so tracking
// logic can be exercised without a real FITS file on disk.
func newTestImage(w, h int, background float64) *fits.Image {
	pixels := make([]float64, w*h)
	for i := range pixels {
		pixels[i] = background
	}
	return &fits.Image{W: w, H: h, Pixels: pixels}
}

// addStarSignal adds a small bright square (not a Gaussian — precision
// doesn't matter here, only "there is signal to centroid on") centered at
// (cx, cy) to img's pixel data.
func addStarSignal(img *fits.Image, cx, cy int, amplitude float64) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || x >= img.W || y < 0 || y >= img.H {
				continue
			}
			img.Pixels[y*img.W+x] += amplitude
		}
	}
}

// addGaussianStarSignal adds a symmetric Gaussian source (sub-pixel
// centroid precision, unlike addStarSignal's flat square) centered at
// (cx, cy) to img's pixel data — mirrors internal/photometry's own test
// helper of the same shape.
func addGaussianStarSignal(img *fits.Image, cx, cy, amplitude, sigma float64) {
	for y := 0; y < img.H; y++ {
		for x := 0; x < img.W; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			v := amplitude * math.Exp(-(dx*dx+dy*dy)/(2*sigma*sigma))
			img.Pixels[y*img.W+x] += v
		}
	}
}

// TestMeasureOneImage_targetSuccessCompFailureRecordsNoDrift is a regression
// test for a crash reported against the app: Process paniced with "index
// out of range" building the tracking-drift plot. The root cause was that
// starTracker.measureTarget appended to t.targetDrift as soon as the target
// star's own measurement succeeded, before comparison stars were measured.
// When a comparison star then failed for that same frame, the frame
// contributed no observation at all, but the drift entry had already been
// recorded — desyncing the drift slice from the observations slice it's
// meant to stay index-aligned with (see measureSeriesResult's doc comment),
// and eventually indexing past the end of the shorter slice.
//
// This test builds one frame where the target measures successfully but
// the sole comparison star has no signal nearby (forcing photometry.Measure
// to fail with "no signal above background"), and asserts measureOneImage
// fails the whole frame WITHOUT recording a drift entry for it.
func TestMeasureOneImage_targetSuccessCompFailureRecordsNoDrift(t *testing.T) {
	const w, h = 61, 61
	img := newTestImage(w, h, 100) // flat sky background
	addStarSignal(img, 30, 30, 5000)
	// Deliberately no signal added around the comparison star's position
	// (50, 50): its Measure call will fail with "no signal above
	// background", failing the whole frame after the target already
	// succeeded.

	target := ui.Star{Name: "VAR-1", Role: ui.RoleTarget, X: 30, Y: 30}
	comp := ui.Star{Name: "COMP-1", Role: ui.RoleComparison, X: 50, Y: 50}
	tracker := newStarTracker(target, []ui.Star{comp})

	_, err := measureOneImage(tracker, img, config.Default())
	if err == nil {
		t.Fatal("measureOneImage: expected an error (comparison star has no signal), got nil")
	}
	if len(tracker.targetDrift) != 0 {
		t.Errorf("targetDrift = %v after a frame that failed on the comparison star, want empty: a frame that contributes no observation must not contribute a drift entry either, or the drift slice desyncs from the observations it's plotted against", tracker.targetDrift)
	}
}

// TestMeasureOneImage_fullSuccessRecordsOneDriftEntry confirms the ordinary
// success path still records exactly one drift entry per successfully
// measured frame (the invariant TestMeasureOneImage_targetSuccessCompFailureRecordsNoDrift
// depends on not being trivially satisfied by "never record anything").
func TestMeasureOneImage_fullSuccessRecordsOneDriftEntry(t *testing.T) {
	const w, h = 61, 61
	img := newTestImage(w, h, 100)
	addStarSignal(img, 30, 30, 5000)
	addStarSignal(img, 50, 50, 5000)

	target := ui.Star{Name: "VAR-1", Role: ui.RoleTarget, X: 30, Y: 30}
	comp := ui.Star{Name: "COMP-1", Role: ui.RoleComparison, X: 50, Y: 50}
	tracker := newStarTracker(target, []ui.Star{comp})

	if _, err := measureOneImage(tracker, img, config.Default()); err != nil {
		t.Fatalf("measureOneImage: %v", err)
	}
	if len(tracker.targetDrift) != 1 {
		t.Errorf("targetDrift = %v after one fully successful frame, want exactly 1 entry", tracker.targetDrift)
	}
}

// TestMeasureFrame_wrongNeighborLockCaughtByPatternCheck is a regression
// test for the real-world bug reported against the app: a series where
// per-star tolerance checking alone repeatedly accepted a comparison star
// that had locked onto a different, real, nearby star instead of the one
// actually marked — because a wrong lock still produces a small,
// "normal-looking" frame-to-frame displacement that passes an individual
// tolerance check. Target and COMP-1 both drift by a small, mutually
// consistent vector (simulating ordinary mount drift); COMP-2's own
// initial (naive) centroid is pulled toward a brighter, wrong neighbor
// star sitting nearby, producing a displacement that individually passes
// tolerance but disagrees with the group's consensus — while COMP-2's
// real, correct star (fainter, but still real) sits exactly where the
// consensus drift predicts. This is what the group-pattern check must
// resolve: it should notice COMP-2's disagreement with the consensus and
// re-search near the consensus-predicted position via reacquireStar
// (which considers ALL real candidates in the region and picks the one
// closest to where the star should be, rather than whatever a naive
// Measure centroid happened to lock onto), landing on the correct star
// instead of silently keeping the wrong one.
func TestMeasureFrame_wrongNeighborLockCaughtByPatternCheck(t *testing.T) {
	const w, h = 121, 121

	// Frame 1: seed the tracker at the marked positions, each with a real
	// star exactly there (zero displacement, establishing the baseline).
	frame1 := newTestImage(w, h, 100)
	addGaussianStarSignal(frame1, 30, 30, 5000, 3) // target
	addGaussianStarSignal(frame1, 50, 50, 5000, 3) // comp1
	addGaussianStarSignal(frame1, 30, 80, 5000, 3) // comp2

	target := ui.Star{Name: "VAR-1", Role: ui.RoleTarget, X: 30, Y: 30}
	comp1 := ui.Star{Name: "COMP-1", Role: ui.RoleComparison, X: 50, Y: 50}
	comp2 := ui.Star{Name: "COMP-2", Role: ui.RoleComparison, X: 30, Y: 80}
	tracker := newStarTracker(target, []ui.Star{comp1, comp2})

	if _, err := measureOneImage(tracker, frame1, config.Default()); err != nil {
		t.Fatalf("frame 1 (baseline): measureOneImage: %v", err)
	}

	// Frame 2: target and comp1 both drift by a small, mutually
	// consistent vector (~(0.3, 0.2)px) — ordinary mount drift. BOTH a
	// wrong (brighter) neighbor and comp2's real (fainter) star are
	// present near comp2's last position; comp2's own naive centroid
	// gets pulled toward the brighter wrong one, producing a
	// displacement that passes its own tolerance individually but
	// disagrees with what target/comp1 agreed on.
	frame2 := newTestImage(w, h, 100)
	addGaussianStarSignal(frame2, 30.3, 30.2, 5000, 3) // target, consistent drift
	addGaussianStarSignal(frame2, 50.3, 50.2, 5000, 3) // comp1, consistent drift
	addGaussianStarSignal(frame2, 36.0, 75.0, 8000, 2) // wrong neighbor near comp2, brighter
	addGaussianStarSignal(frame2, 30.3, 80.2, 4000, 2) // comp2's real star, following the consensus drift

	_, err := measureOneImage(tracker, frame2, config.Default())
	if err != nil {
		t.Fatalf("measureOneImage: %v (want success, reacquired onto the correct, consensus-following star)", err)
	}
	gotX, gotY := tracker.compX[1], tracker.compY[1]
	if math.Hypot(gotX-30.3, gotY-80.2) > 1 {
		t.Errorf("COMP-2 tracked to (%.2f, %.2f), want close to (30.3, 80.2) (the star that follows the group's consensus drift) — got the wrong neighbor instead", gotX, gotY)
	}
}

// TestMeasureFrame_uniformFieldShiftRecovered is a regression test for a
// real-world reported bug: a series where the observer recentered the
// telescope between two exposures, shifting every star by the same large
// vector (tens of pixels) — well outside cfg.CentroidHalfWidth's ordinary
// per-frame search box. Before detectFieldShift existed, every tracked
// star's first-pass Measure centroided on background noise near its now-
// stale position instead of finding the real (shifted) star, producing
// inconsistent per-star displacements that failed both individual
// tolerance and the group-pattern consensus check, failing the whole
// frame with "wrong-star lock suspected" even though nothing was
// actually wrong — the whole field had simply moved together.
//
// This builds a field with the three tracked stars (target + 2 comps)
// plus a handful of unrelated background stars (so detectFieldShift's
// wide-area DetectStars scan has more than 3 points to correlate against,
// closer to a real crowded field), then shifts EVERY star by the same
// (40, -25)px vector in frame 2 — larger than cfg.CentroidHalfWidth (10px
// by default) and than toleranceLevelToPixels' max jump, so the first
// pass cannot find any of them directly.
func TestMeasureFrame_uniformFieldShiftRecovered(t *testing.T) {
	const w, h = 300, 300
	const shiftDX, shiftDY = 40.0, -25.0

	type star struct{ x, y, amp float64 }
	tracked := []star{
		{60, 200, 5000},  // target
		{150, 220, 4500}, // comp1
		{220, 180, 4800}, // comp2
	}
	background := []star{
		{20, 20, 3000}, {280, 30, 3200}, {100, 260, 2900}, {250, 250, 3100},
		{40, 150, 2800}, {180, 40, 3300}, {260, 120, 3000}, {90, 90, 2700},
	}

	frame1 := newTestImage(w, h, 100)
	for _, s := range append(append([]star{}, tracked...), background...) {
		addGaussianStarSignal(frame1, s.x, s.y, s.amp, 3)
	}

	target := ui.Star{Name: "VAR-1", Role: ui.RoleTarget, X: tracked[0].x, Y: tracked[0].y}
	comp1 := ui.Star{Name: "COMP-1", Role: ui.RoleComparison, X: tracked[1].x, Y: tracked[1].y}
	comp2 := ui.Star{Name: "COMP-2", Role: ui.RoleComparison, X: tracked[2].x, Y: tracked[2].y}
	tracker := newStarTracker(target, []ui.Star{comp1, comp2})

	if _, err := measureOneImage(tracker, frame1, config.Default()); err != nil {
		t.Fatalf("frame 1 (baseline): measureOneImage: %v", err)
	}

	frame2 := newTestImage(w, h, 100)
	for _, s := range append(append([]star{}, tracked...), background...) {
		addGaussianStarSignal(frame2, s.x+shiftDX, s.y+shiftDY, s.amp, 3)
	}

	obs, err := measureOneImage(tracker, frame2, config.Default())
	if err != nil {
		t.Fatalf("measureOneImage after uniform field shift: %v (want success via detectFieldShift)", err)
	}

	gotX, gotY := tracker.targetX, tracker.targetY
	wantX, wantY := tracked[0].x+shiftDX, tracked[0].y+shiftDY
	if math.Hypot(gotX-wantX, gotY-wantY) > 1 {
		t.Errorf("target tracked to (%.2f, %.2f), want close to (%.2f, %.2f) (the shifted position)", gotX, gotY, wantX, wantY)
	}
	if obs.Target.NetFlux <= 0 || obs.Comp.NetFlux <= 0 {
		t.Errorf("obs = %+v, want positive flux for both target and combined comparison", obs)
	}
}
