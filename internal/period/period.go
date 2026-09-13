// Package period finds periodic signals in an unevenly-sampled time series
// via the Lomb-Scargle periodogram — the standard astronomical method for
// this problem, chosen specifically because ordinary FFT-based methods
// assume perfectly regular time sampling, which real observing sessions
// never have (clouds, daylight gaps, multi-night baselines).
package period

import (
	"fmt"
	"math"
	"sort"

	"github.com/fikua/fikua-starflux/internal/timeseries"
)

// PeriodogramPoint is one trial period's computed Lomb-Scargle power.
type PeriodogramPoint struct {
	Period float64 // trial period, in the same time units as the input JDs (days)
	Power  float64 // normalized Lomb-Scargle power at this period; higher = better fit
}

// LombScargle computes the classical Lomb-Scargle normalized periodogram
// (Lomb 1976 / Scargle 1982) for points over a grid of numTrials trial
// periods between minPeriod and maxPeriod (inclusive).
//
// The trial grid is built evenly spaced in FREQUENCY (1/period), not in
// period, per standard practice: periodogram features (peak width, aliasing
// structure) are even in frequency space, so a linear period grid would
// under-sample short periods and over-sample long ones. Results are
// returned sorted by increasing Period for convenient plotting.
//
// Returns an error if fewer than 2 points are given, if minPeriod <= 0,
// maxPeriod <= minPeriod, or numTrials < 1.
func LombScargle(points []timeseries.Point, minPeriod, maxPeriod float64, numTrials int) ([]PeriodogramPoint, error) {
	if len(points) < 2 {
		return nil, fmt.Errorf("period: LombScargle: need at least 2 points, got %d", len(points))
	}
	if minPeriod <= 0 {
		return nil, fmt.Errorf("period: LombScargle: minPeriod must be > 0, got %v", minPeriod)
	}
	if maxPeriod <= minPeriod {
		return nil, fmt.Errorf("period: LombScargle: maxPeriod (%v) must be > minPeriod (%v)", maxPeriod, minPeriod)
	}
	if numTrials < 1 {
		return nil, fmt.Errorf("period: LombScargle: numTrials must be >= 1, got %d", numTrials)
	}

	mean := meanDiffMag(points)
	variance := varianceDiffMag(points, mean)

	minFreq := 1.0 / maxPeriod
	maxFreq := 1.0 / minPeriod

	out := make([]PeriodogramPoint, numTrials)
	for i := 0; i < numTrials; i++ {
		var freq float64
		if numTrials == 1 {
			freq = minFreq
		} else {
			t := float64(i) / float64(numTrials-1)
			freq = minFreq + t*(maxFreq-minFreq)
		}
		omega := 2 * math.Pi * freq
		out[i] = PeriodogramPoint{
			Period: 1.0 / freq,
			Power:  lombScarglePower(points, mean, variance, omega),
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Period < out[j].Period })
	return out, nil
}

// lombScarglePower computes the Lomb-Scargle power at one angular frequency
// omega = 2*pi*f, following the standard time-shifted (tau-offset)
// formulation that makes the sine and cosine coefficients statistically
// independent:
//
//	tau = (1/2omega) * atan2( sum(sin(2*omega*t)), sum(cos(2*omega*t)) )
//
//	P(omega) = (1/2) * [
//	    (sum((y-ybar)*cos(omega*(t-tau))))^2 / sum(cos(omega*(t-tau))^2)
//	  + (sum((y-ybar)*sin(omega*(t-tau))))^2 / sum(sin(omega*(t-tau))^2)
//	]
//
// normalized by the variance of y (giving Scargle's "normalized" power,
// where a value near 0 means no fit improvement and larger values mean a
// stronger periodic fit at that frequency).
func lombScarglePower(points []timeseries.Point, mean, variance, omega float64) float64 {
	var sumSin2wt, sumCos2wt float64
	for _, p := range points {
		sumSin2wt += math.Sin(2 * omega * p.JD)
		sumCos2wt += math.Cos(2 * omega * p.JD)
	}
	tau := math.Atan2(sumSin2wt, sumCos2wt) / (2 * omega)

	var sumYCos, sumYSin, sumCos2, sumSin2 float64
	for _, p := range points {
		wt := omega * (p.JD - tau)
		c, s := math.Cos(wt), math.Sin(wt)
		yCentered := p.DiffMag - mean
		sumYCos += yCentered * c
		sumYSin += yCentered * s
		sumCos2 += c * c
		sumSin2 += s * s
	}

	var termCos, termSin float64
	if sumCos2 > 0 {
		termCos = (sumYCos * sumYCos) / sumCos2
	}
	if sumSin2 > 0 {
		termSin = (sumYSin * sumYSin) / sumSin2
	}

	if variance <= 0 {
		return 0
	}
	return (termCos + termSin) / (2 * variance)
}

func meanDiffMag(points []timeseries.Point) float64 {
	var sum float64
	for _, p := range points {
		sum += p.DiffMag
	}
	return sum / float64(len(points))
}

func varianceDiffMag(points []timeseries.Point, mean float64) float64 {
	var sum float64
	for _, p := range points {
		d := p.DiffMag - mean
		sum += d * d
	}
	return sum / float64(len(points))
}

// BestPeriod returns the period and power of the strongest peak in a
// periodogram computed by LombScargle. Returns (0, 0) for an empty
// periodogram.
func BestPeriod(periodogram []PeriodogramPoint) (period, power float64) {
	var best PeriodogramPoint
	for _, pt := range periodogram {
		if pt.Power > best.Power {
			best = pt
		}
	}
	return best.Period, best.Power
}

// FoldPhase returns a copy of points with JD replaced by orbital phase in
// [0, 1) for the given period — phase 0 is the JD of the first point in the
// input (an arbitrary but stable epoch reference, since Starflux doesn't
// track a scientifically meaningful epoch of minimum). DiffMag and
// DiffMagErr are copied unchanged. The convention [0, 1) (rather than
// [-0.5, 0.5)) is chosen because it matches how "Marca periodo" in FotoDif
// and most amateur variable-star tools present a fold: phase 0 and phase 1
// are typically both drawn as reference lines bracketing one full cycle.
// Output is sorted by phase for a clean left-to-right fold plot; input is
// never modified.
//
// Returns an error if period <= 0 or points is empty.
func FoldPhase(points []timeseries.Point, period float64) ([]timeseries.Point, error) {
	if period <= 0 {
		return nil, fmt.Errorf("period: FoldPhase: period must be > 0, got %v", period)
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("period: FoldPhase: no points to fold")
	}

	epoch := points[0].JD
	out := make([]timeseries.Point, len(points))
	for i, p := range points {
		phase := math.Mod((p.JD-epoch)/period, 1.0)
		if phase < 0 {
			phase += 1.0
		}
		out[i] = timeseries.Point{JD: phase, DiffMag: p.DiffMag, DiffMagErr: p.DiffMagErr}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JD < out[j].JD })
	return out, nil
}
