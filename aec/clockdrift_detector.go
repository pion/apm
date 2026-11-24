// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// clockdriftLevel mirrors ClockdriftDetector::Level.
type clockdriftLevel int

const (
	clockdriftNone clockdriftLevel = iota
	clockdriftProbable
	clockdriftVerified
)

// clockdriftStabilityBlocks is the number of blocks with a stable delay
// estimate after which the clockdrift level is reset: 30 seconds.
const clockdriftStabilityBlocks = 7500

// clockdriftDetector mirrors ClockdriftDetector: it detects a clock drift
// between render and capture from a delay estimate that keeps moving in
// one direction.
type clockdriftDetector struct {
	delayHistory     [3]int
	level            clockdriftLevel
	stabilityCounter int
}

// update mirrors Update.
func (d *clockdriftDetector) update(delayEstimate int) {
	if delayEstimate == d.delayHistory[0] {
		// Reset the clockdrift level if the delay estimate is stable.
		d.stabilityCounter++
		if d.stabilityCounter > clockdriftStabilityBlocks {
			d.level = clockdriftNone
		}

		return
	}

	d.stabilityCounter = 0
	probable, verified := clockdriftPattern(
		d.delayHistory[0]-delayEstimate, d.delayHistory[1]-delayEstimate, d.delayHistory[2]-delayEstimate)

	// Set the clockdrift level.
	if verified {
		d.level = clockdriftVerified
	} else if probable && d.level == clockdriftNone {
		d.level = clockdriftProbable
	}

	// Shift the delay history one step.
	d.delayHistory[2] = d.delayHistory[1]
	d.delayHistory[1] = d.delayHistory[0]
	d.delayHistory[0] = delayEstimate
}

// clockdriftPattern tells whether the differences between the last three
// delay estimates and the new one show a drift, and whether the third
// difference verifies it.
func clockdriftPattern(d1, d2, d3 int) (probable, verified bool) {
	// Patterns recognized as positive clockdrift:
	// [x-3], x-2, x-1, x.
	// [x-3], x-1, x-2, x.
	probableDriftUp := isDriftPair(d1, d2, -1)
	driftUp := probableDriftUp && d3 == -3

	// Patterns recognized as negative clockdrift:
	// [x+3], x+2, x+1, x.
	// [x+3], x+1, x+2, x.
	probableDriftDown := isDriftPair(d1, d2, 1)
	driftDown := probableDriftDown && d3 == 3

	return probableDriftUp || probableDriftDown, driftUp || driftDown
}

// isDriftPair tells whether d1 and d2 are one and two steps away, in
// either order, in the direction of sign.
func isDriftPair(d1, d2, sign int) bool {
	return (d1 == sign && d2 == 2*sign) || (d1 == 2*sign && d2 == sign)
}

// clockdriftLevel mirrors ClockdriftLevel.
func (d *clockdriftDetector) clockdriftLevel() clockdriftLevel { return d.level }
