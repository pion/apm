// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package hpf

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilter_MatchesUpstream compares the output with the output of the
// upstream HighPassFilter at the pinned commit, bit for bit, fed the same generated
// stereo noise with a DC offset. Each line of the reference file is
// keyed by "rate frame channel".
func TestFilter_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	const numChannels = 2
	compared := 0
	for _, rate := range []int{16000, 32000, 48000} {
		filter, err := New(rate, numChannels)
		require.NoError(t, err)

		audio := make([][]float32, numChannels)
		for channel := range audio {
			audio[channel] = make([]float32, rate/100)
		}
		seed := uint32(1)
		for frame := range 20 {
			for _, samples := range audio {
				for i := range samples {
					seed = seed*1664525 + 1013904223
					samples[i] = float32(int(seed>>16)-32768) + 1000
				}
			}
			require.NoError(t, filter.Process(audio))

			for channel, samples := range audio {
				if want, ok := reference[fmt.Sprintf("%d %d %d", rate, frame, channel)]; ok {
					assert.Equal(t, want, samples, "rate %d, frame %d, channel %d", rate, frame, channel)
					compared++
				}
			}
		}
	}
	assert.Equal(t, len(reference), compared)
}
