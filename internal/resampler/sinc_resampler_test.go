// SPDX-FileCopyrightText: 2012 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package resampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleRateRatio    = 192000.0 / 44100.0
	testDefaultRequestSize = 512
)

// countingSource stands for the MockSource of sinc_resampler_unittest.cc:
// it counts the calls and fills the buffer with value.
type countingSource struct {
	calls int
	value float32
}

func (s *countingSource) run(destination []float32) {
	s.calls++
	for i := range destination {
		destination[i] = s.value
	}
}

// Port of ChunkedResample: requesting multiples of chunkSize frames
// results in the proper number of callbacks.
func TestSincResampler_ChunkedResample(t *testing.T) {
	source := &countingSource{}
	// A high ratio of input to output samples exhausts the internal
	// buffers quickly.
	resampler := newSincResampler(testSampleRateRatio, testDefaultRequestSize, source.run)

	const chunks = 2
	destination := make([]float32, resampler.chunkSize()*chunks)

	resampler.resample(destination[:resampler.chunkSize()])
	assert.Equal(t, 1, source.calls)

	source.calls = 0
	resampler.resample(destination)
	assert.Equal(t, chunks, source.calls)
}

// Port of Flush: flush resets the internal state.
func TestSincResampler_Flush(t *testing.T) {
	// Like the FillBuffer action of upstream, every byte is 64.
	source := &countingSource{value: math.Float32frombits(0x40404040)}
	resampler := newSincResampler(testSampleRateRatio, testDefaultRequestSize, source.run)
	destination := make([]float32, resampler.chunkSize()/2)

	// Fill the resampler with junk data.
	resampler.resample(destination)
	assert.Equal(t, 1, source.calls)
	require.NotZero(t, destination[0])

	// Flush and request more data, which should all be zeros now.
	resampler.flush()
	source.calls = 0
	source.value = 0
	resampler.resample(destination)
	assert.Equal(t, 1, source.calls)
	assert.Equal(t, make([]float32, len(destination)), destination)
}

// Port of SincResamplerTest.Resample, without the change of ratio:
// SetRatio is not ported. The thresholds are in dBFS.
func TestSincResampler_Resample(t *testing.T) {
	// Almost all conversions have an RMS error of around -14 dBFS.
	const rmsErr = -14.58
	// All conversions have a high frequency error around -6 dBFS.
	const highFrequencyMaxError = -6.02
	for _, tc := range []struct {
		inputRate, outputRate  int
		rmsError, lowFreqError float64
	}{
		// To 22.05 kHz.
		{8000, 22050, rmsErr, -62.73},
		{11025, 22050, rmsErr, -72.19},
		{16000, 22050, rmsErr, -62.54},
		{22050, 22050, rmsErr, -73.53},
		{32000, 22050, rmsErr, -46.45},
		{44100, 22050, rmsErr, -28.49},
		{48000, 22050, -15.01, -25.56},
		{96000, 22050, -18.49, -13.42},
		{192000, 22050, -20.50, -9.23},
		// To 44.1 kHz.
		{8000, 44100, rmsErr, -62.73},
		{11025, 44100, rmsErr, -72.19},
		{16000, 44100, rmsErr, -62.54},
		{22050, 44100, rmsErr, -73.53},
		{32000, 44100, rmsErr, -63.32},
		{44100, 44100, rmsErr, -73.52},
		{48000, 44100, -15.01, -64.04},
		{96000, 44100, -18.49, -25.51},
		{192000, 44100, -20.50, -13.31},
		// To 48 kHz.
		{8000, 48000, rmsErr, -63.43},
		{11025, 48000, rmsErr, -62.61},
		{16000, 48000, rmsErr, -63.95},
		{22050, 48000, rmsErr, -62.42},
		{32000, 48000, rmsErr, -64.04},
		{44100, 48000, rmsErr, -62.63},
		{48000, 48000, rmsErr, -73.52},
		{96000, 48000, -18.40, -28.44},
		{192000, 48000, -20.43, -14.11},
		// To 96 kHz.
		{8000, 96000, rmsErr, -63.19},
		{11025, 96000, rmsErr, -62.61},
		{16000, 96000, rmsErr, -63.39},
		{22050, 96000, rmsErr, -62.42},
		{32000, 96000, rmsErr, -63.95},
		{44100, 96000, rmsErr, -62.63},
		{48000, 96000, rmsErr, -73.52},
		{96000, 96000, rmsErr, -73.52},
		{192000, 96000, rmsErr, -28.41},
		// To 192 kHz.
		{8000, 192000, rmsErr, -63.10},
		{11025, 192000, rmsErr, -62.61},
		{16000, 192000, rmsErr, -63.14},
		{22050, 192000, rmsErr, -62.42},
		{32000, 192000, rmsErr, -63.38},
		{44100, 192000, rmsErr, -62.63},
		{48000, 192000, rmsErr, -73.44},
		{96000, 192000, rmsErr, -73.52},
		{192000, 192000, rmsErr, -73.52},
	} {
		// Make comparisons using one second of data.
		inputNyquistFreq := 0.5 * float64(tc.inputRate)
		source := newSinusoidalLinearChirpSource(tc.inputRate, tc.inputRate, inputNyquistFreq, 0)
		resampler := newSincResampler(float64(tc.inputRate)/float64(tc.outputRate), testDefaultRequestSize,
			source.run)

		resampled := make([]float32, tc.outputRate)
		resampler.resample(resampled)

		pure := make([]float32, tc.outputRate)
		pureSource := newSinusoidalLinearChirpSource(tc.outputRate, tc.outputRate, inputNyquistFreq, 0)
		pureSource.run(pure)

		rmsError, lowFreqMaxError, highFreqMaxError := resamplingErrors(resampled, pure, pureSource,
			min(tc.inputRate, tc.outputRate), 0)
		assert.LessOrEqual(t, rmsError, tc.rmsError, "%d to %d Hz", tc.inputRate, tc.outputRate)
		assert.LessOrEqual(t, lowFreqMaxError, tc.lowFreqError, "%d to %d Hz", tc.inputRate, tc.outputRate)
		assert.LessOrEqual(t, highFreqMaxError, float64(highFrequencyMaxError), "%d to %d Hz", tc.inputRate,
			tc.outputRate)
	}
}
