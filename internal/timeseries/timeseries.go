// Package timeseries turns per-image photometry measurements into a
// differential magnitude light curve: Δm = m_target - m_comp over time.
package timeseries

import (
	"fmt"
	"math"
	"sort"

	"github.com/fikua/fikua-starflux/internal/photometry"
)

// Point is one differential-magnitude measurement in a light curve.
type Point struct {
	JD         float64 // Julian Date of the observation
	DiffMag    float64 // m_target - m_comp
	DiffMagErr float64 // propagated 1-sigma uncertainty on DiffMag
}

// Observation is a single image's photometry inputs for one epoch: the
// measured target and comparison stars, plus the exposure time and JD
// needed to place the point in the series.
type Observation struct {
	JD     float64
	Target photometry.Result
	Comp   photometry.Result
	// GainEPerADU converts ADU to electrons for shot-noise error
	// estimation. A value <= 0 disables error propagation (DiffMagErr
	// will be reported as 0).
	GainEPerADU float64
}

// Magnitude converts a net flux (in ADU) to an instrumental magnitude.
// Returns +Inf for non-positive flux (no signal / below background).
func Magnitude(netFlux float64) float64 {
	if netFlux <= 0 {
		return math.Inf(1)
	}
	return -2.5 * math.Log10(netFlux)
}

// DifferentialMagnitude computes Δm = m_target - m_comp for a single
// observation, along with its propagated uncertainty when gain is
// available. Returns an error if either star's net flux is non-positive.
func DifferentialMagnitude(obs Observation) (Point, error) {
	if obs.Target.NetFlux <= 0 {
		return Point{}, fmt.Errorf("timeseries: target has non-positive net flux %.3f at JD %.5f", obs.Target.NetFlux, obs.JD)
	}
	if obs.Comp.NetFlux <= 0 {
		return Point{}, fmt.Errorf("timeseries: comparison has non-positive net flux %.3f at JD %.5f", obs.Comp.NetFlux, obs.JD)
	}

	mTarget := Magnitude(obs.Target.NetFlux)
	mComp := Magnitude(obs.Comp.NetFlux)

	var magErr float64
	if obs.GainEPerADU > 0 {
		targetErr := propagatedError(obs.Target, obs.GainEPerADU)
		compErr := propagatedError(obs.Comp, obs.GainEPerADU)
		magErr = math.Sqrt(targetErr*targetErr + compErr*compErr)
	}

	return Point{
		JD:         obs.JD,
		DiffMag:    mTarget - mComp,
		DiffMagErr: magErr,
	}, nil
}

// propagatedError estimates the 1-sigma magnitude error for one star's
// measurement from Poisson shot noise on the source and sky background,
// converted to electrons via gain.
func propagatedError(res photometry.Result, gain float64) float64 {
	sourceE := res.NetFlux * gain
	skyE := res.ApPixels * res.SkyPerPx * gain
	if sourceE <= 0 {
		return 0
	}
	totalE := math.Max(sourceE+skyE, 0)
	snr := sourceE / math.Sqrt(totalE)
	if snr <= 0 {
		return 0
	}
	// d(mag)/d(flux) = -2.5/(ln(10)*flux); relative flux error is 1/snr.
	return 2.5 / math.Ln10 / snr
}

// MergeSorted concatenates prev and next and returns the result sorted by
// JD ascending, so a resumed series' new points combine correctly with
// points already accumulated from an earlier, interrupted run regardless
// of reload order.
func MergeSorted(prev, next []Point) []Point {
	merged := make([]Point, 0, len(prev)+len(next))
	merged = append(merged, prev...)
	merged = append(merged, next...)
	sort.Slice(merged, func(i, j int) bool { return merged[i].JD < merged[j].JD })
	return merged
}

// Series builds a full light curve from a set of observations, skipping
// (and reporting) any epoch where the differential magnitude cannot be
// computed. The returned points are in the same order as obs.
func Series(obs []Observation) (points []Point, errs []error) {
	for _, o := range obs {
		p, err := DifferentialMagnitude(o)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		points = append(points, p)
	}
	return points, errs
}

// SkippedObservation pairs one Series-skipped epoch with the Observation it
// came from, so a caller can attribute the failure back to its source
// (e.g. Starflux's per-image status UI) without parsing error text.
type SkippedObservation struct {
	Obs Observation
	Err error
}

// SeriesWithObservations is Series, but returns each skipped epoch's
// source Observation alongside its error instead of a bare error. Series
// itself is unchanged and implemented independently; this exists purely so
// callers that need to correlate a skip back to its Observation don't have
// to re-run DifferentialMagnitude themselves.
func SeriesWithObservations(obs []Observation) (points []Point, skipped []SkippedObservation) {
	for _, o := range obs {
		p, err := DifferentialMagnitude(o)
		if err != nil {
			skipped = append(skipped, SkippedObservation{Obs: o, Err: err})
			continue
		}
		points = append(points, p)
	}
	return points, skipped
}

// FitBaseline fits a straight line (DiffMag = slope*JD + intercept) by
// ordinary least squares, using only the points whose JD falls within any
// of the given [start, end] ranges — the "flat" baseline zone(s) the user
// has identified (e.g. out-of-transit data, or two ranges for in/out of
// transit), matching FotoDif's tilt-correction workflow of marking 2 or 4
// reference points. Returns an error if fewer than 2 points fall within
// any range, or if all of them share the same JD (a degenerate fit).
func FitBaseline(points []Point, ranges [][2]float64) (slope, intercept float64, err error) {
	var n int
	var sumX, sumY, sumXX, sumXY float64
	for _, p := range points {
		if !inAnyRange(p.JD, ranges) {
			continue
		}
		n++
		sumX += p.JD
		sumY += p.DiffMag
		sumXX += p.JD * p.JD
		sumXY += p.JD * p.DiffMag
	}
	if n < 2 {
		return 0, 0, fmt.Errorf("timeseries: FitBaseline: need at least 2 points within the given range(s), found %d", n)
	}
	nf := float64(n)
	denom := nf*sumXX - sumX*sumX
	if denom == 0 {
		return 0, 0, fmt.Errorf("timeseries: FitBaseline: degenerate fit (all points share the same JD)")
	}
	slope = (nf*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / nf
	return slope, intercept, nil
}

func inAnyRange(jd float64, ranges [][2]float64) bool {
	for _, r := range ranges {
		lo, hi := r[0], r[1]
		if lo > hi {
			lo, hi = hi, lo
		}
		if jd >= lo && jd <= hi {
			return true
		}
	}
	return false
}

// SubtractTilt returns a new slice of points with DiffMag adjusted by
// subtracting the fitted baseline line (slope*JD + intercept) from every
// point, leaving the input slice unmodified. This is a presentational
// correction only, matching FotoDif's documented behavior ("una
// interpretación de las medidas, no un cambio real") — callers must not
// persist the result over the original accumulated points.
func SubtractTilt(points []Point, slope, intercept float64) []Point {
	out := make([]Point, len(points))
	for i, p := range points {
		out[i] = p
		out[i].DiffMag -= slope*p.JD + intercept
	}
	return out
}

// Dispersion returns the sample standard deviation of DiffMag across
// points — a simple measure of how much a light curve "wobbles" overall.
// Returns 0 for fewer than 2 points (no meaningful spread to compute).
func Dispersion(points []Point) float64 {
	if len(points) < 2 {
		return 0
	}
	var sum float64
	for _, p := range points {
		sum += p.DiffMag
	}
	mean := sum / float64(len(points))

	var sumSq float64
	for _, p := range points {
		d := p.DiffMag - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(points)-1)) // sample variance (n-1), unbiased estimator
}

// MedianErr returns the median of DiffMagErr across points, used as a
// stand-in for a star's "typical expected noise" when flagging variability
// candidates. Returns 0 for an empty slice.
func MedianErr(points []Point) float64 {
	if len(points) == 0 {
		return 0
	}
	errs := make([]float64, len(points))
	for i, p := range points {
		errs[i] = p.DiffMagErr
	}
	sort.Float64s(errs)
	n := len(errs)
	if n%2 == 1 {
		return errs[n/2]
	}
	return (errs[n/2-1] + errs[n/2]) / 2
}

// VariabilityRatio returns Dispersion(points) / MedianErr(points): how many
// "typical measurement errors wide" the light curve's actual scatter is. A
// perfectly constant star measured with realistic noise should have a
// ratio near 1 (the scatter IS the noise); a ratio well above 1 means the
// star is varying by more than measurement noise alone can explain.
// Returns 0 if MedianErr is 0 (no usable error estimate — e.g. gain wasn't
// configured) rather than dividing by zero, so callers should treat a
// returned 0 as "unknown", not "definitely not variable".
func VariabilityRatio(points []Point) float64 {
	medErr := MedianErr(points)
	if medErr <= 0 {
		return 0
	}
	return Dispersion(points) / medErr
}

// IsPossibleVariable reports whether a star's light curve scatter exceeds
// threshold times its own typical measurement error — a deliberately
// generous, high-recall filter (matching FotoDif's documented design goal:
// "bastante generoso... es posible que genere algunas detecciones falsas"),
// not a rigorous statistical test. threshold values around 3 are a
// reasonable starting point (see VariableThresholdDefault).
func IsPossibleVariable(points []Point, threshold float64) bool {
	return VariabilityRatio(points) > threshold
}

// VariableThresholdDefault is the suggested default multiplier for
// IsPossibleVariable: a light curve whose scatter is more than 3x its own
// median measurement error is flagged. This is intentionally loose (a
// truly constant star's scatter should sit close to 1x its error under
// pure Gaussian noise) so as not to miss real but marginal variables,
// mirroring FotoDif's stated preference for false positives over missed
// detections.
const VariableThresholdDefault = 3.0
