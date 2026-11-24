// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEchoPathVariability_AudioPathChanged(t *testing.T) {
	assert.False(t, echoPathVariability{}.audioPathChanged())
	assert.False(t, echoPathVariability{clockDrift: true}.audioPathChanged())
	assert.True(t, echoPathVariability{gainChange: true}.audioPathChanged())
	assert.True(t, echoPathVariability{delayChange: delayAdjustmentBufferFlush}.audioPathChanged())
	assert.True(t, echoPathVariability{delayChange: delayAdjustmentNewDetectedDelay}.audioPathChanged())
}

// Port of CorrectBehavior.
func TestEchoPathVariability_CorrectBehavior(t *testing.T) {
	for _, tc := range []struct {
		gainChange  bool
		delayChange delayAdjustment
		changed     bool
	}{
		{true, delayAdjustmentNewDetectedDelay, true},
		{true, delayAdjustmentNone, true},
		{false, delayAdjustmentNewDetectedDelay, true},
		{false, delayAdjustmentNone, false},
	} {
		v := echoPathVariability{gainChange: tc.gainChange, delayChange: tc.delayChange}
		assert.Equal(t, tc.changed, v.audioPathChanged())
		assert.False(t, v.clockDrift)
	}
}
