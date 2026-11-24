// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ns

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceDelta bounds the difference to upstream on the int16 scale.
const referenceDelta = 0.05

// referenceFrame is one output band of testdata/upstream.txt. Each line
// reads "rate channels level frame channel band: samples".
type referenceFrame struct {
	rate, channels, level int
	frame, channel, band  int
	samples               []float32
}

func loadReference(t *testing.T) []referenceFrame {
	t.Helper()

	var frames []referenceFrame
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		ref := referenceFrame{samples: entry.Samples}
		_, err := fmt.Sscan(entry.Key, &ref.rate, &ref.channels, &ref.level, &ref.frame, &ref.channel, &ref.band)
		require.NoError(t, err)
		require.Len(t, ref.samples, nsFrameSize)
		frames = append(frames, ref)
	}

	return frames
}

// referenceSignal generates the input the upstream reference was produced
// with: uniform noise in every band, plus a 200 Hz triangle wave in the
// lowest band during alternating 250 ms periods. bands is indexed
// [band][channel][sample].
type referenceSignal struct {
	seed uint32
}

func (s *referenceSignal) fill(frame int, bands [][][]float32) {
	speech := (frame/25)%2 == 1
	for channel := range bands[0] {
		for band := range bands {
			for i := range nsFrameSize {
				s.seed = s.seed*1664525 + 1013904223
				value := int(s.seed>>16)%1001 - 500
				if speech && band == 0 {
					phase := (frame*nsFrameSize + i + 7*channel) % 80
					value += min(phase, 80-phase)*400 - 8000
				}
				if band > 0 {
					value /= 4
				}
				bands[band][channel][i] = float32(value)
			}
		}
	}
}

// TestSuppressor_MatchesUpstream compares the output with the output of
// the upstream NoiseSuppressor at the pinned commit for the same input.
func TestSuppressor_MatchesUpstream(t *testing.T) {
	type stream struct{ rate, channels, level int }
	want := map[stream]map[int][]referenceFrame{}
	for _, ref := range loadReference(t) {
		key := stream{ref.rate, ref.channels, ref.level}
		if want[key] == nil {
			want[key] = map[int][]referenceFrame{}
		}
		want[key][ref.frame] = append(want[key][ref.frame], ref)
	}
	require.NotEmpty(t, want)

	for key, frames := range want {
		name := fmt.Sprintf("%dHz_%dch_level%d", key.rate, key.channels, key.level)
		t.Run(name, func(t *testing.T) {
			suppressor, err := NewSuppressor(Config{TargetLevel: SuppressionLevel(key.level)}, key.rate, key.channels)
			require.NoError(t, err)

			bands := make([][][]float32, key.rate/16000)
			for band := range bands {
				bands[band] = make([][]float32, key.channels)
				for channel := range bands[band] {
					bands[band][channel] = make([]float32, nsFrameSize)
				}
			}
			lastFrame := 0
			for frame := range frames {
				lastFrame = max(lastFrame, frame)
			}

			signal := referenceSignal{seed: 1}
			for frame := 0; frame <= lastFrame; frame++ {
				signal.fill(frame, bands)
				require.NoError(t, suppressor.Analyze(bands[0]))
				require.NoError(t, suppressor.Process(bands[0], bands[1:]...))
				for _, ref := range frames[frame] {
					assert.InDeltaSlice(t, ref.samples, bands[ref.band][ref.channel], referenceDelta,
						"frame %d, channel %d, band %d", frame, ref.channel, ref.band)
				}
			}
		})
	}
}
