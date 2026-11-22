// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newSplitBandsFrame returns a frame of 160 samples per band that is
// indexed by channel, band and sample.
func newSplitBandsFrame(numChannels, numBands int) [][][]float32 {
	const frameLength = 160
	frame := make([][][]float32, numChannels)
	for ch := range frame {
		frame[ch] = make([][]float32, numBands)
		for band := range frame[ch] {
			frame[ch][band] = make([]float32, frameLength)
		}
	}

	return frame
}

// blockDelayBufferSampleValue ports SampleValue.
func blockDelayBufferSampleValue(sampleIndex int) float32 { return float32(sampleIndex % 32768) }

// runBlockDelayBufferTest runs the body of
// BlockDelayBufferTest.CorrectDelayApplied.
func runBlockDelayBufferTest(t *testing.T, delay, rate, numChannels int) {
	t.Helper()
	const (
		subbandFrameLength = 160
		numFramesToProcess = 20
	)
	numBands := numBandsForRate(rate)
	delayBuffer := newBlockDelayBuffer(numChannels, numBands, subbandFrameLength, delay)

	for frameIndex := range numFramesToProcess {
		// Populate the frame with linearly increasing sample values for
		// each band.
		frame := newSplitBandsFrame(numChannels, numBands)
		firstSampleIndex := frameIndex * subbandFrameLength
		for ch := range frame {
			for band := range frame[ch] {
				for i := range frame[ch][band] {
					frame[ch][band][i] = blockDelayBufferSampleValue(firstSampleIndex + i)
				}
			}
		}

		delayBuffer.delaySignal(frame)

		for ch := range frame {
			for band := range frame[ch] {
				for i, sample := range frame[ch][band] {
					want := float32(0)
					if sampleIndex := firstSampleIndex + i; sampleIndex >= delay {
						want = blockDelayBufferSampleValue(sampleIndex - delay)
					}
					require.Equal(t, want, sample, "rate %d, delay %d", rate, delay) //nolint:testifylint // Exact upstream check.
				}
			}
		}
	}
}

// Port of BlockDelayBufferTest.CorrectDelayApplied: the correct signal
// delay is achieved.
func TestBlockDelayBuffer_CorrectDelayApplied(t *testing.T) {
	for _, delay := range []int{0, 1, 27, 160, 4321, 7021} {
		for _, rate := range []int{16000, 32000, 48000} {
			for _, numChannels := range []int{1, 2, 4} {
				runBlockDelayBufferTest(t, delay, rate, numChannels)
			}
		}
	}
}
