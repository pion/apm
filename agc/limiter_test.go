// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Constants of agc2_testing_common.h.
const (
	testDecayMs                  float32 = 20
	testLimiterMaxInputLevelDBFS float32 = 1
	testLimiterSamplesPerChannel         = 480
)

// dbfsToFloatS16 mirrors DbfsToFloatS16.
func dbfsToFloatS16(v float32) float32 {
	return dbToRatio(v) * kMaxAbsFloatS16Value
}

// Port of LimiterShouldConstructAndRun.
func TestLimiter_LimiterShouldConstructAndRun(t *testing.T) {
	frame := newFrame(1, testLimiterSamplesPerChannel, kMaxAbsFloatS16Value)
	assert.NotPanics(t, func() { newLimiter(testLimiterSamplesPerChannel).process(frame) })
}

// Port of OutputVolumeAboveThreshold.
func TestLimiter_OutputVolumeAboveThreshold(t *testing.T) {
	inputLevel := (kMaxAbsFloatS16Value + dbfsToFloatS16(testLimiterMaxInputLevelDBFS)) / 2
	l := newLimiter(testLimiterSamplesPerChannel)

	// Give the level estimator time to adapt.
	for range 5 {
		l.process(newFrame(1, testLimiterSamplesPerChannel, inputLevel))
	}

	frame := newFrame(1, testLimiterSamplesPerChannel, inputLevel)
	l.process(frame)
	for _, sample := range frame[0] {
		require.Less(t, 0.9*kMaxAbsFloatS16Value, sample)
	}
}
