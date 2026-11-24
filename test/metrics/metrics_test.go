// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRate = 16000

func sine(n int, f, amp float64) []float32 {
	out := make([]float32, n)
	w := 2 * math.Pi * f / testRate
	for i := range n {
		out[i] = float32(amp * math.Sin(w*float64(i)))
	}

	return out
}

func white(n int, src *rand.Rand) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(src.NormFloat64() * 0.1)
	}

	return out
}

func TestSNR_IdenticalIsInfinite(t *testing.T) {
	x := sine(16000, 1000, 0.5)
	assert.True(t, math.IsInf(SNR(x, x), +1))
}

func TestSNR_HalvedReferenceMatchesExpected(t *testing.T) {
	x := sine(16000, 1000, 1.0)
	y := make([]float32, len(x))
	for i := range x {
		// Error is 0.5x, so error power is 0.25 of signal power: 6.02 dB.
		y[i] = x[i] * 0.5
	}
	assert.InDelta(t, 6.02, SNR(x, y), 0.05)
}

func TestSegmentalSNR_Sensible(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Deterministic test signal.
	ref := sine(16000, 1000, 0.5)
	test := append([]float32(nil), ref...)
	noise := white(16000, rng)
	for i := range test {
		test[i] += noise[i] * 0.05
	}
	got := SegmentalSNR(ref, test, 160, -50)
	assert.False(t, math.IsNaN(got))
	assert.Positive(t, got)
}

func TestERLE_PerfectCancellation(t *testing.T) {
	mic := sine(16000, 1000, 0.5)
	residual := make([]float32, len(mic))
	assert.GreaterOrEqual(t, ERLE(mic, residual, 160, -50), 60.0)
}

func TestERLE_NoCancellation(t *testing.T) {
	mic := sine(16000, 1000, 0.5)
	residual := append([]float32(nil), mic...)
	assert.InDelta(t, 0, ERLE(mic, residual, 160, -50), 0.5)
}

func TestLSD_IdenticalIsZero(t *testing.T) {
	x := sine(8192, 1000, 0.5)
	assert.LessOrEqual(t, LogSpectralDistance(x, x, testRate, 512, 256), 0.01)
}

func TestLSD_DifferentFreqIsPositive(t *testing.T) {
	x := sine(8192, 1000, 0.5)
	y := sine(8192, 2000, 0.5)
	assert.Greater(t, LogSpectralDistance(x, y, testRate, 512, 256), 1.0)
}

func TestCepstral_IdenticalIsZero(t *testing.T) {
	x := sine(8192, 1000, 0.5)
	assert.LessOrEqual(t, CepstralDistance(x, x, testRate, 512, 256, 13), 0.01)
}

func TestAttackTime_StepResponds(t *testing.T) {
	dur := testRate // 1 s
	step := dur / 2
	out := make([]float32, dur)
	for i := range step {
		out[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/testRate) * 0.05)
	}
	for i := step; i < dur; i++ {
		out[i] = float32(math.Sin(2*math.Pi*1000*float64(i)/testRate) * 0.5)
	}
	at := AttackTime(out, testRate, step, 80, 3, 1.0)
	require.False(t, math.IsNaN(at), "attack should converge")
	assert.GreaterOrEqual(t, at, 0.0)
	assert.LessOrEqual(t, at, 0.1)
}

func TestPeakLevelDBFS(t *testing.T) {
	x := []float32{0.5, -0.5, 0.25}
	assert.InDelta(t, 20*math.Log10(0.5), PeakLevelDBFS(x), 1e-6)
}

func TestTHDPlusN_CleanSineIsLow(t *testing.T) {
	x := sine(8192, 1000, 0.5)
	assert.LessOrEqual(t, THDPlusN(x, testRate, 1000), -40.0)
}

func TestTHDPlusN_NoiseRaisesIt(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // Deterministic test signal.
	x := sine(8192, 1000, 0.5)
	n := white(8192, rng)
	for i := range x {
		x[i] += n[i]
	}
	assert.GreaterOrEqual(t, THDPlusN(x, testRate, 1000), -30.0)
}

func TestPEAQLite_Identical(t *testing.T) {
	x := sine(16384, 1000, 0.5)
	odg := PEAQLite(x, x, testRate)
	assert.GreaterOrEqual(t, odg, -0.1)
	assert.LessOrEqual(t, odg, 0.0)
}
