// SPDX-FileCopyrightText: 2025 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of PostFilterCreateTest.CreateOnlyFor48k.
func TestPostFilter_CreateOnlyFor48k(t *testing.T) {
	for _, sampleRateHz := range []int{8000, 16000, 24000, 32000, 44100, 48000} {
		filter := newPostFilterIfNeeded(sampleRateHz, 2)
		assert.Equal(t, sampleRateHz == 48000, filter != nil, "rate %d", sampleRateHz)
	}
}

// postFilterTonePowerRatios returns, for 20 frames of a tone at 48 kHz,
// the power of the output of the filter over the power of its input. A
// first frame primes the filter.
func postFilterTonePowerRatios(t *testing.T, toneFrequency float64) []float64 {
	t.Helper()
	const sampleRateHz = 48000
	filter := newPostFilterIfNeeded(sampleRateHz, 1)
	require.NotNil(t, filter)

	phaseIncrement := toneFrequency * 2 * math.Pi / sampleRateHz
	phase := 0.0
	audio := [][]float32{make([]float32, sampleRateHz/100)}
	power := func() float64 {
		var energy float64
		for _, sample := range audio[0] {
			energy += float64(sample * sample)
		}

		return energy / float64(len(audio[0]))
	}
	var ratios []float64
	for frame := range 21 {
		for k := range audio[0] {
			phase += phaseIncrement
			audio[0][k] = float32(math.Sin(phase))
		}
		inputPower := power()
		filter.process(audio)
		if frame > 0 {
			ratios = append(ratios, power()/inputPower)
		}
	}

	return ratios
}

// Port of PostFilterTest.Tone19p8kHzSignalAttenuation48k.
func TestPostFilter_Tone19p8kHzSignalAttenuation48k(t *testing.T) {
	for _, ratio := range postFilterTonePowerRatios(t, 19800) {
		assert.LessOrEqual(t, 10*math.Log10(ratio), -20.0)
	}
}

// Port of PostFilterTest.Tone17kHzSignalNoAttenuation48k.
func TestPostFilter_Tone17kHzSignalNoAttenuation48k(t *testing.T) {
	for _, ratio := range postFilterTonePowerRatios(t, 16800) {
		assert.InDelta(t, 0, 10*math.Log10(ratio), 0.5)
	}
}
