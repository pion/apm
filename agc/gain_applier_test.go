// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// newFrame mirrors VectorFloatFrame: numChannels channels of
// samplesPerChannel samples set to value.
func newFrame(numChannels, samplesPerChannel int, value float32) [][]float32 {
	frame := make([][]float32, numChannels)
	for ch := range frame {
		frame[ch] = make([]float32, samplesPerChannel)
		for i := range frame[ch] {
			frame[ch][i] = value
		}
	}

	return frame
}

// Port of InitialGainIsRespected.
func TestGainApplier_InitialGainIsRespected(t *testing.T) {
	const initialSignalLevel = 123
	const gainFactor = 10
	frame := newFrame(1, 1, initialSignalLevel)
	newGainApplier(true, gainFactor).apply(frame)
	assert.InDelta(t, initialSignalLevel*gainFactor, frame[0][0], 0.1)
}

// Port of ClippingIsDone.
func TestGainApplier_ClippingIsDone(t *testing.T) {
	frame := newFrame(1, 1, 30000)
	newGainApplier(true, 10).apply(frame)
	assert.InDelta(t, math.MaxInt16, frame[0][0], 0.1)
}

// Port of ClippingIsNotDone.
func TestGainApplier_ClippingIsNotDone(t *testing.T) {
	const initialSignalLevel = 30000
	const gainFactor = 10
	frame := newFrame(1, 1, initialSignalLevel)
	newGainApplier(false, gainFactor).apply(frame)
	assert.InDelta(t, initialSignalLevel*gainFactor, frame[0][0], 0.1)
}

// Port of RampingIsDone.
func TestGainApplier_RampingIsDone(t *testing.T) {
	const initialSignalLevel = 30000
	const initialGainFactor = 1
	const targetGainFactor = 0.5
	const numChannels = 3
	const samplesPerChannel = 4
	frame := newFrame(numChannels, samplesPerChannel, initialSignalLevel)
	applier := newGainApplier(false, initialGainFactor)

	applier.setGainFactor(targetGainFactor)
	applier.apply(frame)

	// The maximal gain change should be close to that in linear
	// interpolation.
	const totalGainChange = (initialGainFactor - targetGainFactor) * initialSignalLevel
	for _, channel := range frame {
		var maxSignalChange float32
		lastSignalLevel := float32(initialSignalLevel)
		for _, sample := range channel {
			maxSignalChange = max(maxSignalChange, float32(math.Abs(float64(lastSignalLevel-sample))))
			lastSignalLevel = sample
		}
		assert.InDelta(t, totalGainChange/samplesPerChannel, maxSignalChange, 0.1)
	}

	// The next frame should have the new gain.
	next := newFrame(numChannels, samplesPerChannel, initialSignalLevel)
	applier.apply(next)
	assert.InDelta(t, initialSignalLevel*targetGainFactor, next[0][0], 0.1)
}
