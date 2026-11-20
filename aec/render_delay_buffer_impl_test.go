// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of RenderDelayBuffer.BufferOverflow.
func TestRenderDelayBuffer_BufferOverflow(t *testing.T) {
	config := defaultEchoCanceller3Config()
	for _, numChannels := range []int{1, 2, 8} {
		for _, rate := range []int{16000, 32000, 48000} {
			delayBuffer := newRenderDelayBufferImpl(config, rate, numChannels)
			blockToInsert := newBlock(numBandsForRate(rate), numChannels)
			for range 10 {
				assert.Equal(t, bufferingEventNone, delayBuffer.insert(blockToInsert))
			}
			overrunOccurred := false
			for range 1000 {
				overrunOccurred = overrunOccurred || delayBuffer.insert(blockToInsert) == bufferingEventRenderOverrun
			}
			assert.True(t, overrunOccurred, "rate %d, channels %d", rate, numChannels)
		}
	}
}

// Port of RenderDelayBuffer.AvailableBlock.
func TestRenderDelayBuffer_AvailableBlock(t *testing.T) {
	const sampleRateHz = 48000
	delayBuffer := newRenderDelayBufferImpl(defaultEchoCanceller3Config(), sampleRateHz, 1)
	inputBlock := newBlock(numBandsForRate(sampleRateHz), 1)
	fill(inputBlock.data, 1)

	assert.Equal(t, bufferingEventNone, delayBuffer.insert(inputBlock))
	delayBuffer.prepareCaptureProcessing()
}

// Port of RenderDelayBuffer.AlignFromDelay.
func TestRenderDelayBuffer_AlignFromDelay(t *testing.T) {
	delayBuffer := newRenderDelayBufferImpl(defaultEchoCanceller3Config(), 16000, 1)
	require.NotZero(t, delayBuffer.getDelay())
	delayBuffer.reset()
	for delay := range 20 {
		require.True(t, delayBuffer.alignFromDelay(delay))
		assert.Equal(t, delay, delayBuffer.getDelay())
	}
}

// renderDelayBufferScenario drives a buffer through the steps of the
// upstream reference: generated noise with render bursts, missing render
// blocks, delay changes and resets.
type renderDelayBufferScenario struct {
	buffer *renderDelayBufferImpl
	block  *block
	seed   uint32
}

func newRenderDelayBufferScenario(rate, numChannels int) *renderDelayBufferScenario {
	return &renderDelayBufferScenario{
		buffer: newRenderDelayBufferImpl(defaultEchoCanceller3Config(), rate, numChannels),
		block:  newBlock(numBandsForRate(rate), numChannels),
		seed:   1,
	}
}

func (s *renderDelayBufferScenario) insert() bufferingEvent {
	for i := range s.block.data {
		s.seed = s.seed*1664525 + 1013904223
		s.block.data[i] = float32(int(s.seed>>16)%16001 - 8000)
	}

	return s.buffer.insert(s.block)
}

// numInserts returns the number of render blocks of a step.
func (s *renderDelayBufferScenario) numInserts(step int) int {
	switch {
	case step >= 300 && step < 320:
		return 2 // Render burst.
	case step >= 600 && step < 640:
		return 0 // Missing render blocks.
	case step >= 1500 && step < 1800 && step%20 == 0:
		return 2 // Slow excess of render.
	}

	return 1
}

// alignment returns the delay to align from at a step, if any.
func (s *renderDelayBufferScenario) alignment(step int) (int, bool) {
	delays := map[int]int{100: 3, 200: 3, 400: 40, 800: 200, 1300: 7}
	delay, ok := delays[step]

	return delay, ok
}

// step runs one step and returns its state: the events of the last insert
// and of the capture preparation and the result of the alignment (-1 for
// none), then the delay, the position, the headroom, the read and write
// indices of the downsampled buffer, the render activity and whether an
// external delay was received.
func (s *renderDelayBufferScenario) step(step int) []float32 {
	insertEvent := -1
	for range s.numInserts(step) {
		insertEvent = int(s.insert())
	}
	switch step {
	case 1000:
		s.buffer.setAudioBufferDelay(40)
	case 1100:
		s.buffer.reset()
	}
	prepareEvent := -1
	if step%97 == 96 {
		s.buffer.handleSkippedCaptureProcessing()
	} else {
		prepareEvent = int(s.buffer.prepareCaptureProcessing())
	}
	aligned := float32(-1)
	if delay, ok := s.alignment(step); ok {
		aligned = boolValue(s.buffer.alignFromDelay(delay))
	}
	render := s.buffer.getRenderBuffer()
	lowRate := s.buffer.getDownsampledRenderBuffer()

	return []float32{
		float32(insertEvent), float32(prepareEvent), aligned, float32(s.buffer.getDelay()),
		float32(render.position()), float32(render.headroom()), float32(lowRate.read), float32(lowRate.write),
		boolValue(render.getRenderActivity()), boolValue(s.buffer.hasReceivedBufferDelay()),
	}
}

// TestRenderDelayBuffer_MatchesUpstream compares the state after every
// step, and the data the echo remover reads after the last ones, with
// the upstream RenderDelayBuffer at the pinned commit. Each line of the
// reference file is keyed by "name rate channels [step]".
func TestRenderDelayBuffer_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/render_delay_buffer_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	for _, stream := range []struct{ rate, numChannels int }{{16000, 1}, {48000, 2}} {
		key := fmt.Sprintf("%d %d", stream.rate, stream.numChannels)
		t.Run(key, func(t *testing.T) {
			scenario := newRenderDelayBufferScenario(stream.rate, stream.numChannels)
			require.Equal(t, reference["max_delay "+key], []float32{float32(scenario.buffer.maxDelay())})

			var state []float32
			for step := range 2000 {
				state = append(state, scenario.step(step)...)
			}
			require.Equal(t, reference["state "+key], state)

			// The data of the last channel, of the last band for the block.
			channel := stream.numChannels - 1
			band := numBandsForRate(stream.rate) - 1
			for step := range 3 {
				scenario.insert()
				scenario.buffer.prepareCaptureProcessing()
				render := scenario.buffer.getRenderBuffer()
				stepKey := fmt.Sprintf(" %s %d", key, step)

				assert.Equal(t, reference["block"+stepKey], render.getBlock(0).view(band, channel))
				spectrum := render.spectrum(0)[channel]
				assertTransform(t, reference["spectrum"+stepKey], spectrum[:], "spectrum"+stepKey)
				data := &render.getFftBuffer()[render.position()][channel]
				assertTransform(t, reference["fft_re"+stepKey], data.re[:], "fft_re"+stepKey)
				assertTransform(t, reference["fft_im"+stepKey], data.im[:], "fft_im"+stepKey)
			}

			// The newest 64 samples of the downsampled render.
			lowRate := scenario.buffer.getDownsampledRenderBuffer()
			newest := make([]float32, 64)
			for i := range newest {
				newest[i] = lowRate.buffer[lowRate.offsetIndex(lowRate.read, i)]
			}
			assert.InDeltaSlice(t, reference["low_rate "+key], newest, 0.05)
		})
	}
}

// TestRenderDelayBuffer_ExternalDelayMatchesUpstream compares the delay,
// the position and the headroom of every step with an upstream buffer
// that is aligned from an external delay estimate.
func TestRenderDelayBuffer_ExternalDelayMatchesUpstream(t *testing.T) {
	var want []float32
	for _, entry := range refdata.Load(t, "testdata/render_delay_buffer_upstream.txt") {
		if entry.Key == "external" {
			want = entry.Samples
		}
	}

	config := defaultEchoCanceller3Config()
	config.delay.useExternalDelayEstimator = true
	buffer := newRenderDelayBufferImpl(config, 16000, 1)
	blk := newBlock(1, 1)

	var state []float32
	for step := range 300 {
		switch step {
		case 50:
			buffer.setAudioBufferDelay(40)
		case 150:
			buffer.setAudioBufferDelay(100)
		case 200:
			buffer.reset()
		}
		buffer.insert(blk)
		if step%30 == 29 {
			buffer.insert(blk)
		}
		buffer.prepareCaptureProcessing()
		buffer.alignFromExternalDelay()
		render := buffer.getRenderBuffer()
		state = append(state, float32(buffer.getDelay()), float32(render.position()), float32(render.headroom()))
	}
	assert.Equal(t, want, state)
}
