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

// runDelayEstimatorReference replays one scenario of the upstream
// reference. The capture is the render noise delayed by delay samples, in
// the last channel; the other channels are silent. The estimator is reset
// at block 1000. It returns, for every block, the quality of the estimate
// (-1 for none), its delay and the clockdrift level.
func runDelayEstimatorReference(downSamplingFactor, numChannels, delay int) []float32 {
	const numBlocks = 1500
	cfg := defaultDelayConfig()
	cfg.downSamplingFactor = downSamplingFactor
	subBlockSize := kBlockSize / downSamplingFactor

	estimator := newEchoPathDelayEstimator(cfg, defaultRenderLevelsConfig(), numChannels)
	renderDecimator := newDecimator(downSamplingFactor)
	buffer := newDownsampledRenderBuffer(getDownSampledBufferSize(downSamplingFactor, cfg.numFilters))
	buffer.read = buffer.offsetIndex(buffer.write, subBlockSize)

	var history []float32
	results := make([]float32, 0, 3*numBlocks)
	render := make([]float32, kBlockSize)
	downsampled := make([]float32, subBlockSize)
	capture := newBlock(1, numChannels)
	seed := uint32(1)
	for blockIndex := range numBlocks {
		if blockIndex == 1000 {
			estimator.reset(true)
		}
		captureChannel := capture.view(0, numChannels-1)
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

		quality, estimatedDelay := -1, -1
		if estimate, ok := estimator.estimateDelay(buffer, capture); ok {
			quality, estimatedDelay = int(estimate.quality), estimate.delay
		}
		results = append(results, float32(quality), float32(estimatedDelay), float32(estimator.clockdrift()))
	}

	return results
}

// TestEchoPathDelayEstimator_MatchesUpstream compares the estimate of
// every block with the upstream EchoPathDelayEstimator at the pinned
// commit, built with the portable matched filter. Each line of the
// reference file is keyed by
// "estimates down_sampling_factor capture_channels delay".
func TestEchoPathDelayEstimator_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/echo_path_delay_estimator_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	require.Len(t, reference, 3)

	for _, scenario := range []struct{ downSamplingFactor, numChannels, delay int }{
		{4, 1, 300}, {8, 2, 1000}, {4, 2, 600},
	} {
		key := fmt.Sprintf("estimates %d %d %d", scenario.downSamplingFactor, scenario.numChannels, scenario.delay)
		results := runDelayEstimatorReference(scenario.downSamplingFactor, scenario.numChannels, scenario.delay)
		assert.Equal(t, reference[key], results, key)
	}
}
