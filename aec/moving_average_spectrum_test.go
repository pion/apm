// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const movingAverageTestNumElem = 4

func movingAverageTestData() [4][]float32 {
	return [4][]float32{{1, 2, 3, 4}, {5, 1, 9, 7}, {3, 3, 5, 6}, {8, 4, 2, 1}}
}

// assertMovingAverage checks that the output is the average of the given
// inputs.
func assertMovingAverage(t *testing.T, output []float32, inputs ...[]float32) {
	t.Helper()
	for k := range output {
		var total float32
		for _, input := range inputs {
			total += input[k]
		}
		assert.InDelta(t, total/float32(len(inputs)), output[k], 1e-6)
	}
}

// Port of MovingAverageSpectrum.Average.
func TestMovingAverageSpectrum_Average(t *testing.T) {
	data := movingAverageTestData()
	average := newMovingAverageSpectrum(movingAverageTestNumElem, 3)
	output := make([]float32, movingAverageTestNumElem)

	average.average(data[0], output)
	assertMovingAverage(t, output, data[0])

	average.average(data[1], output)
	assertMovingAverage(t, output, data[0], data[1])

	average.average(data[2], output)
	assertMovingAverage(t, output, data[0], data[1], data[2])

	average.average(data[3], output)
	assertMovingAverage(t, output, data[1], data[2], data[3])
}

// Port of MovingAverageSpectrum.UpdateMemoryLength.
func TestMovingAverageSpectrum_UpdateMemoryLength(t *testing.T) {
	data := movingAverageTestData()
	average := newMovingAverageSpectrum(movingAverageTestNumElem, 3)
	output := make([]float32, movingAverageTestNumElem)

	average.average(data[0], output)
	assert.InDelta(t, data[0][0], output[0], 1e-6)

	// After the update, it should behave as if it was just created with a
	// memory length of 1.
	average.updateMemoryLength(1)
	average.average(data[1], output)
	assertMovingAverage(t, output, data[1])
}

// Port of MovingAverageSpectrum.PassThrough.
func TestMovingAverageSpectrum_PassThrough(t *testing.T) {
	average := newMovingAverageSpectrum(movingAverageTestNumElem, 1)
	output := make([]float32, movingAverageTestNumElem)

	for _, input := range movingAverageTestData() {
		average.average(input, output)
		assertMovingAverage(t, output, input)
	}
}
