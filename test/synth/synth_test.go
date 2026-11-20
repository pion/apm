// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package synth

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRng() *rand.Rand {
	return rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Deterministic test signal.
}

func TestSine_RMSIs1OverSqrt2(t *testing.T) {
	s := Sine(48000, 48000, 1000, 1.0)
	assert.InDelta(t, 1.0/math.Sqrt(2), rmsFloat32(s), 0.01)
}

func TestSweep_StartsAtF0(t *testing.T) {
	s := Sweep(48000, 48000, 100, 10000, 1.0)
	for i := range 100 {
		require.False(t, math.IsNaN(float64(s[i])), "NaN at i=%d", i)
	}
}

func TestWhiteNoise_Stats(t *testing.T) {
	w := WhiteNoise(48000, newRng())
	mean, variance := stats(w)
	assert.InDelta(t, 0, mean, 0.05)
	assert.InDelta(t, 1, variance, 0.1)
}

func TestImpulse(t *testing.T) {
	want := make([]float32, 100)
	want[17] = 1
	assert.Equal(t, want, Impulse(100, 17))
}

func TestConvolve_IdentityWithDelta(t *testing.T) {
	x := []float32{1, 2, 3, 4}
	assert.Equal(t, x, Convolve(x, []float32{1}))
}

func TestConvolve_ShiftWithDelayedDelta(t *testing.T) {
	y := Convolve([]float32{1, 2, 3}, []float32{0, 0, 1})
	assert.Equal(t, []float32{0, 0, 1, 2, 3}, y)
}

func TestMixAtSNR_HitsTarget(t *testing.T) {
	rng := newRng()
	clean := Sine(48000, 48000, 1000, 0.5)
	noise := WhiteNoise(48000, rng)
	for _, target := range []float64{-10, 0, 10, 20} {
		mix, err := MixAtSNR(clean, noise, target)
		require.NoError(t, err)
		require.Len(t, mix, len(clean))
		assert.Positive(t, rmsFloat32(mix))
	}
}

func TestMixAtSNR_Errors(t *testing.T) {
	_, err := MixAtSNR(nil, nil, 0)
	require.ErrorIs(t, err, errEmptyInput)
	_, err = MixAtSNR(make([]float32, 4), make([]float32, 4), 0)
	require.ErrorIs(t, err, errZeroPower)
}

func TestLevelDBFS_KnownInputs(t *testing.T) {
	// Full-scale sine: RMS = 1/sqrt(2), about -3.01 dBFS.
	assert.InDelta(t, -3.01, LevelDBFS(Sine(48000, 48000, 1000, 1.0)), 0.05)
	// Half-scale sine: about -9.03 dBFS.
	assert.InDelta(t, -9.03, LevelDBFS(Sine(48000, 48000, 1000, 0.5)), 0.05)
}

func TestScaleToDBFS(t *testing.T) {
	s := Sine(48000, 48000, 1000, 1.0)
	ScaleToDBFS(s, -20)
	assert.InDelta(t, -20, LevelDBFS(s), 0.1)
}

func rmsFloat32(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}

	return math.Sqrt(s / float64(len(x)))
}

func stats(x []float32) (mean, variance float64) {
	for _, v := range x {
		mean += float64(v)
	}
	mean /= float64(len(x))
	for _, v := range x {
		d := float64(v) - mean
		variance += d * d
	}
	variance /= float64(len(x))

	return mean, variance
}
