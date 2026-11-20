// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// filled returns kBlockSize samples of value.
func filled(value float32) []float32 {
	samples := make([]float32, kBlockSize)
	fill(samples, value)

	return samples
}

// fillAdaptiveModeInput sets every channel of a frame to a constant, the
// strongest channel scaled up.
func fillAdaptiveModeInput(input *block, frame, strongestCh int) {
	const (
		channelOffset          = 100
		maxChannelsToTest      = 8
		strongestSignalScaling = maxChannelsToTest * channelOffset * 100
	)
	for channel := range input.numChannels {
		scaling := float32(1)
		if channel == strongestCh {
			scaling = strongestSignalScaling
		}
		fill(input.view(0, channel), float32(frame+channel*channelOffset)*scaling)
	}
}

// runAdaptiveMode ports the body of AlignmentMixer.GeneralAdaptiveMode
// for one combination of parameters.
func runAdaptiveMode(
	t *testing.T, initialSilence, hugeActivityThreshold, preferFirstTwoChannels bool, numChannels, strongestCh int,
) {
	t.Helper()
	excitationLimit := float32(0.001)
	if hugeActivityThreshold {
		excitationLimit = 1000000000
	}
	mixer := newAlignmentMixer(numChannels, alignmentMixing{
		adaptiveSelection:      true,
		activityPowerThreshold: excitationLimit,
		preferFirstTwoChannels: preferFirstTwoChannels,
	})
	input := newBlock(1, numChannels)
	out := make([]float32, kBlockSize)

	if initialSilence {
		for range 10 * kNumBlocksPerSecond {
			mixer.produceOutput(input, out)
		}
	}

	for frame := range 2 * kNumBlocksPerSecond {
		fillAdaptiveModeInput(input, frame, strongestCh)
		fill(out, -1)
		mixer.produceOutput(input, out)

		if frame <= kNumBlocksPerSecond {
			continue
		}
		if !preferFirstTwoChannels || hugeActivityThreshold {
			assert.Equal(t, filled(input.view(0, strongestCh)[0]), out, "frame %d", frame)
		} else {
			leftOrRightChosen := slices.Equal(out, input.view(0, 0)) || slices.Equal(out, input.view(0, 1))
			assert.True(t, leftOrRightChosen, "frame %d", frame)
		}
	}
}

// Port of AlignmentMixer.GeneralAdaptiveMode.
func TestAlignmentMixer_GeneralAdaptiveMode(t *testing.T) {
	for _, initialSilence := range []bool{false, true} {
		for _, hugeActivityThreshold := range []bool{false, true} {
			for _, preferFirstTwoChannels := range []bool{false, true} {
				for numChannels := 2; numChannels < 8; numChannels++ {
					for strongestCh := range numChannels {
						runAdaptiveMode(t, initialSilence, hugeActivityThreshold, preferFirstTwoChannels,
							numChannels, strongestCh)
					}
				}
			}
		}
	}
}

// Port of AlignmentMixer.DownmixMode.
func TestAlignmentMixer_DownmixMode(t *testing.T) {
	for numChannels := 1; numChannels < 8; numChannels++ {
		mixer := newAlignmentMixer(numChannels, alignmentMixing{downmix: true, activityPowerThreshold: 1})
		x := newBlock(1, numChannels)
		out := make([]float32, kBlockSize)

		for frame := range 10 {
			var expectedMixedValue float32
			for channel := range numChannels {
				fill(x.view(0, channel), float32(frame+channel))
				expectedMixedValue += float32(frame + channel)
			}
			expectedMixedValue *= 1 / float32(numChannels)

			fill(out, -1)
			mixer.produceOutput(x, out)
			assert.Equal(t, filled(expectedMixedValue), out, "channels %d, frame %d", numChannels, frame)
		}
	}
}

// Port of AlignmentMixer.FixedMode.
func TestAlignmentMixer_FixedMode(t *testing.T) {
	for numChannels := 1; numChannels < 8; numChannels++ {
		mixer := newAlignmentMixer(numChannels, alignmentMixing{activityPowerThreshold: 1})
		x := newBlock(1, numChannels)
		out := make([]float32, kBlockSize)

		for frame := range 10 {
			for channel := range numChannels {
				fill(x.view(0, channel), float32(frame+channel))
			}
			fill(out, -1)
			mixer.produceOutput(x, out)
			assert.Equal(t, filled(x.view(0, 0)[0]), out, "channels %d, frame %d", numChannels, frame)
		}
	}
}
