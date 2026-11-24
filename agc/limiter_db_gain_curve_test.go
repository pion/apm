// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Limiter parameters of agc2_testing_common.h.
const (
	testLimiterKneeSmoothnessDB float32 = 1
	testLimiterCompressionRatio float32 = 5
)

// floatS16ToDbfs mirrors FloatS16ToDbfs.
func floatS16ToDbfs(v float32) float32 {
	const minDbfs float32 = -90.30899869919436
	if v <= 1 {
		return minDbfs
	}

	return 20*float32(math.Log10(float64(v))) + minDbfs
}

// limiterDBGainCurve mirrors LimiterDbGainCurve, the test-only limiter gain
// curve from which the tables of the interpolated gain curve are computed.
// Like upstream, it computes in double, through the float conversions of
// DbfsToFloatS16 and FloatS16ToDbfs.
type limiterDBGainCurve struct {
	maxInputLevelDB      float64
	kneeSmoothnessDB     float64
	compressionRatio     float64
	maxInputLevelLinear  float64
	kneeStartDbfs        float64
	kneeStartLinear      float64
	limiterStartDbfs     float64
	limiterStartLinear   float64
	kneeRegionPolynomial [3]float64
	gainCurveLimiterD1   float64
	gainCurveLimiterD2   float64
	gainCurveLimiterI1   float64
	gainCurveLimiterI2   float64
}

func newLimiterDBGainCurve() *limiterDBGainCurve {
	maxInputLevelDB := float64(testLimiterMaxInputLevelDBFS)
	kneeSmoothnessDB := float64(testLimiterKneeSmoothnessDB)
	compressionRatio := float64(testLimiterCompressionRatio)
	maxAbs := float64(kMaxAbsFloatS16Value)
	curve := &limiterDBGainCurve{
		maxInputLevelDB:     maxInputLevelDB,
		kneeSmoothnessDB:    kneeSmoothnessDB,
		compressionRatio:    compressionRatio,
		maxInputLevelLinear: float64(dbfsToFloatS16(float32(maxInputLevelDB))),
		// ComputeKneeStart.
		kneeStartDbfs: -kneeSmoothnessDB/2.0 - maxInputLevelDB/(compressionRatio-1.0),
	}
	curve.kneeStartLinear = float64(dbfsToFloatS16(float32(curve.kneeStartDbfs)))
	curve.limiterStartDbfs = curve.kneeStartDbfs + kneeSmoothnessDB
	curve.limiterStartLinear = float64(dbfsToFloatS16(float32(curve.limiterStartDbfs)))

	// ComputeKneeRegionPolynomial.
	a := (1.0 - compressionRatio) / (2.0 * kneeSmoothnessDB * compressionRatio)
	b := 1.0 - 2.0*a*curve.kneeStartDbfs
	c := a * curve.kneeStartDbfs * curve.kneeStartDbfs
	curve.kneeRegionPolynomial = [3]float64{a, b, c}

	// ComputeLimiterD1, ComputeLimiterD2 and ComputeLimiterI2.
	limiterLevel := math.Pow(10.0, -maxInputLevelDB/(20.0*compressionRatio))
	curve.gainCurveLimiterD1 = (limiterLevel * (1.0 - compressionRatio) / compressionRatio) / maxAbs
	curve.gainCurveLimiterD2 = (1.0 - 2.0*compressionRatio) / compressionRatio
	curve.gainCurveLimiterI1 = 1.0 / compressionRatio
	curve.gainCurveLimiterI2 = limiterLevel / curve.gainCurveLimiterI1 / math.Pow(maxAbs, curve.gainCurveLimiterI1-1)

	return curve
}

// getOutputLevelDbfs mirrors GetOutputLevelDbfs.
func (l *limiterDBGainCurve) getOutputLevelDbfs(inputLevelDbfs float64) float64 {
	switch {
	case inputLevelDbfs < l.kneeStartDbfs:
		return inputLevelDbfs
	case inputLevelDbfs < l.limiterStartDbfs:
		p := l.kneeRegionPolynomial

		return p[0]*inputLevelDbfs*inputLevelDbfs + p[1]*inputLevelDbfs + p[2]
	default:
		return (inputLevelDbfs - l.maxInputLevelDB) / l.compressionRatio
	}
}

// getGainLinear mirrors GetGainLinear.
func (l *limiterDBGainCurve) getGainLinear(inputLevelLinear float64) float64 {
	if inputLevelLinear < l.kneeStartLinear {
		return 1.0
	}
	outputLevelDbfs := l.getOutputLevelDbfs(float64(floatS16ToDbfs(float32(inputLevelLinear))))

	return float64(dbfsToFloatS16(float32(outputLevelDbfs))) / inputLevelLinear
}

// limiterCurveLevels returns the levels the upstream tests sweep: from -90
// dBFS to the maximum input level, in float steps of 0.5 dB.
func limiterCurveLevels(l *limiterDBGainCurve) []float32 {
	var levels []float32
	for level := float32(-90); float64(level) <= l.maxInputLevelDB; level += 0.5 {
		levels = append(levels, level)
	}

	return levels
}

// Port of ConstructDestruct, with the checks of the upstream constructor.
func TestLimiterDBGainCurve_ConstructDestruct(t *testing.T) {
	l := newLimiterDBGainCurve()
	assert.Less(t, (l.compressionRatio-1.0)*l.kneeSmoothnessDB/(2.0*l.compressionRatio), l.maxInputLevelDB)
	assert.GreaterOrEqual(t, l.maxInputLevelDB, l.kneeStartDbfs+l.kneeSmoothnessDB)
}

// Port of GainCurveShouldBeMonotone.
func TestLimiterDBGainCurve_GainCurveShouldBeMonotone(t *testing.T) {
	l := newLimiterDBGainCurve()
	levels := limiterCurveLevels(l)
	lastOutputLevel := float32(l.getOutputLevelDbfs(float64(levels[0])))
	for _, level := range levels {
		currentOutputLevel := float32(l.getOutputLevelDbfs(float64(level)))
		assert.LessOrEqual(t, lastOutputLevel, currentOutputLevel, "level %v", level)
		lastOutputLevel = currentOutputLevel
	}
}

// Port of GainCurveShouldBeContinuous.
func TestLimiterDBGainCurve_GainCurveShouldBeContinuous(t *testing.T) {
	const maxDelta float32 = 0.5
	l := newLimiterDBGainCurve()
	levels := limiterCurveLevels(l)
	lastOutputLevel := float32(l.getOutputLevelDbfs(float64(levels[0])))
	for _, level := range levels {
		currentOutputLevel := float32(l.getOutputLevelDbfs(float64(level)))
		assert.LessOrEqual(t, currentOutputLevel, lastOutputLevel+maxDelta, "level %v", level)
		lastOutputLevel = currentOutputLevel
	}
}

// Port of OutputGainShouldBeLessThanFullScale.
func TestLimiterDBGainCurve_OutputGainShouldBeLessThanFullScale(t *testing.T) {
	l := newLimiterDBGainCurve()
	for _, level := range limiterCurveLevels(l) {
		assert.LessOrEqual(t, float32(l.getOutputLevelDbfs(float64(level))), float32(0), "level %v", level)
	}
}
