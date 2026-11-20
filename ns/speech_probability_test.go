// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ns

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func meanSpeechProbability(t *testing.T, snr float32) float32 {
	t.Helper()

	est := newSpeechProbabilityEstimator()
	var spectrum [fftSizeBy2Plus1]float32
	var sum float32
	for i := range spectrum {
		spectrum[i] = 100 + float32(i)
		sum += spectrum[i]
	}
	snrs := constantSpectrum(snr)
	// 600 frames run past one feature window, so the prior model is updated too.
	for frame := range int32(600) {
		est.update(frame, snrs, snrs, constantSpectrum(100), &spectrum, sum, 1e4)
	}

	assert.GreaterOrEqual(t, est.priorSpeechProb, float32(0.01))
	assert.LessOrEqual(t, est.priorSpeechProb, float32(1))
	var mean float32
	for _, p := range est.speechProb {
		assert.GreaterOrEqual(t, p, float32(0))
		assert.LessOrEqual(t, p, float32(1))
		mean += p
	}

	return mean / fftSizeBy2Plus1
}

func TestSpeechProbabilityFollowsSnr(t *testing.T) {
	assert.Less(t, meanSpeechProbability(t, 0), float32(0.2))
	assert.Greater(t, meanSpeechProbability(t, 10), float32(0.9))
}

func TestFindFirstOfTwoLargestPeaksMergesNeighbors(t *testing.T) {
	var hist [kHistogramSize]int
	hist[10] = 300
	hist[11] = 200
	hist[50] = 100

	var pos float32
	var weight int
	findFirstOfTwoLargestPeaks(binSizeSpecFlat, &hist, &pos, &weight)
	assert.Equal(t, 500, weight)
	assert.InDelta(t, 11*binSizeSpecFlat, pos, 1e-6)
}
