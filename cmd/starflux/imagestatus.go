package main

import "github.com/fikua/fikua-starflux/internal/ui"

// imageResultKind classifies a single image's outcome from the most recent
// Process run, for the "Series inspector" dialog.
type imageResultKind int

const (
	statusPending  imageResultKind = iota // never processed since being loaded
	statusOK                              // measured successfully and contributed a light-curve point
	statusExcluded                        // user marked this image excluded; Process did not attempt it
	statusError                           // Process attempted this image and it failed
)

func (k imageResultKind) String() string {
	switch k {
	case statusOK:
		return "OK"
	case statusExcluded:
		return "Excluded"
	case statusError:
		return "Error"
	default:
		return "Not yet processed"
	}
}

// imageStatus is one image's latest recorded outcome, keyed by img.Path on
// appState.imageStatus. It is overwritten every time processAction runs,
// so it always reflects the most recent Process attempt, never a stale one
// from several runs ago. With multiple Target stars, only one outcome per
// image is kept (the last target processed wins) — a deliberate
// simplification; each target's own "N epoch(s) skipped" summary still
// names that target after its own light curve, as before.
type imageStatus struct {
	kind    imageResultKind
	target  string // target star name this status came from; "" if kind is statusPending/statusExcluded
	message string // human-readable failure detail; "" unless kind == statusError

	// recoveredAfterGap is >0 when this frame's tracking succeeded via the
	// fallback re-acquisition path (see reacquireStar) after this many
	// consecutive skipped (excluded or errored) frames — surfaced in the
	// Series inspector so the user can see this OK frame followed a gap.
	// 0 for an ordinary first-attempt success.
	recoveredAfterGap int

	// trackedStars is a snapshot of every tracked star's position (target
	// and comparisons) as starTracker held them immediately after
	// attempting this image — whether the attempt succeeded or failed. On
	// failure, this shows exactly where the tracker was searching from,
	// which is itself diagnostic. nil for statusPending/statusExcluded (no
	// measurement was attempted, so there's nothing tracked to show).
	trackedStars []ui.Star
}
