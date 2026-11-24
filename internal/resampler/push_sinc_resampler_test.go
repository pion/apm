// SPDX-FileCopyrightText: 2013 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package resampler

import (
	"fmt"
	"math"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceDelta bounds the difference to upstream for samples in
// [-1, 1]. It allows for the last-bit differences between math libraries
// in the kernel and for fused multiply-add on some architectures.
const referenceDelta = 1e-5

// TestPushSincResampler_MatchesUpstream compares the output with the
// output of the upstream PushSincResampler at the pinned commit, built
// with the plain C convolution and fed the same generated noise in 10 ms
// chunks. Each line of the reference file is keyed by
// "source_rate destination_rate chunk".
func TestPushSincResampler_MatchesUpstream(t *testing.T) {
	type conversion struct{ sourceRate, destinationRate int }
	want := map[conversion]map[int][]float32{}
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		var key conversion
		var chunk int
		_, err := fmt.Sscan(entry.Key, &key.sourceRate, &key.destinationRate, &chunk)
		require.NoError(t, err)
		if want[key] == nil {
			want[key] = map[int][]float32{}
		}
		want[key][chunk] = entry.Samples
	}

	for key, chunks := range want {
		t.Run(fmt.Sprintf("%d_to_%d", key.sourceRate, key.destinationRate), func(t *testing.T) {
			resampler, err := NewPushSincResampler(key.sourceRate/100, key.destinationRate/100)
			require.NoError(t, err)

			source := make([]float32, key.sourceRate/100)
			destination := make([]float32, key.destinationRate/100)
			seed := uint32(1)
			for chunk := range 10 {
				for i := range source {
					seed = seed*1664525 + 1013904223
					source[i] = float32(int(seed>>16)-32768) / 32768
				}
				require.NoError(t, resampler.Resample(source, destination))
				if samples, ok := chunks[chunk]; ok {
					assert.InDeltaSlice(t, samples, destination, referenceDelta, "chunk %d", chunk)
				}
			}
		})
	}
}

// A tone well inside the pass band keeps its level through a rate change
// and back, delayed by half the kernel on each side.
func TestPushSincResampler_RoundTripPreservesTone(t *testing.T) {
	const (
		lowFrames  = 80
		highFrames = 160
		toneHz     = 1000.0
	)
	up, err := NewPushSincResampler(lowFrames, highFrames)
	require.NoError(t, err)
	down, err := NewPushSincResampler(highFrames, lowFrames)
	require.NoError(t, err)

	source := make([]float32, lowFrames)
	high := make([]float32, highFrames)
	output := make([]float32, lowFrames)
	var inEnergy, outEnergy float64
	for chunk := range 50 {
		for i := range source {
			source[i] = float32(0.5 * math.Sin(2*math.Pi*toneHz*float64(chunk*lowFrames+i)/8000))
		}
		require.NoError(t, up.Resample(source, high))
		require.NoError(t, down.Resample(high, output))
		if chunk < 5 {
			continue
		}
		for i := range source {
			inEnergy += float64(source[i]) * float64(source[i])
			outEnergy += float64(output[i]) * float64(output[i])
		}
	}
	assert.InDelta(t, 0, 10*math.Log10(outEnergy/inEnergy), 0.1)
}

func TestPushSincResampler_RejectsBadShapes(t *testing.T) {
	_, err := NewPushSincResampler(48, 96)
	require.ErrorIs(t, err, ErrFrameShape)
	_, err = NewPushSincResampler(80, 0)
	require.ErrorIs(t, err, ErrFrameShape)

	resampler, err := NewPushSincResampler(80, 160)
	require.NoError(t, err)
	assert.ErrorIs(t, resampler.Resample(make([]float32, 79), make([]float32, 160)), ErrFrameShape)
	assert.ErrorIs(t, resampler.Resample(make([]float32, 80), make([]float32, 161)), ErrFrameShape)
}

func TestPushSincResampler_ZeroAlloc(t *testing.T) {
	resampler, err := NewPushSincResampler(80, 160)
	require.NoError(t, err)
	source := make([]float32, 80)
	destination := make([]float32, 160)
	require.NoError(t, resampler.Resample(source, destination))

	assert.Zero(t, testing.AllocsPerRun(100, func() {
		_ = resampler.Resample(source, destination)
	}))
}

// Port of PushSincResamplerTest.ResampleFloat: the resampled chirp is
// compared with the chirp generated at the output rate, delayed like the
// resampler. ResampleInt has no Go counterpart: the Go resampler only
// takes float. The thresholds are in dBFS.
func TestPushSincResampler_ResampleFloat(t *testing.T) {
	// Almost all conversions have an RMS error of around -14 dBFS.
	const rmsErr = -14.42
	// All conversions have a high frequency error around -6 dBFS.
	const highFrequencyMaxError = -6.01
	for _, tc := range []struct {
		inputRate, outputRate  int
		rmsError, lowFreqError float64
	}{
		// The rates of SincResamplerTest, with 22000 and 11000 Hz for 22050
		// and 11025 Hz. To 22 kHz.
		{8000, 22000, rmsErr, -62.73},
		{11000, 22000, rmsErr, -74.17},
		{16000, 22000, rmsErr, -62.54},
		{22000, 22000, rmsErr, -73.53},
		{32000, 22000, rmsErr, -46.45},
		{44100, 22000, rmsErr, -28.34},
		{48000, 22000, -15.01, -25.56},
		{96000, 22000, -18.49, -13.30},
		{192000, 22000, -20.50, -9.20},
		// To 44.1 kHz.
		{8000, 44100, rmsErr, -62.73},
		{11000, 44100, rmsErr, -63.57},
		{16000, 44100, rmsErr, -62.54},
		{22000, 44100, rmsErr, -62.73},
		{32000, 44100, rmsErr, -63.32},
		{44100, 44100, rmsErr, -73.53},
		{48000, 44100, -15.01, -64.04},
		{96000, 44100, -18.49, -25.51},
		{192000, 44100, -20.50, -13.31},
		// To 48 kHz.
		{8000, 48000, rmsErr, -63.43},
		{11000, 48000, rmsErr, -63.96},
		{16000, 48000, rmsErr, -63.96},
		{22000, 48000, rmsErr, -63.80},
		{32000, 48000, rmsErr, -64.04},
		{44100, 48000, rmsErr, -62.63},
		{48000, 48000, rmsErr, -73.52},
		{96000, 48000, -18.40, -28.44},
		{192000, 48000, -20.43, -14.11},
		// To 96 kHz.
		{8000, 96000, rmsErr, -63.19},
		{11000, 96000, rmsErr, -63.89},
		{16000, 96000, rmsErr, -63.39},
		{22000, 96000, rmsErr, -63.39},
		{32000, 96000, rmsErr, -63.95},
		{44100, 96000, rmsErr, -62.63},
		{48000, 96000, rmsErr, -73.52},
		{96000, 96000, rmsErr, -73.52},
		{192000, 96000, rmsErr, -28.41},
		// To 192 kHz.
		{8000, 192000, rmsErr, -63.10},
		{11000, 192000, rmsErr, -63.17},
		{16000, 192000, rmsErr, -63.14},
		{22000, 192000, rmsErr, -63.14},
		{32000, 192000, rmsErr, -63.38},
		{44100, 192000, rmsErr, -62.63},
		{48000, 192000, rmsErr, -73.44},
		{96000, 192000, rmsErr, -73.52},
		{192000, 192000, rmsErr, -73.52},
		// Additional cases for WebRTC. To 8 kHz.
		{8000, 8000, rmsErr, -75.50},
		{16000, 8000, -18.56, -28.79},
		{32000, 8000, -20.36, -14.13},
		{44100, 8000, -21.00, -11.39},
		{48000, 8000, -20.96, -11.04},
		// To 16 kHz.
		{8000, 16000, rmsErr, -70.30},
		{11000, 16000, rmsErr, -72.31},
		{16000, 16000, rmsErr, -75.51},
		{22000, 16000, rmsErr, -52.08},
		{32000, 16000, -18.48, -28.59},
		{44100, 16000, -19.30, -19.67},
		{48000, 16000, -19.81, -18.11},
		{96000, 16000, -20.95, -10.9596},
		// To 32 kHz.
		{8000, 32000, rmsErr, -70.30},
		{11000, 32000, rmsErr, -71.34},
		{16000, 32000, rmsErr, -75.51},
		{22000, 32000, rmsErr, -72.05},
		{32000, 32000, rmsErr, -75.51},
		{44100, 32000, -16.44, -51.0349},
		{48000, 32000, -16.90, -43.9967},
		{96000, 32000, -19.61, -18.04},
		{192000, 32000, -21.02, -10.94},
	} {
		name := fmt.Sprintf("%d to %d Hz", tc.inputRate, tc.outputRate)
		// Make comparisons using one second of data, in 10 ms blocks.
		inputBlockSize := tc.inputRate / 100
		outputBlockSize := tc.outputRate / 100
		inputNyquistFreq := 0.5 * float64(tc.inputRate)
		source := newSinusoidalLinearChirpSource(tc.inputRate, tc.inputRate, inputNyquistFreq, 0)
		resampler, err := NewPushSincResampler(inputBlockSize, outputBlockSize)
		require.NoError(t, err)

		// The delay of the resampler, about half the kernel at the input
		// rate, must be read before the first call, which changes chunkSize.
		outputDelaySamples := outputBlockSize - resampler.resampler.chunkSize()

		input := make([]float32, tc.inputRate)
		source.run(input)
		resampled := make([]float32, tc.outputRate)
		for i := range 100 {
			require.NoError(t, resampler.Resample(input[i*inputBlockSize:(i+1)*inputBlockSize],
				resampled[i*outputBlockSize:(i+1)*outputBlockSize]))
		}

		pure := make([]float32, tc.outputRate)
		pureSource := newSinusoidalLinearChirpSource(tc.outputRate, tc.outputRate, inputNyquistFreq,
			float64(outputDelaySamples))
		pureSource.run(pure)

		// Like upstream, the largest errors allow for the quantization to
		// int16 of the int test, at input and at output.
		rmsError, lowFreqMaxError, highFreqMaxError := resamplingErrors(resampled, pure, pureSource,
			min(tc.inputRate, tc.outputRate), 2.0/32767)
		assert.LessOrEqual(t, rmsError, tc.rmsError, name)
		assert.LessOrEqual(t, lowFreqMaxError, tc.lowFreqError, name)
		assert.LessOrEqual(t, highFreqMaxError, float64(highFrequencyMaxError), name)
	}
}
