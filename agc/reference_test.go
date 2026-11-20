// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package agc

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceDelta bounds the difference to upstream on the int16 scale.
const referenceDelta = 0.05

// referenceStream identifies one upstream run of testdata/upstream.txt.
type referenceStream struct {
	rate, channels int
	gainDB         float32
}

// loadReference returns the reference samples indexed by stream, frame
// and channel. Each line of the reference file is keyed by
// "rate channels gain_db frame channel".
func loadReference(t *testing.T) map[referenceStream]map[int]map[int][]float32 {
	t.Helper()

	reference := map[referenceStream]map[int]map[int][]float32{}
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		var key referenceStream
		var frame, channel int
		_, err := fmt.Sscan(entry.Key, &key.rate, &key.channels, &key.gainDB, &frame, &channel)
		require.NoError(t, err)
		if reference[key] == nil {
			reference[key] = map[int]map[int][]float32{}
		}
		if reference[key][frame] == nil {
			reference[key][frame] = map[int][]float32{}
		}
		reference[key][frame][channel] = entry.Samples
	}

	return reference
}

// fillReferenceSignal generates the input of the upstream runs: uniform
// noise that alternates between quiet and loud every 100 ms, so the
// limiter attacks and releases.
func fillReferenceSignal(seed *uint32, frame int, audio [][]float32) {
	amplitude := 2000
	if (frame/10)%2 == 1 {
		amplitude = 30000
	}
	for _, samples := range audio {
		for i := range samples {
			*seed = *seed*1664525 + 1013904223
			samples[i] = float32(int(*seed>>16)%(2*amplitude+1) - amplitude)
		}
	}
}

// TestController_MatchesUpstream compares the output with the output of
// the upstream GainApplier and Limiter at the pinned commit, chained as
// in GainController2::Process.
func TestController_MatchesUpstream(t *testing.T) {
	for key, frames := range loadReference(t) {
		t.Run(fmt.Sprintf("%dHz_%dch_%gdB", key.rate, key.channels, key.gainDB), func(t *testing.T) {
			controller, err := New(Config{FixedDigitalGainDB: key.gainDB}, key.rate, key.channels)
			require.NoError(t, err)

			audio := make([][]float32, key.channels)
			for channel := range audio {
				audio[channel] = make([]float32, key.rate/100)
			}
			seed := uint32(1)
			for frame := range 60 {
				fillReferenceSignal(&seed, frame, audio)
				require.NoError(t, controller.Process(audio))
				for channel, samples := range frames[frame] {
					assert.InDeltaSlice(t, samples, audio[channel], referenceDelta, "frame %d, channel %d", frame, channel)
				}
			}
		})
	}
}
