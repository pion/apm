// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package fft

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	for _, n := range []int{16, 64, 256, 1024} {
		p := New(n)
		rng := rand.New(rand.NewPCG(uint64(n), 7)) //nolint:gosec // Deterministic test signal.
		in := make([]float32, n)
		for i := range in {
			in[i] = float32(rng.NormFloat64())
		}
		re := make([]float32, n/2+1)
		im := make([]float32, n/2+1)
		out := make([]float32, n)
		p.Forward(in, re, im)
		p.Inverse(re, im, out)
		require.InDeltaSlice(t, in, out, 1e-4, "n=%d", n)
	}
}

func TestForward_KnownTone(t *testing.T) {
	n := 256
	p := New(n)
	in := make([]float32, n)
	const bin = 5
	for i := range in {
		in[i] = float32(math.Cos(2 * math.Pi * float64(bin*i) / float64(n)))
	}
	re := make([]float32, n/2+1)
	im := make([]float32, n/2+1)
	p.Forward(in, re, im)
	// Energy should be concentrated at bin `bin`.
	var peakBin int
	peak := float32(-1)
	for k := range re {
		mag := re[k]*re[k] + im[k]*im[k]
		if mag > peak {
			peak = mag
			peakBin = k
		}
	}
	assert.Equal(t, bin, peakBin)
}

func TestForward_ZeroAlloc(t *testing.T) {
	p := New(256)
	in := make([]float32, 256)
	re := make([]float32, 129)
	im := make([]float32, 129)
	assert.Zero(t, testing.AllocsPerRun(100, func() { p.Forward(in, re, im) }))
	assert.Zero(t, testing.AllocsPerRun(100, func() { p.Inverse(re, im, in) }))
}

func BenchmarkForward256(b *testing.B) {
	p := New(256)
	in := make([]float32, 256)
	re := make([]float32, 129)
	im := make([]float32, 129)
	for i := range in {
		in[i] = float32(math.Sin(float64(i) * 0.1))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Forward(in, re, im)
	}
}
