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

// Port of EchoPathDelayEstimatorMultiChannel.BasicApiCalls.
func TestEchoPathDelayEstimator_BasicAPICalls(t *testing.T) {
	const sampleRateHz = 48000
	for _, numRenderChannels := range []int{1, 2, 3, 6, 8} {
		for _, numCaptureChannels := range []int{1, 2, 4} {
			config := defaultEchoCanceller3Config()
			renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels)
			estimator := newEchoPathDelayEstimator(config.delay, config.renderLevels, numCaptureChannels)
			render := newBlock(numBandsForRate(sampleRateHz), numRenderChannels)
			capture := newBlock(1, numCaptureChannels)
			for range 100 {
				renderDelayBuffer.insert(render)
				estimator.estimateDelay(renderDelayBuffer.getDownsampledRenderBuffer(), capture)
			}
		}
	}
}

// Port of EchoPathDelayEstimator.DelayEstimation: the delay estimator
// produces the correct delay for artificially delayed signals.
func TestEchoPathDelayEstimator_DelayEstimation(t *testing.T) {
	const sampleRateHz = 48000
	random := newTestRandom(42)
	render := newBlock(numBandsForRate(sampleRateHz), 1)
	capture := newBlock(1, 1)
	for _, downSamplingFactor := range []int{4, 8} {
		config := defaultEchoCanceller3Config()
		config.delay.delayHeadroomSamples = 0
		config.delay.downSamplingFactor = downSamplingFactor
		config.delay.numFilters = 10
		for _, delaySamples := range []int{30, 64, 150, 200, 800, 4000} {
			renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)
			signalDelayBuffer := newTestDelayBuffer(delaySamples)
			estimator := newEchoPathDelayEstimator(config.delay, config.renderLevels, 1)

			var estimatedDelaySamples delayEstimate
			found := false
			for k := range 500 + delaySamples/kBlockSize {
				randomizeSampleVector(random, render.view(0, 0))
				signalDelayBuffer.delay(render.view(0, 0), capture.view(0, 0))
				renderDelayBuffer.insert(render)
				if k == 0 {
					renderDelayBuffer.reset()
				}
				renderDelayBuffer.prepareCaptureProcessing()
				if estimate, ok := estimator.estimateDelay(renderDelayBuffer.getDownsampledRenderBuffer(), capture); ok {
					estimatedDelaySamples, found = estimate, true
				}
			}

			require.True(t, found, "delay %d, factor %d", delaySamples, downSamplingFactor)
			// Allow the estimated delay to be off by a block as internally
			// the delay is quantized with an error up to a block.
			assert.InDelta(t, delaySamples/downSamplingFactor, estimatedDelaySamples.delay/downSamplingFactor,
				float64(kBlockSize/downSamplingFactor), "delay %d, factor %d", delaySamples, downSamplingFactor)
		}
	}
}

// Port of EchoPathDelayEstimator.NoDelayEstimatesForLowLevelRenderSignals.
func TestEchoPathDelayEstimator_NoDelayEstimatesForLowLevelRenderSignals(t *testing.T) {
	const sampleRateHz = 48000
	random := newTestRandom(42)
	config := defaultEchoCanceller3Config()
	render := newBlock(numBandsForRate(sampleRateHz), 1)
	capture := newBlock(1, 1)
	estimator := newEchoPathDelayEstimator(config.delay, config.renderLevels, 1)
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)

	for range 100 {
		randomizeSampleVector(random, render.view(0, 0))
		for i := range render.view(0, 0) {
			render.view(0, 0)[i] *= 100.0 / 32767.0
		}
		copy(capture.view(0, 0), render.view(0, 0))
		renderDelayBuffer.insert(render)
		renderDelayBuffer.prepareCaptureProcessing()
		_, ok := estimator.estimateDelay(renderDelayBuffer.getDownsampledRenderBuffer(), capture)
		assert.False(t, ok)
	}
}
