// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

const levelEstimatorInputLevel float32 = 10000

// sampleRateToDefaultChannelSize mirrors SampleRateToDefaultChannelSize.
func sampleRateToDefaultChannelSize(sampleRateHz int) int { return sampleRateHz / 100 }

// testLevelEstimator mirrors TestLevelEstimator: it runs a constant
// signal through the level estimator and checks that the level falls
// within the bounds.
func testLevelEstimator(
	t *testing.T, samplesPerChannel, numChannels int, inputLevel, expectedMin, expectedMax float32,
) {
	t.Helper()
	estimator := newLevelEstimator(samplesPerChannel)
	frame := newFrame(numChannels, samplesPerChannel, inputLevel)
	var level [kSubFramesInFrame]float32

	for i := range 500 {
		estimator.computeLevel(frame, &level)

		// Give the estimator some time to ramp up.
		if i < 50 {
			continue
		}
		for _, x := range level {
			assert.LessOrEqual(t, expectedMin, x)
			assert.LessOrEqual(t, x, expectedMax)
		}
	}
}

// timeMsToDecreaseLevel mirrors TimeMsToDecreaseLevel: the time it takes
// for the level estimate to decrease by levelReductionDB.
func timeMsToDecreaseLevel(
	t *testing.T, samplesPerChannel, numChannels int, inputLevelDB, levelReductionDB float32,
) float32 {
	t.Helper()
	frame := newFrame(numChannels, samplesPerChannel, dbfsToFloatS16(inputLevelDB))
	estimator := newLevelEstimator(samplesPerChannel)
	var level [kSubFramesInFrame]float32

	// Give the estimator plenty of time to ramp up and stabilize.
	var lastLevel float32
	for range 500 {
		estimator.computeLevel(frame, &level)
		lastLevel = level[kSubFramesInFrame-1]
	}

	// Set the input to 0.
	zero := newFrame(numChannels, samplesPerChannel, 0)
	reducedLevelLinear := dbfsToFloatS16(inputLevelDB - levelReductionDB)
	subFramesUntilLevelReduction := 0
	for lastLevel > reducedLevelLinear {
		estimator.computeLevel(zero, &level)
		for _, v := range level {
			assert.Less(t, v, lastLevel)
			subFramesUntilLevelReduction++
			lastLevel = v
			if lastLevel <= reducedLevelLinear {
				break
			}
		}
	}

	return float32(subFramesUntilLevelReduction) * kFrameDurationMs / kSubFramesInFrame
}

// Port of EstimatorShouldNotCrash.
func TestLevelEstimator_EstimatorShouldNotCrash(t *testing.T) {
	testLevelEstimator(t, sampleRateToDefaultChannelSize(8000), 1, 0, -math.MaxFloat32, math.MaxFloat32)
}

// Port of EstimatorShouldEstimateConstantLevel.
func TestLevelEstimator_EstimatorShouldEstimateConstantLevel(t *testing.T) {
	testLevelEstimator(t, sampleRateToDefaultChannelSize(10000), 1, levelEstimatorInputLevel,
		levelEstimatorInputLevel*0.99, levelEstimatorInputLevel*1.01)
}

// Port of EstimatorShouldEstimateConstantLevelForManyChannels.
func TestLevelEstimator_EstimatorShouldEstimateConstantLevelForManyChannels(t *testing.T) {
	testLevelEstimator(t, sampleRateToDefaultChannelSize(20000), 10, levelEstimatorInputLevel,
		levelEstimatorInputLevel*0.99, levelEstimatorInputLevel*1.01)
}

func TestLevelEstimator_TimeToDecrease(t *testing.T) {
	const levelReductionDB = 25
	const expectedTime = levelReductionDB * testDecayMs
	for _, tc := range []struct {
		name           string
		sampleRateHz   int
		numChannels    int
		initialLevelDB float32
	}{
		// Port of TimeToDecreaseForLowLevel.
		{"low level", 22000, 1, -40},
		// Port of TimeToDecreaseForFullScaleLevel.
		{"full scale level", 26000, 1, 0},
		// Port of TimeToDecreaseForMultipleChannels.
		{"multiple channels", 28000, 10, 0},
	} {
		timeToDecrease := timeMsToDecreaseLevel(t, sampleRateToDefaultChannelSize(tc.sampleRateHz), tc.numChannels,
			tc.initialLevelDB, levelReductionDB)
		assert.LessOrEqual(t, expectedTime*0.9, timeToDecrease, tc.name)
		assert.LessOrEqual(t, timeToDecrease, expectedTime*1.1, tc.name)
	}
}
