// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import (
	"math/rand/v2"
	"testing"

	"github.com/pion/apm/aec"
	"github.com/pion/apm/ns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessStream_RoundtripsSamples(t *testing.T) {
	proc, err := New(DefaultConfig(Rate16k, 1))
	require.NoError(t, err)
	f := NewFrame(Rate16k, 1)
	for i := range f.Data[0] {
		f.Data[0][i] = float32(i) / 160
	}
	want := append([]float32(nil), f.Data[0]...)

	require.NoError(t, proc.ProcessStream(f))
	// With all modules disabled, ProcessStream is a pass-through.
	assert.Equal(t, want, f.Data[0])
}

func TestFrame_Validate(t *testing.T) {
	tests := []struct {
		name    string
		frame   *Frame
		wantErr bool
	}{
		{"good 16k mono", NewFrame(Rate16k, 1), false},
		{"good 48k stereo", NewFrame(Rate48k, 2), false},
		{"bad rate", &Frame{SampleRate: 12345, Data: [][]float32{make([]float32, 160)}}, true},
		{"no channels", &Frame{SampleRate: Rate16k}, true},
		{"short channel", &Frame{SampleRate: Rate16k, Data: [][]float32{make([]float32, 100)}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSamplesPerFrame(t *testing.T) {
	cases := []struct {
		rate SampleRate
		want int
	}{
		{Rate16k, 160},
		{Rate32k, 320},
		{Rate48k, 480},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.rate.SamplesPerFrame(), "rate %d", c.rate)
	}
}

func TestProcessor_ZeroAllocSteadyState(t *testing.T) {
	proc, err := New(DefaultConfig(Rate16k, 1))
	require.NoError(t, err)
	f := NewFrame(Rate16k, 1)
	// Warm up: first call may allocate scratch.
	_ = proc.ProcessStream(f)
	allocs := testing.AllocsPerRun(1000, func() {
		_ = proc.ProcessStream(f)
	})
	assert.Zero(t, allocs, "ProcessStream allocates in steady state")
}

func TestProcessReverseStream_LeavesFrameUnchanged(t *testing.T) {
	for _, rate := range []SampleRate{Rate16k, Rate32k, Rate48k} {
		cfg := DefaultConfig(rate, 1)
		cfg.AEC.Enabled = true
		proc, err := New(cfg)
		require.NoError(t, err)

		far := NewFrame(rate, 1)
		for i := range far.Data[0] {
			far.Data[0][i] = float32(i%32)/32 - 0.5
		}
		want := append([]float32(nil), far.Data[0]...)

		require.NoError(t, proc.ProcessReverseStream(far))
		assert.Equal(t, want, far.Data[0], "rate %d", rate)
	}
}

func TestNew_RejectsUnsupportedSampleRate(t *testing.T) {
	for _, rate := range []SampleRate{0, 8000, 44100} {
		_, err := New(DefaultConfig(rate, 1))
		assert.ErrorIs(t, err, ErrInvalidSampleRate, "rate %d", rate)
	}
}

func TestNew_ValidatesReverseStream(t *testing.T) {
	cfg := DefaultConfig(Rate16k, 1)
	cfg.ReverseSampleRate = 8000
	_, err := New(cfg)
	assert.ErrorIs(t, err, ErrInvalidSampleRate)

	cfg = DefaultConfig(Rate16k, 1)
	cfg.ReverseNumChannels = -1
	_, err = New(cfg)
	assert.ErrorIs(t, err, ErrInvalidChannels)

	cfg = DefaultConfig(Rate16k, 1)
	cfg.ReverseNumChannels = 2
	_, err = New(cfg)
	assert.NoError(t, err, "channel counts may differ without echo cancellation")

	cfg.AEC.Enabled = true
	_, err = New(cfg)
	assert.ErrorIs(t, err, ErrInvalidChannels)
}

func TestProcessor_MixedRatesWithAEC(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.ReverseSampleRate = Rate16k
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	require.NoError(t, proc.ProcessReverseStream(NewFrame(Rate16k, 1)))
	require.NoError(t, proc.ProcessStream(NewFrame(Rate48k, 1)))
}

func TestProcessor_ZeroAllocAllModules(t *testing.T) {
	for _, rate := range []SampleRate{Rate16k, Rate32k, Rate48k} {
		cfg := DefaultConfig(rate, 2)
		cfg.HPF.Enabled = true
		cfg.AEC.Enabled = true
		cfg.NS.Enabled = true
		cfg.AGC.Enabled = true
		proc, err := New(cfg)
		require.NoError(t, err)

		far, near := NewFrame(rate, 2), NewFrame(rate, 2)
		process := func() {
			_ = proc.ProcessReverseStream(far)
			_ = proc.ProcessStream(near)
		}
		for range 10 {
			process()
		}
		assert.Zero(t, testing.AllocsPerRun(100, process), "rate %d", rate)
	}
}

// The capture path follows upstream: noise analysis, echo cancellation,
// then noise suppression.
func TestProcessor_AnalyzesNoiseBeforeEchoCancellation(t *testing.T) {
	cfg := DefaultConfig(Rate16k, 1)
	cfg.AEC.Enabled = true
	cfg.NS.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	canceller, err := aec.New(aec.DefaultConfig(), 16000, 1)
	require.NoError(t, err)
	suppressor, err := ns.NewSuppressor(ns.Config{TargetLevel: mapNSLevel(cfg.NS.Level)}, 16000, 1)
	require.NoError(t, err)

	rng := rand.New(rand.NewPCG(11, 0)) //nolint:gosec // Deterministic test signal.
	for i := range 50 {
		far, near := NewFrame(Rate16k, 1), NewFrame(Rate16k, 1)
		wantFar := [][]float32{make([]float32, 160)}
		want := [][]float32{make([]float32, 160)}
		for j := range 160 {
			far.Data[0][j] = float32(rng.NormFloat64() * 0.1)
			near.Data[0][j] = 0.5*far.Data[0][j] + float32(rng.NormFloat64()*0.01)
			wantFar[0][j] = far.Data[0][j] * apmScale
			want[0][j] = near.Data[0][j] * apmScale
		}
		require.NoError(t, canceller.AnalyzeRender(wantFar))
		require.NoError(t, suppressor.Analyze(want))
		require.NoError(t, canceller.ProcessCapture(want))
		require.NoError(t, suppressor.Process(want))
		scaleDown(want)

		require.NoError(t, proc.ProcessReverseStream(far))
		require.NoError(t, proc.ProcessStream(near))
		require.Equal(t, want[0], near.Data[0], "frame %d", i)
	}
}
