// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import (
	"testing"

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
		{Rate8k, 80},
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
