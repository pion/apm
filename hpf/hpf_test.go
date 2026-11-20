// SPDX-FileCopyrightText: 2012 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package hpf

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_UnsupportedRate(t *testing.T) {
	_, err := New(8000, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
	_, err = New(44100, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
}

func TestProcess_DCRejection(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		filter, err := New(rate, 1)
		require.NoError(t, err)
		n := rate / 4 // 250 ms: well past convergence
		in := make([]float32, n)
		for i := range in {
			in[i] = 1.0
		}
		require.NoError(t, filter.Process([][]float32{in}))
		// Examine the converged tail.
		var sum float64
		tail := in[n/2:]
		for _, v := range tail {
			sum += math.Abs(float64(v))
		}
		avg := sum / float64(len(tail))
		assert.LessOrEqual(t, avg, 1e-3, "%d Hz: DC residual", rate)
	}
}

func TestProcess_HighFreqPassband(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		filter, err := New(rate, 1)
		require.NoError(t, err)
		n := rate / 2
		freq := 1000.0
		in := make([]float32, n)
		ref := make([]float32, n)
		for i := range in {
			in[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(rate)))
			ref[i] = in[i]
		}
		require.NoError(t, filter.Process([][]float32{in}))
		var sIn, sOut float64
		for i := 1000; i < n; i++ {
			sIn += float64(ref[i]) * float64(ref[i])
			sOut += float64(in[i]) * float64(in[i])
		}
		ratio := sOut / sIn
		assert.InDelta(t, 1.0, ratio, 0.1, "%d Hz: passband ratio", rate)
	}
}

func TestProcess_LowFreqRejection(t *testing.T) {
	// 10 Hz tone should be attenuated by far more than 20 dB.
	rate := 16000
	filter, err := New(rate, 1)
	require.NoError(t, err)
	n := rate
	freq := 10.0
	in := make([]float32, n)
	ref := make([]float32, n)
	for i := range in {
		in[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(rate)))
		ref[i] = in[i]
	}
	require.NoError(t, filter.Process([][]float32{in}))
	var sIn, sOut float64
	for i := 2000; i < n; i++ {
		sIn += float64(ref[i]) * float64(ref[i])
		sOut += float64(in[i]) * float64(in[i])
	}
	if sOut == 0 {
		return
	}
	attenDB := 10 * math.Log10(sOut/sIn)
	assert.LessOrEqual(t, attenDB, -20.0, "10 Hz attenuation in dB")
}

func TestProcess_MultiChannelIndependent(t *testing.T) {
	filter, err := New(16000, 2)
	require.NoError(t, err)
	left := make([]float32, 160)
	right := make([]float32, 160)
	for i := range left {
		left[i] = 1.0 // DC on left
		right[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 16000))
	}
	rightRef := append([]float32(nil), right...)
	require.NoError(t, filter.Process([][]float32{left, right}))
	// Right channel should be largely preserved (1 kHz passband).
	var diff float64
	for i := range right {
		d := float64(right[i] - rightRef[i])
		diff += d * d
	}
	rmse := math.Sqrt(diff / float64(len(right)))
	assert.LessOrEqual(t, rmse, 0.3, "right-channel distortion rmse")
}

func TestProcess_ChannelCountMismatch(t *testing.T) {
	filter, err := New(16000, 2)
	require.NoError(t, err)
	assert.ErrorIs(t, filter.Process([][]float32{make([]float32, 160)}), ErrChannelCount)
}

func TestProcess_ZeroAlloc(t *testing.T) {
	filter, err := New(16000, 1)
	require.NoError(t, err)
	in := [][]float32{make([]float32, 160)}
	_ = filter.Process(in)
	assert.Zero(t, testing.AllocsPerRun(100, func() { _ = filter.Process(in) }))
}

func TestReset_ClearsState(t *testing.T) {
	filter, err := New(16000, 1)
	require.NoError(t, err)
	in := make([]float32, 160)
	for i := range in {
		in[i] = 1.0
	}
	_ = filter.Process([][]float32{in})
	filter.Reset()
	// Re-process DC; if reset worked, output starts large again
	// (transient before convergence) instead of being already-converged.
	in2 := make([]float32, 160)
	for i := range in2 {
		in2[i] = 1.0
	}
	_ = filter.Process([][]float32{in2})
	assert.GreaterOrEqual(t, math.Abs(float64(in2[0])), 0.1, "after Reset(), expected a large transient")
}
