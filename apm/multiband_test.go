// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessor_SplitBand runs the full pipeline at 32 and 48 kHz and
// verifies the inner pipeline executes without errors.
func TestProcessor_SplitBand(t *testing.T) {
	cfg32 := DefaultConfig(Rate32k, 1)
	cfg32.HPF.Enabled = true
	cfg32.NS = NSConfig{Enabled: true, Level: NSModerate}

	cfg48 := DefaultConfig(Rate48k, 1)
	cfg48.HPF.Enabled = true
	cfg48.NS = NSConfig{Enabled: true, Level: NSModerate}
	cfg48.AGC = AGCConfig{Enabled: true, CompressionGain: 6}

	for seed, cfg := range []Config{cfg32, cfg48} {
		proc, err := New(cfg)
		require.NoError(t, err)
		rng := rand.New(rand.NewPCG(uint64(seed+1), uint64(seed+1))) //nolint:gosec // Deterministic test signal.
		for i := range 200 {
			f := NewFrame(cfg.SampleRate, 1)
			for j := range f.Data[0] {
				f.Data[0][j] = float32(rng.NormFloat64() * 0.1)
			}
			require.NoError(t, proc.ProcessStream(f), "rate %d frame %d", cfg.SampleRate, i)
		}
	}
}

// TestProcessor_48kHz_AEC drives the AEC at 48 kHz with a band-limited
// far-end signal whose energy sits in band 0 (below 8 kHz). The single-band
// AEC in band 0 should cancel most of the echo, which verifies the
// multi-band wiring (split, process band 0, synthesize) end to end.
// Full-band echo handling needs upper-band gain plumbing.
func TestProcessor_48kHz_AEC(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	const frames = 600
	const delay = 240
	total := frames * Rate48k.SamplesPerFrame()

	far := bandLimitedNoise(total)
	mic := make([]float32, total)
	for i := delay; i < total; i++ {
		mic[i] = 0.5 * far[i-delay]
	}
	out := runEchoPath(t, proc, far, mic)

	tailStart := total - int(Rate48k)
	var pm, pr float64
	for i := tailStart; i < total; i++ {
		pm += float64(mic[i]) * float64(mic[i])
		pr += float64(out[i]) * float64(out[i])
	}
	require.Positive(t, pr, "output had zero power")
	erleDB := 10 * math.Log10(pm/pr)
	t.Logf("48 kHz AEC steady-state ERLE = %.1f dB (band-limited echo)", erleDB)
	assert.GreaterOrEqual(t, erleDB, 3.0)
}

// bandLimitedNoise returns noise low-passed by an 8-tap moving average so
// its energy sits mostly below 8 kHz at 48 kHz.
func bandLimitedNoise(n int) []float32 {
	rng := rand.New(rand.NewPCG(3, 3)) //nolint:gosec // Deterministic test signal.
	raw := make([]float32, n+8)
	for i := range raw {
		raw[i] = float32(rng.NormFloat64() * 0.1)
	}
	out := make([]float32, n)
	for i := range n {
		v := float32(0)
		for k := range 8 {
			v += raw[i+k]
		}
		out[i] = v / 8
	}

	return out
}

// runEchoPath feeds far and mic through proc frame by frame at 48 kHz and
// returns the processed capture signal.
func runEchoPath(t *testing.T, proc *Processor, far, mic []float32) []float32 {
	t.Helper()
	spf := Rate48k.SamplesPerFrame()
	out := make([]float32, len(mic))
	for off := 0; off+spf <= len(mic); off += spf {
		ff := NewFrame(Rate48k, 1)
		nf := NewFrame(Rate48k, 1)
		copy(ff.Data[0], far[off:off+spf])
		copy(nf.Data[0], mic[off:off+spf])
		require.NoError(t, proc.ProcessReverseStream(ff))
		require.NoError(t, proc.ProcessStream(nf))
		copy(out[off:], nf.Data[0])
	}

	return out
}
