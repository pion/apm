// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDelayControllerReference replays one scenario of the upstream
// reference. The capture is the render noise delayed by delay1 samples,
// and by delay2 from block 700 on. The controller is reset at block 1200.
// It returns, for every block, the quality of the delay (-1 for none),
// the delay in blocks and hasClockdrift.
func runDelayControllerReference(delay1, delay2 int) []float32 {
	const numBlocks = 1500
	cfg := defaultDelayConfig()
	subBlockSize := kBlockSize / cfg.downSamplingFactor

	controller := newRenderDelayController(cfg, defaultRenderLevelsConfig(), 1)
	renderDecimator := newDecimator(cfg.downSamplingFactor)
	buffer := newDownsampledRenderBuffer(getDownSampledBufferSize(cfg.downSamplingFactor, cfg.numFilters))
	buffer.read = buffer.offsetIndex(buffer.write, subBlockSize)

	var history []float32
	results := make([]float32, 0, 3*numBlocks)
	render := make([]float32, kBlockSize)
	downsampled := make([]float32, subBlockSize)
	capture := newBlock(1, 1)
	seed := uint32(1)
	for blockIndex := range numBlocks {
		if blockIndex == 1200 {
			controller.reset(true)
		}
		delay := delay1
		if blockIndex >= 700 {
			delay = delay2
		}
		captureChannel := capture.view(0, 0)
		for i := range render {
			seed = seed*1664525 + 1013904223
			render[i] = float32(int(seed>>16)%16001 - 8000)
			history = append(history, render[i])
			captureChannel[i] = 0
			if now := len(history) - 1; now >= delay {
				captureChannel[i] = history[now-delay]
			}
		}

		// Insert the decimated render the way RenderDelayBuffer does.
		renderDecimator.decimate(render, downsampled)
		buffer.updateWriteIndex(-subBlockSize)
		for i := range downsampled {
			buffer.buffer[buffer.write+i] = downsampled[subBlockSize-1-i]
		}
		buffer.updateReadIndex(-subBlockSize)

		quality, delayBlocks := -1, -1
		if estimate, ok := controller.getDelay(buffer, capture); ok {
			quality, delayBlocks = int(estimate.quality), estimate.delay
		}
		results = append(results, float32(quality), float32(delayBlocks), boolValue(controller.hasClockdrift()))
	}

	return results
}

// TestRenderDelayController_MatchesUpstream compares the delay of every
// block with the upstream RenderDelayController at the pinned commit,
// built with the portable matched filter. Each line of the reference
// file is keyed by "delays delay1 delay2".
func TestRenderDelayController_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/render_delay_controller_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	require.Len(t, reference, 3)

	for _, scenario := range []struct{ delay1, delay2 int }{{300, 364}, {600, 1100}, {900, 500}} {
		key := fmt.Sprintf("delays %d %d", scenario.delay1, scenario.delay2)
		assert.Equal(t, reference[key], runDelayControllerReference(scenario.delay1, scenario.delay2), key)
	}
}

func TestComputeBufferDelay(t *testing.T) {
	refined := func(delay int) delayEstimate {
		return delayEstimate{quality: delayQualityRefined, delay: delay}
	}

	// Samples become blocks.
	assert.Equal(t, refined(3), computeBufferDelay(delayEstimate{}, false, 1, refined(3*kBlockSize+10)))
	// An increase within the hysteresis keeps the current delay.
	assert.Equal(t, refined(3), computeBufferDelay(refined(3), true, 1, refined(4*kBlockSize)))
	// A larger increase, or any decrease, is applied.
	assert.Equal(t, refined(5), computeBufferDelay(refined(3), true, 1, refined(5*kBlockSize)))
	assert.Equal(t, refined(2), computeBufferDelay(refined(3), true, 1, refined(2*kBlockSize)))
	// Without hysteresis the increase is applied.
	assert.Equal(t, refined(4), computeBufferDelay(refined(3), true, 0, refined(4*kBlockSize)))
}
