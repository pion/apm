// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import "math"

// limiter mirrors Agc2's Limiter: envelope estimator + interpolated gain
// curve + per-sample interpolation + final clip.
type limiter struct {
	level             *fixedDigitalLevelEstimator
	scalingFactors    [kSubFramesInFrame + 1]float32
	perSampleScaling  [kMaxSamplesPerChannel]float32
	lastScalingFactor float32
}

const attackFirstSubframeInterpolationPower = 8.0

func newLimiter(samplesPerChannel int) *limiter {
	return &limiter{
		level:             newLevelEstimator(samplesPerChannel),
		lastScalingFactor: 1,
	}
}

// reset matches Limiter::Reset: it resets the level estimator only.
func (l *limiter) reset() {
	l.level.reset()
}

// process applies the limiter to channels in place.
func (l *limiter) process(channels [][]float32) {
	var levelEst [kSubFramesInFrame]float32
	l.level.computeLevel(channels, &levelEst)

	l.scalingFactors[0] = l.lastScalingFactor
	for i, v := range levelEst {
		l.scalingFactors[i+1] = lookUpGainToApply(v)
	}

	n := len(channels[0])
	subframeSize := n / kSubFramesInFrame
	perSample := l.perSampleScaling[:n]

	isAttack := l.scalingFactors[0] > l.scalingFactors[1]
	if isAttack {
		last := l.scalingFactors[0]
		cur := l.scalingFactors[1]
		for i := range subframeSize {
			t := float64(i) / float64(subframeSize)
			perSample[i] = float32(math.Pow(1.0-t, attackFirstSubframeInterpolationPower))*(last-cur) + cur
		}
	}

	startSub := 0
	if isAttack {
		startSub = 1
	}
	for sf := startSub; sf < kSubFramesInFrame; sf++ {
		start := sf * subframeSize
		s0 := l.scalingFactors[sf]
		s1 := l.scalingFactors[sf+1]
		diff := (s1 - s0) / float32(subframeSize)
		for j := range subframeSize {
			perSample[start+j] = s0 + diff*float32(j)
		}
	}

	for _, ch := range channels {
		for j := range n {
			ch[j] = safeClamp(ch[j]*perSample[j], kMinFloatS16Value, kMaxFloatS16Value)
		}
	}

	l.lastScalingFactor = l.scalingFactors[kSubFramesInFrame]
}
