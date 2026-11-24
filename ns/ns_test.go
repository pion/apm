// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSuppressor_RejectsBadRate(t *testing.T) {
	_, err := NewSuppressor(DefaultConfig(), 8000, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
	_, err = NewSuppressor(DefaultConfig(), 44100, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
}

func TestSuppressor_RejectsWrongBandCount(t *testing.T) {
	frame := func() [][]float32 { return [][]float32{make([]float32, nsFrameSize)} }

	sup, err := NewSuppressor(DefaultConfig(), 16000, 1)
	require.NoError(t, err)
	assert.ErrorIs(t, sup.Process(frame(), frame()), ErrInvalidBandCount)

	sup, err = NewSuppressor(DefaultConfig(), 48000, 1)
	require.NoError(t, err)
	assert.ErrorIs(t, sup.Process(frame()), ErrInvalidBandCount)
	assert.ErrorIs(t, sup.Process(frame(), frame(), [][]float32{make([]float32, 80)}), ErrInvalidFrameSize)
	assert.NoError(t, sup.Process(frame(), frame(), frame()))
}

// The upper bands come out delayed by the overlap of the filter bank.
func TestDelaySignal(t *testing.T) {
	var memory [overlapSize]float32
	var delayed [nsFrameSize]float32
	input := make([]float32, 3*nsFrameSize)
	for i := range input {
		input[i] = float32(i + 1)
	}

	var output []float32
	for start := 0; start < len(input); start += nsFrameSize {
		delaySignal(input[start:start+nsFrameSize], &memory, &delayed)
		output = append(output, delayed[:]...)
	}

	assert.Equal(t, make([]float32, overlapSize), output[:overlapSize])
	assert.Equal(t, input[:len(input)-overlapSize], output[overlapSize:])
}

func TestSuppressor_FrameLength(t *testing.T) {
	sup, err := NewSuppressor(DefaultConfig(), 16000, 1)
	require.NoError(t, err)
	assert.ErrorIs(t, sup.Process([][]float32{make([]float32, 100)}), ErrInvalidFrameSize)
	assert.ErrorIs(t, sup.Analyze([][]float32{make([]float32, 100)}), ErrInvalidFrameSize)
}

func TestSuppressor_NoiseReduction(t *testing.T) {
	sup, err := NewSuppressor(Config{TargetLevel: Level18dB}, 16000, 1)
	require.NoError(t, err)
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Deterministic test signal.

	const frames = 400
	noise := make([]float32, frames*nsFrameSize)
	for i := range noise {
		noise[i] = float32(rng.NormFloat64()) * 100
	}

	inPower := powerOf(noise)

	// Train and process: give NS time to learn the noise model.
	out := make([]float32, frames*nsFrameSize)
	for f := range frames {
		frame := append([]float32(nil), noise[f*nsFrameSize:(f+1)*nsFrameSize]...)
		require.NoError(t, sup.Analyze([][]float32{frame}))
		require.NoError(t, sup.Process([][]float32{frame}))
		copy(out[f*nsFrameSize:], frame)
	}
	// Examine steady-state output (skip first 200 frames of adaptation).
	steady := out[200*nsFrameSize:]
	outPower := powerOf(steady)
	require.Positive(t, inPower)
	require.Positive(t, outPower)
	atten := 10 * math.Log10(outPower/inPower)
	assert.Less(t, atten, -3.0, "noise should be attenuated by more than 3 dB")
	t.Logf("NS attenuation on pure noise (steady): %.1f dB", atten)
}

func TestSuppressor_PreservesAmplitudeModulatedSpeech(t *testing.T) {
	// A stationary tone looks like noise to NS by design. Speech-like
	// signals have an envelope; this test uses an AM-modulated multi-tone
	// (closer to voiced speech) and asserts NS doesn't gut it.
	sup, err := NewSuppressor(Config{TargetLevel: Level12dB}, 16000, 1)
	require.NoError(t, err)
	const frames = 400
	const rate = 16000
	signal := make([]float32, frames*nsFrameSize)
	for i := range signal {
		t := float64(i) / float64(rate)
		// Amplitude-modulated speech-like signal: three harmonics at 200/400/600 Hz
		// modulated by a 5 Hz envelope.
		env := 0.5 + 0.5*math.Sin(2*math.Pi*5*t)
		carrier := math.Sin(2*math.Pi*200*t) + 0.7*math.Sin(2*math.Pi*400*t) + 0.4*math.Sin(2*math.Pi*600*t)
		signal[i] = float32(env * carrier * 300)
	}

	out := make([]float32, frames*nsFrameSize)
	for f := range frames {
		frame := append([]float32(nil), signal[f*nsFrameSize:(f+1)*nsFrameSize]...)
		require.NoError(t, sup.Analyze([][]float32{frame}))
		require.NoError(t, sup.Process([][]float32{frame}))
		copy(out[f*nsFrameSize:], frame)
	}
	steady := out[200*nsFrameSize:]
	steadyIn := signal[200*nsFrameSize:]
	atten := 10 * math.Log10(powerOf(steady)/powerOf(steadyIn))
	t.Logf("AM-multi-tone attenuation (steady): %.1f dB", atten)
	assert.GreaterOrEqual(t, atten, -10.0, "speech-like signal over-attenuated")
}

func powerOf(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}

	return s / float64(len(x))
}

// populateIdenticalChannels mirrors PopulateInputFrameWithIdenticalChannels
// of noise_suppressor_unittest.cc. bands is indexed [band][channel][sample].
func populateIdenticalChannels(bands [][][]float32, frameIndex int) {
	for b, band := range bands {
		for _, channel := range band {
			for i := range channel {
				value := float32(frameIndex*nsFrameSize + i)
				channel[i] = 0
				if value > 0 {
					channel[i] = float32(5000*b) + value
				}
			}
		}
	}
}

// Port of NoiseSuppressor.IdenticalChannelEffects: the same effect is
// applied to all channels. Upstream loops over the suppression levels
// without setting them, so the test runs once with the default config.
func TestSuppressor_IdenticalChannelEffects(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		for _, numChannels := range []int{1, 4, 8} {
			sup, err := NewSuppressor(DefaultConfig(), rate, numChannels)
			require.NoError(t, err)
			bands := make([][][]float32, rate/16000)
			for b := range bands {
				bands[b] = make([][]float32, numChannels)
				for ch := range bands[b] {
					bands[b][ch] = make([]float32, nsFrameSize)
				}
			}

			for frameIndex := range 1000 {
				populateIdenticalChannels(bands, frameIndex)
				require.NoError(t, sup.Analyze(bands[0]))
				require.NoError(t, sup.Process(bands[0], bands[1:]...))
				for b, band := range bands {
					for ch := 1; ch < numChannels; ch++ {
						require.Equal(t, band[0], band[ch], "rate %d, %d channels, frame %d, band %d, channel %d",
							rate, numChannels, frameIndex, b, ch)
					}
				}
			}
		}
	}
}
