// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fftPower ports Power: the average power of a spectrum.
func fftPower(spectrum *fftData) float32 {
	var power [kFftLengthBy2Plus1]float32
	spectrum.spectrum(&power)

	return sum(power[:]) / float32(len(power))
}

// Port of ComfortNoiseGenerator.CorrectLevel.
func TestComfortNoiseGenerator_CorrectLevel(t *testing.T) {
	const numChannels = 5
	generator := newComfortNoiseGenerator(defaultEchoCanceller3Config(), numChannels)
	n2 := make([][kFftLengthBy2Plus1]float32, numChannels)
	lower := make([]fftData, numChannels)
	upper := make([]fftData, numChannels)
	for ch := range n2 {
		fill(n2[ch][:], 1000*1000/float32(ch+1))
	}

	// Ensure an instantaneous update to nonzero noise.
	generator.compute(false, n2, lower, upper)
	for ch := range numChannels {
		assert.Positive(t, fftPower(&lower[ch]))
		assert.Positive(t, fftPower(&upper[ch]))
	}

	for range 10000 {
		generator.compute(false, n2, lower, upper)
	}
	for ch := range numChannels {
		assert.InDelta(t, 2*n2[ch][0], fftPower(&lower[ch]), float64(n2[ch][0]/10))
		assert.InDelta(t, 2*n2[ch][0], fftPower(&upper[ch]), float64(n2[ch][0]/10))
	}
}

// assertComfortNoise compares a spectrum with the next line of the
// reference that has this name, relative to its largest value.
func assertComfortNoise(t *testing.T, reference *referenceCursor, name string, got []float32, index int) {
	t.Helper()
	want := reference.next(name)
	var largest float32
	for _, value := range want {
		largest = max(largest, abs32(value))
	}
	require.InDeltaSlice(t, want, got, float64(largest)*1e-6, "%s, block %d", name, index)
}

// comfortNoiseScenarioLevel returns the level of the capture spectrum of
// a block of the reference scenario.
func comfortNoiseScenarioLevel(index int) float32 {
	switch {
	case inRange(index, 300, 600):
		return 1e4
	case inRange(index, 600, 700):
		return 1e-9
	case index >= 1100:
		return 1e8
	default:
		return 1e6
	}
}

// TestComfortNoiseGenerator_MatchesUpstream compares the noise spectrum
// and the comfort noise of the last channel with the upstream
// ComfortNoiseGenerator at the pinned commit.
//
// The capture spectrum is drawn at random. Its level is 1e6, 1e4 in
// blocks 300 to 599, 1e-9 in blocks 600 to 699 and 1e8 from block 1100
// on. The capture is saturated in blocks 100 to 119 and 1050 to 1059.
// Each line of the reference file is keyed by "name capture_channels".
func TestComfortNoiseGenerator_MatchesUpstream(t *testing.T) {
	for _, numChannels := range []int{1, 2} {
		t.Run(fmt.Sprint(numChannels), func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/comfort_noise_generator_upstream.txt",
				fmt.Sprintf(" %d", numChannels))
			generator := newComfortNoiseGenerator(defaultEchoCanceller3Config(), numChannels)
			random := newTestRandom(42)
			y2 := make([][kFftLengthBy2Plus1]float32, numChannels)
			lower := make([]fftData, numChannels)
			upper := make([]fftData, numChannels)
			last := numChannels - 1

			for index := range 1300 {
				for ch := range numChannels {
					for k := range y2[ch] {
						y2[ch][k] = comfortNoiseScenarioLevel(index) * (0.5 + random.randFloat()) / float32(ch+1)
					}
				}
				generator.compute(inRange(index, 100, 120) || inRange(index, 1050, 1060), y2, lower, upper)

				if index%100 == 0 || inRange(index, 1017, 1022) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					assertComfortNoise(t, reference, "noise_spectrum", generator.noiseSpectrum()[last][:], index)
					assertComfortNoise(t, reference, "lower_re", lower[last].re[:], index)
					assertComfortNoise(t, reference, "lower_im", lower[last].im[:], index)
					assertComfortNoise(t, reference, "upper_re", upper[last].re[:], index)
					assertComfortNoise(t, reference, "upper_im", upper[last].im[:], index)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
