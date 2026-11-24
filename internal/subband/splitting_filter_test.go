// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplittingFilter_RejectsBadBands(t *testing.T) {
	_, err := NewSplittingFilter(1, 4)
	assert.ErrorIs(t, err, ErrInvalidConfig, "4 bands")
	_, err = NewSplittingFilter(0, 2)
	assert.ErrorIs(t, err, ErrInvalidConfig, "0 channels")
}

func TestSplittingFilter_2Band(t *testing.T) {
	sf, err := NewSplittingFilter(1, 2)
	require.NoError(t, err)
	in := [][]float32{make([]float32, 320)}
	rng := rand.New(rand.NewPCG(3, 3)) //nolint:gosec // Deterministic test signal.
	for i := range in[0] {
		in[0][i] = float32(rng.NormFloat64() * 1000)
	}
	bands := [][][]float32{
		{make([]float32, 160)},
		{make([]float32, 160)},
	}
	require.NoError(t, sf.Analysis(in, bands))
	out := [][]float32{make([]float32, 320)}
	require.NoError(t, sf.Synthesis(bands, out))
	// Just confirm shape: full SNR check is in TestQMF_RoundTrip.
	assert.Len(t, out[0], 320)
}

// sineChunk fills chunk i of the input of
// SplitsIntoThreeBandsAndReconstructs with the sines of the bands that
// the bits of i select. Like upstream, each sine is computed in double and
// added to the float input.
func sineChunk(in []float32, i int, frequenciesHz []float64, amplitude float32) []bool {
	isPresent := make([]bool, len(frequenciesHz))
	clear(in)
	for j, frequencyHz := range frequenciesHz {
		isPresent[j] = i&(1<<j) != 0
		var bandAmplitude float64
		if isPresent[j] {
			bandAmplitude = float64(amplitude)
		}
		for k := range in {
			sample := bandAmplitude * math.Sin(2*math.Pi*frequencyHz*float64(i*len(in)+k)/48000)
			in[k] = float32(float64(in[k]) + sample)
		}
	}

	return isPresent
}

// maxCrossCorrelation returns the largest cross correlation of in and out
// over the delays of out.
func maxCrossCorrelation(in, out []float32) float32 {
	var xcorr float32
	for delay := range out {
		var corr float32
		for j := delay; j < len(out); j++ {
			corr += in[j-delay] * out[j]
		}
		corr /= float32(len(out))
		xcorr = max(xcorr, corr)
	}

	return xcorr
}

// Port of SplittingFilterTest.SplitsIntoThreeBandsAndReconstructs: a signal
// made of sines of different frequencies is split into three bands, whose
// energies tell which sines are present, and merged back with a high
// cross correlation with the input.
func TestSplittingFilter_SplitsIntoThreeBandsAndReconstructs(t *testing.T) {
	const amplitude float32 = 8192
	frequenciesHz := []float64{1000, 12000, 18000}
	sf, err := NewSplittingFilter(1, 3)
	require.NoError(t, err)
	in := [][]float32{make([]float32, ThreeBandFullBandSize)}
	out := [][]float32{make([]float32, ThreeBandFullBandSize)}
	bands := make([][][]float32, 3)
	for j := range bands {
		bands[j] = [][]float32{make([]float32, ThreeBandSplitBandSize)}
	}

	for i := range 8 {
		isPresent := sineChunk(in[0], i, frequenciesHz, amplitude)

		require.NoError(t, sf.Analysis(in, bands))
		for j, present := range isPresent {
			var energy float32
			for _, sample := range bands[j][0] {
				energy += sample * sample
			}
			energy /= ThreeBandSplitBandSize
			if present {
				assert.Greater(t, energy, amplitude*amplitude/4, "chunk %d, band %d", i, j)
			} else {
				assert.Less(t, energy, amplitude*amplitude/4, "chunk %d, band %d", i, j)
			}
		}

		require.NoError(t, sf.Synthesis(bands, out))
		if slices.Contains(isPresent, true) {
			assert.Greater(t, maxCrossCorrelation(in[0], out[0]), amplitude*amplitude/4, "chunk %d", i)
		}
	}
}
