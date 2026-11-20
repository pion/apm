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
	_, err = NewSuppressor(DefaultConfig(), 48000, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate, "multi-band not yet supported")
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
