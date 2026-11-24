// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func verifyErl(t *testing.T, estimator *erlEstimator, reference float32) {
	t.Helper()
	for _, value := range estimator.getErl() {
		assert.InDelta(t, reference, value, 0.001)
	}
	assert.InDelta(t, reference, estimator.getErlTimeDomain(), 0.001)
}

// runErlEstimates ports the body of ErlEstimatorMultiChannel.Estimates.
func runErlEstimates(t *testing.T, numRenderChannels, numCaptureChannels int) {
	t.Helper()
	x2 := make([][kFftLengthBy2Plus1]float32, numRenderChannels)
	y2 := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	convergedFilters := make([]bool, numCaptureChannels)
	convergedIdx := numCaptureChannels - 1
	convergedFilters[convergedIdx] = true
	estimator := newErlEstimator(0)
	fillRender := func(value float32) {
		for ch := range x2 {
			fill(x2[ch][:], value)
		}
	}
	update := func(times int) {
		for range times {
			estimator.update(convergedFilters, x2, y2)
		}
	}

	// Verifies that the ERL estimate is properly reduced to lower values.
	fillRender(500 * 1000 * 1000)
	fill(y2[convergedIdx][:], 10*x2[0][0])
	update(200)
	verifyErl(t, estimator, 10)

	// Verifies that the ERL is not immediately increased when the ERL in
	// the data increases.
	fill(y2[convergedIdx][:], 10000*x2[0][0])
	update(998)
	verifyErl(t, estimator, 10)

	// Verifies that the rate of increase is 3 dB.
	update(1)
	verifyErl(t, estimator, 20)

	// Verifies that the maximum ERL is achieved when there are no low ERL
	// estimates.
	update(1000)
	verifyErl(t, estimator, 1000)

	// Verifies that the ERL estimate is not updated for low-level signals.
	fillRender(1000 * 1000)
	fill(y2[convergedIdx][:], 10*x2[0][0])
	update(200)
	verifyErl(t, estimator, 1000)
}

// Port of ErlEstimatorMultiChannel.Estimates: the correct ERL estimates
// are achieved.
func TestErlEstimator_Estimates(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 8} {
		for _, numCaptureChannels := range []int{1, 2, 8} {
			runErlEstimates(t, numRenderChannels, numCaptureChannels)
		}
	}
}

func TestErlEstimator_Reset(t *testing.T) {
	estimator := newErlEstimator(10)
	x2 := [][kFftLengthBy2Plus1]float32{{}}
	y2 := [][kFftLengthBy2Plus1]float32{{}}
	fill(x2[0][:], 500*1000*1000)
	fill(y2[0][:], 10*x2[0][0])

	// No update during the startup phase, which restarts on reset.
	for range 9 {
		estimator.update([]bool{true}, x2, y2)
	}
	assert.InDelta(t, maxErl, estimator.getErlTimeDomain(), 0)
	estimator.reset()
	for range 9 {
		estimator.update([]bool{true}, x2, y2)
	}
	assert.InDelta(t, maxErl, estimator.getErlTimeDomain(), 0)
	estimator.update([]bool{true}, x2, y2)
	assert.Less(t, estimator.getErlTimeDomain(), float32(maxErl))
}
