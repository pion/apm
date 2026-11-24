// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"container/heap"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getGainFirstDerivativeLinear mirrors GetGainFirstDerivativeLinear.
func (l *limiterDBGainCurve) getGainFirstDerivativeLinear(x float64) float64 {
	return l.gainCurveLimiterD1 * math.Pow(x/float64(kMaxAbsFloatS16Value), l.gainCurveLimiterD2)
}

// getGainIntegralLinear mirrors GetGainIntegralLinear.
func (l *limiterDBGainCurve) getGainIntegralLinear(x0, x1 float64) float64 {
	integral := func(x float64) float64 {
		return l.gainCurveLimiterI2 * math.Pow(x, l.gainCurveLimiterI1)
	}

	return integral(x1) - integral(x0)
}

// linSpace mirrors LinSpace: numPoints evenly spaced numbers over [l, r].
func linSpace(l, r float64, numPoints int) []float64 {
	points := make([]float64, numPoints)
	step := (r - l) / (float64(numPoints) - 1.0)
	points[0] = l
	for i := 1; i < numPoints-1; i++ {
		points[i] = l + float64(i)*step
	}
	points[numPoints-1] = r

	return points
}

// interpolatedParameters mirrors test::InterpolatedParameters.
type interpolatedParameters struct {
	x, m, q [kInterpolatedGainCurveTotalPoints]float32
}

// computeLinearApproximationParams mirrors ComputeLinearApproximationParams.
func computeLinearApproximationParams(l *limiterDBGainCurve, x float64) (m, q float64) {
	m = l.getGainFirstDerivativeLinear(x)
	q = l.getGainLinear(x) - m*x

	return m, q
}

// computeAreaUnderPiecewiseLinearApproximation mirrors
// ComputeAreaUnderPiecewiseLinearApproximation.
func computeAreaUnderPiecewiseLinearApproximation(l *limiterDBGainCurve, x0, x1 float64) float64 {
	m0, q0 := computeLinearApproximationParams(l, x0)
	m1, q1 := computeLinearApproximationParams(l, x1)
	xSplit := (q0 - q1) / (m1 - m0)
	areaUnderLinearPiece := func(xL, xR, m, q float64) float64 {
		return xR*(m*xR/2.0+q) - xL*(m*xL/2.0+q)
	}

	return areaUnderLinearPiece(x0, xSplit, m0, q0) + areaUnderLinearPiece(xSplit, x1, m1, q1)
}

// limiterUnderApproximationNegativeError mirrors
// LimiterUnderApproximationNegativeError.
func limiterUnderApproximationNegativeError(l *limiterDBGainCurve, x0, x1 float64) float64 {
	return l.getGainIntegralLinear(x0, x1) - computeAreaUnderPiecewiseLinearApproximation(l, x0, x1)
}

type limiterInterval struct{ x0, x1, err float64 }

// limiterIntervals is the max-heap on the error of SampleLimiterRegion.
type limiterIntervals []limiterInterval

func (h limiterIntervals) Len() int           { return len(h) }
func (h limiterIntervals) Less(i, j int) bool { return h[i].err > h[j].err }
func (h limiterIntervals) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *limiterIntervals) Push(x any)        { *h = append(*h, x.(limiterInterval)) } //nolint:forcetypeassert

func (h *limiterIntervals) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]

	return last
}

// sampleLimiterRegion mirrors SampleLimiterRegion: it splits the limiter
// region where the linear approximation is the worst.
func sampleLimiterRegion(l *limiterDBGainCurve) []float64 {
	newInterval := func(x0, x1 float64) limiterInterval {
		return limiterInterval{x0, x1, limiterUnderApproximationNegativeError(l, x0, x1)}
	}
	intervals := &limiterIntervals{newInterval(l.limiterStartLinear, l.maxInputLevelLinear)}
	for intervals.Len() < kInterpolatedGainCurveBeyondKneePoints {
		interval := heap.Pop(intervals).(limiterInterval) //nolint:forcetypeassert
		xSplit := (interval.x0 + interval.x1) / 2.0
		heap.Push(intervals, newInterval(interval.x0, xSplit))
		heap.Push(intervals, newInterval(xSplit, interval.x1))
	}

	samples := make([]float64, 0, kInterpolatedGainCurveBeyondKneePoints)
	for intervals.Len() > 0 {
		samples = append(samples, heap.Pop(intervals).(limiterInterval).x1) //nolint:forcetypeassert
	}
	slices.Sort(samples)

	return samples
}

// computeInterpolatedGainCurveApproximationParams mirrors
// ComputeInterpolatedGainCurveApproximationParams, which computes the
// tables of the interpolated gain curve. Like upstream, the parameters
// are stored as float.
func computeInterpolatedGainCurveApproximationParams() *interpolatedParameters {
	curve := newLimiterDBGainCurve()
	params := &interpolatedParameters{}

	// PrecomputeKneeApproxParams.
	points := linSpace(curve.kneeStartLinear, curve.limiterStartLinear, kInterpolatedGainCurveKneePoints-1)
	params.x[0] = float32(points[0])
	params.x[1] = float32((points[0] + points[1]) / 2.0)
	for i, point := range points[1:] {
		params.x[i+2] = float32(point)
	}
	for i := range kInterpolatedGainCurveKneePoints - 1 {
		x0 := float64(params.x[i])
		x1 := float64(params.x[i+1])
		y0 := curve.getGainLinear(x0)
		y1 := curve.getGainLinear(x1)
		params.m[i] = float32((y1 - y0) / (x1 - x0))
		params.q[i] = float32(y0 - float64(params.m[i])*x0)
	}

	// PrecomputeBeyondKneeApproxParams.
	m, q := computeLinearApproximationParams(curve, float64(params.x[kInterpolatedGainCurveKneePoints-1]))
	params.m[kInterpolatedGainCurveKneePoints-1] = float32(m)
	params.q[kInterpolatedGainCurveKneePoints-1] = float32(q)
	for i, sample := range sampleLimiterRegion(curve) {
		m, q = computeLinearApproximationParams(curve, sample)
		params.m[i+kInterpolatedGainCurveKneePoints] = float32(m)
		params.q[i+kInterpolatedGainCurveKneePoints] = float32(q)
	}
	for i := kInterpolatedGainCurveKneePoints; i < kInterpolatedGainCurveTotalPoints; i++ {
		params.x[i] = (params.q[i-1] - params.q[i]) / (params.m[i] - params.m[i-1])
	}

	return params
}

// gainCurveRegionStats mirrors the region counters of
// InterpolatedGainCurve::Stats. Upstream only reports them as histograms,
// which are not ported, so the tests classify the levels like UpdateStats.
type gainCurveRegionStats struct {
	identity, knee, limiter, saturation int
}

func (s *gainCurveRegionStats) lookUpGainToApply(inputLevel float32) float32 {
	switch {
	case inputLevel < approximationParamsX[0]:
		s.identity++
	case inputLevel < approximationParamsX[kInterpolatedGainCurveKneePoints-1]:
		s.knee++
	case inputLevel < kMaxInputLevelLinear:
		s.limiter++
	default:
		s.saturation++
	}

	return lookUpGainToApply(inputLevel)
}

const (
	levelEpsilon                   = 1e-2 * float64(kMaxAbsFloatS16Value)
	interpolatedGainCurveTolerance = 1.0 / 32768.0
	noOverApproximationTolerance   = 1e-7
)

// Port of CreateUse.
func TestInterpolatedGainCurve_CreateUse(t *testing.T) {
	l := newLimiterDBGainCurve()
	for _, level := range linSpace(levelEpsilon, float64(dbfsToFloatS16(float32(l.maxInputLevelDB+1))), 500) {
		assert.GreaterOrEqual(t, lookUpGainToApply(float32(level)), float32(0))
	}
}

// Port of CheckValidOutput.
func TestInterpolatedGainCurve_CheckValidOutput(t *testing.T) {
	l := newLimiterDBGainCurve()
	for _, level := range linSpace(levelEpsilon, l.maxInputLevelLinear*2.0, 500) {
		gain := lookUpGainToApply(float32(level))
		assert.LessOrEqual(t, float32(0), gain, "level %v", level)
		assert.LessOrEqual(t, gain, float32(1), "level %v", level)
	}
}

// Port of CheckMonotonicity.
func TestInterpolatedGainCurve_CheckMonotonicity(t *testing.T) {
	l := newLimiterDBGainCurve()
	prevGain := lookUpGainToApply(0)
	for _, level := range linSpace(levelEpsilon, l.maxInputLevelLinear+levelEpsilon+0.5, 500) {
		gain := lookUpGainToApply(float32(level))
		assert.GreaterOrEqual(t, prevGain, gain, "level %v", level)
		prevGain = gain
	}
}

// Port of CheckApproximation.
func TestInterpolatedGainCurve_CheckApproximation(t *testing.T) {
	l := newLimiterDBGainCurve()
	for _, level := range linSpace(levelEpsilon, l.maxInputLevelLinear-levelEpsilon, 500) {
		gain := float64(lookUpGainToApply(float32(level)))
		assert.Less(t, math.Abs(l.getGainLinear(level)-gain), interpolatedGainCurveTolerance, "level %v", level)
	}
}

// Port of CheckRegionBoundaries.
func TestInterpolatedGainCurve_CheckRegionBoundaries(t *testing.T) {
	l := newLimiterDBGainCurve()
	var stats gainCurveRegionStats
	for _, level := range []float64{
		levelEpsilon,
		l.kneeStartLinear + levelEpsilon,
		l.limiterStartLinear + levelEpsilon,
		l.maxInputLevelLinear + levelEpsilon,
	} {
		stats.lookUpGainToApply(float32(level))
	}
	assert.Equal(t, gainCurveRegionStats{identity: 1, knee: 1, limiter: 1, saturation: 1}, stats)
}

// Port of CheckIdentityRegion.
func TestInterpolatedGainCurve_CheckIdentityRegion(t *testing.T) {
	const numSteps = 10
	l := newLimiterDBGainCurve()
	var stats gainCurveRegionStats
	for _, level := range linSpace(levelEpsilon, l.kneeStartLinear, numSteps) {
		assert.Equal(t, float32(1), stats.lookUpGainToApply(float32(level)), "level %v", level)
	}
	assert.Equal(t, gainCurveRegionStats{identity: numSteps - 1, knee: 1}, stats)
}

// Port of CheckNoOverApproximationKnee.
func TestInterpolatedGainCurve_CheckNoOverApproximationKnee(t *testing.T) {
	const numSteps = 10
	l := newLimiterDBGainCurve()
	var stats gainCurveRegionStats
	for _, level := range linSpace(l.kneeStartLinear+levelEpsilon, l.limiterStartLinear, numSteps) {
		gain := float64(stats.lookUpGainToApply(float32(level)))
		// Like upstream, a small tolerance is added for comparing a float
		// with a double.
		assert.LessOrEqual(t, gain, l.getGainLinear(level)+noOverApproximationTolerance, "level %v", level)
	}
	assert.Equal(t, gainCurveRegionStats{knee: numSteps - 1, limiter: 1}, stats)
}

// Port of CheckNoOverApproximationBeyondKnee.
func TestInterpolatedGainCurve_CheckNoOverApproximationBeyondKnee(t *testing.T) {
	const numSteps = 10
	l := newLimiterDBGainCurve()
	var stats gainCurveRegionStats
	for _, level := range linSpace(l.limiterStartLinear+levelEpsilon, l.maxInputLevelLinear-levelEpsilon, numSteps) {
		gain := float64(stats.lookUpGainToApply(float32(level)))
		assert.LessOrEqual(t, gain, l.getGainLinear(level)+noOverApproximationTolerance, "level %v", level)
	}
	assert.Equal(t, gainCurveRegionStats{limiter: numSteps}, stats)
}

// Port of CheckNoOverApproximationWithSaturation.
func TestInterpolatedGainCurve_CheckNoOverApproximationWithSaturation(t *testing.T) {
	const numSteps = 3
	l := newLimiterDBGainCurve()
	var stats gainCurveRegionStats
	for _, level := range linSpace(l.maxInputLevelLinear+levelEpsilon, l.maxInputLevelLinear+levelEpsilon+0.5,
		numSteps) {
		gain := float64(stats.lookUpGainToApply(float32(level)))
		assert.LessOrEqual(t, gain, l.getGainLinear(level), "level %v", level)
	}
	assert.Equal(t, gainCurveRegionStats{saturation: numSteps}, stats)
}

// Port of CheckApproximationParams: the tables match the parameters
// computed from the limiter gain curve.
func TestInterpolatedGainCurve_CheckApproximationParams(t *testing.T) {
	params := computeInterpolatedGainCurveApproximationParams()
	for i := range kInterpolatedGainCurveTotalPoints {
		// Like upstream, the tolerances account for the deviations due to
		// computing in single precision.
		require.InDelta(t, approximationParamsX[i], params.x[i], 0.9, "x[%d]", i)
		require.InDelta(t, approximationParamsM[i], params.m[i], 0.00001, "m[%d]", i)
		require.InDelta(t, approximationParamsQ[i], params.q[i], 0.001, "q[%d]", i)
	}
}
