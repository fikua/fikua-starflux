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
