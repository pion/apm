// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package resampler

import (
	"fmt"
	"math"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceDelta bounds the difference to upstream for samples in
// [-1, 1]. It allows for the last-bit differences between math libraries
// in the kernel and for fused multiply-add on some architectures.
const referenceDelta = 1e-5

// TestPushSincResampler_MatchesUpstream compares the output with the
// output of the upstream PushSincResampler at the pinned commit, built
// with the plain C convolution and fed the same generated noise in 10 ms
// chunks. Each line of the reference file is keyed by
// "source_rate destination_rate chunk".
func TestPushSincResampler_MatchesUpstream(t *testing.T) {
	type conversion struct{ sourceRate, destinationRate int }
	want := map[conversion]map[int][]float32{}
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		var key conversion
		var chunk int
		_, err := fmt.Sscan(entry.Key, &key.sourceRate, &key.destinationRate, &chunk)
		require.NoError(t, err)
		if want[key] == nil {
			want[key] = map[int][]float32{}
		}
		want[key][chunk] = entry.Samples
	}

	for key, chunks := range want {
		t.Run(fmt.Sprintf("%d_to_%d", key.sourceRate, key.destinationRate), func(t *testing.T) {
			resampler, err := NewPushSincResampler(key.sourceRate/100, key.destinationRate/100)
			require.NoError(t, err)

			source := make([]float32, key.sourceRate/100)
			destination := make([]float32, key.destinationRate/100)
			seed := uint32(1)
			for chunk := range 10 {
				for i := range source {
					seed = seed*1664525 + 1013904223
					source[i] = float32(int(seed>>16)-32768) / 32768
				}
				require.NoError(t, resampler.Resample(source, destination))
				if samples, ok := chunks[chunk]; ok {
					assert.InDeltaSlice(t, samples, destination, referenceDelta, "chunk %d", chunk)
				}
			}
		})
	}
}

// A tone well inside the pass band keeps its level through a rate change
// and back, delayed by half the kernel on each side.
func TestPushSincResampler_RoundTripPreservesTone(t *testing.T) {
	const (
		lowFrames  = 80
		highFrames = 160
		toneHz     = 1000.0
	)
	up, err := NewPushSincResampler(lowFrames, highFrames)
	require.NoError(t, err)
	down, err := NewPushSincResampler(highFrames, lowFrames)
	require.NoError(t, err)

	source := make([]float32, lowFrames)
	high := make([]float32, highFrames)
	output := make([]float32, lowFrames)
	var inEnergy, outEnergy float64
	for chunk := range 50 {
		for i := range source {
			source[i] = float32(0.5 * math.Sin(2*math.Pi*toneHz*float64(chunk*lowFrames+i)/8000))
		}
		require.NoError(t, up.Resample(source, high))
		require.NoError(t, down.Resample(high, output))
		if chunk < 5 {
			continue
		}
		for i := range source {
			inEnergy += float64(source[i]) * float64(source[i])
			outEnergy += float64(output[i]) * float64(output[i])
		}
	}
	assert.InDelta(t, 0, 10*math.Log10(outEnergy/inEnergy), 0.1)
}

func TestPushSincResampler_RejectsBadShapes(t *testing.T) {
	_, err := NewPushSincResampler(48, 96)
	require.ErrorIs(t, err, ErrFrameShape)
	_, err = NewPushSincResampler(80, 0)
	require.ErrorIs(t, err, ErrFrameShape)

	resampler, err := NewPushSincResampler(80, 160)
	require.NoError(t, err)
	assert.ErrorIs(t, resampler.Resample(make([]float32, 79), make([]float32, 160)), ErrFrameShape)
	assert.ErrorIs(t, resampler.Resample(make([]float32, 80), make([]float32, 161)), ErrFrameShape)
}

func TestPushSincResampler_ZeroAlloc(t *testing.T) {
	resampler, err := NewPushSincResampler(80, 160)
	require.NoError(t, err)
	source := make([]float32, 80)
	destination := make([]float32, 160)
	require.NoError(t, resampler.Resample(source, destination))

	assert.Zero(t, testing.AllocsPerRun(100, func() {
		_ = resampler.Resample(source, destination)
	}))
}
