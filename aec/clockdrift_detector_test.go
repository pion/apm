// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Port of ClockdriftDetector.ClockdriftDetector.
func TestClockdriftDetector(t *testing.T) {
	var detector clockdriftDetector
	update := func(delay, times int) {
		for range times {
			detector.update(delay)
		}
	}

	// No clockdrift at start.
	assert.Equal(t, clockdriftNone, detector.clockdriftLevel())

	// Monotonically increasing delay.
	update(1000, 100)
	assert.Equal(t, clockdriftNone, detector.clockdriftLevel())
	update(1001, 100)
	assert.Equal(t, clockdriftNone, detector.clockdriftLevel())
	update(1002, 100)

	// Probable clockdrift.
	assert.Equal(t, clockdriftProbable, detector.clockdriftLevel())
	update(1003, 100)

	// Verified clockdrift.
	assert.Equal(t, clockdriftVerified, detector.clockdriftLevel())

	// Stable delay.
	update(1003, 10000)

	// No clockdrift.
	assert.Equal(t, clockdriftNone, detector.clockdriftLevel())

	// Decreasing delay.
	update(1001, 100)
	update(999, 100)

	// Probable clockdrift.
	assert.Equal(t, clockdriftProbable, detector.clockdriftLevel())
	update(1000, 100)
	update(998, 100)

	// Verified clockdrift.
	assert.Equal(t, clockdriftVerified, detector.clockdriftLevel())
}
