// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RejectsBadShape(t *testing.T) {
	_, err := New(Config{}, 16000, 0)
	assert.ErrorIs(t, err, ErrInvalidChannels)
	_, err = New(Config{}, 11025, 1)
	assert.ErrorIs(t, err, ErrInvalidSampleRate)
}

func TestController_LimiterPreventsOvershoot(t *testing.T) {
	c, err := New(Config{FixedDigitalGainDB: 12}, 16000, 1)
	require.NoError(t, err)
	rng := rand.New(rand.NewPCG(1, 1)) //nolint:gosec // Deterministic test signal.
	const frames = 200
	for frame := range frames {
		ch := make([]float32, 160)
		for i := range ch {
			ch[i] = float32(rng.NormFloat64() * 20000) // already loud
		}
		require.NoError(t, c.Process([][]float32{ch}))
		var peak float32
		for _, v := range ch {
			a := v
			if a < 0 {
				a = -a
			}
			if a > peak {
				peak = a
			}
		}
		assert.LessOrEqual(t, peak, float32(kMaxFloatS16Value+1), "frame %d", frame)
	}
}

func TestController_QuietSignalRaisedByFixedGain(t *testing.T) {
	c, err := New(Config{FixedDigitalGainDB: 12}, 16000, 1)
	require.NoError(t, err)
	const frames = 100
	var rmsIn, rmsOut float64
	for f := range frames {
		in := make([]float32, 160)
		for i := range in {
			in[i] = float32(math.Sin(2*math.Pi*1000*float64(f*160+i)/16000)) * 500 // quiet
		}
		ref := append([]float32(nil), in...)
		require.NoError(t, c.Process([][]float32{in}))
		// Skip warmup ramp.
		if f < 10 {
			continue
		}
		for i := range in {
			rmsIn += float64(ref[i]) * float64(ref[i])
			rmsOut += float64(in[i]) * float64(in[i])
		}
	}
	assert.Greater(t, rmsOut, rmsIn, "fixed +12 dB gain should raise level")
}

func TestController_ZeroAllocSteadyState(t *testing.T) {
	c, _ := New(Config{FixedDigitalGainDB: 6}, 16000, 1)
	ch := make([]float32, 160)
	for i := range ch {
		ch[i] = 1000
	}
	_ = c.Process([][]float32{ch})
	allocs := testing.AllocsPerRun(100, func() {
		_ = c.Process([][]float32{ch})
	})
	assert.Zero(t, allocs)
}
