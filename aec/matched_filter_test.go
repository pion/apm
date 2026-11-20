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

// matchedFilterReferenceDelta allows for fused multiply-add on some
// architectures. Filter taps and accumulated errors are at most about 1.
const matchedFilterReferenceDelta = 1e-5

// The reference scenarios use the upstream defaults: a downsampling
// factor of 4, so 16-sample sub-blocks, and 5 filters.
const (
	referenceSubBlockSize = 16
	referenceNumFilters   = 5
	referenceNumUpdates   = 500
)

func referenceMatchedFilter() *matchedFilter {
	return newMatchedFilter(matchedFilterConfig{
		subBlockSize:            referenceSubBlockSize,
		windowSizeSubBlocks:     kMatchedFilterWindowSizeSubBlocks,
		numMatchedFilters:       referenceNumFilters,
		alignmentShiftSubBlocks: kMatchedFilterAlignmentShiftSizeSubBlocks,
		excitationLimit:         150,
		smoothingFast:           0.7,
		smoothingSlow:           0.7,
		matchingFilterThreshold: 0.2,
		detectPreEcho:           true,
	})
}

// runReferenceScenario replays one scenario of the upstream reference.
// The capture is the render noise delayed by delay downsampled samples,
// plus two weaker taps starting at preEchoDelay when that is not
// negative. It returns the lag and pre-echo lag of every update, -1 when
// there is no estimate.
func runReferenceScenario(filter *matchedFilter, delay, preEchoDelay int) []float32 {
	buffer := newDownsampledRenderBuffer(getDownSampledBufferSize(4, referenceNumFilters))
	buffer.read = buffer.offsetIndex(buffer.write, referenceSubBlockSize)

	var history []float32
	lags := make([]float32, 0, 2*referenceNumUpdates)
	seed := uint32(1)
	for update := range referenceNumUpdates {
		var render, capture [referenceSubBlockSize]float32
		for i := range render {
			seed = seed*1664525 + 1013904223
			render[i] = float32(int(seed>>16)%16001 - 8000)
			history = append(history, render[i])
			now := len(history) - 1
			if now >= delay {
				capture[i] = history[now-delay]
			}
			if preEchoDelay >= 0 && now > preEchoDelay {
				capture[i] += 0.8 * (history[now-preEchoDelay] + history[now-preEchoDelay-1])
			}
		}

		// Insert the sub-block the way RenderDelayBuffer does: newest first.
		buffer.updateWriteIndex(-referenceSubBlockSize)
		for i := range render {
			buffer.buffer[buffer.write+i] = render[referenceSubBlockSize-1-i]
		}
		buffer.updateReadIndex(-referenceSubBlockSize)

		filter.update(buffer, capture[:], update >= referenceNumUpdates/2)
		if estimate, ok := filter.bestLagEstimate(); ok {
			lags = append(lags, float32(estimate.lag), float32(estimate.preEchoLag))
		} else {
			lags = append(lags, -1, -1)
		}
	}

	return lags
}

// TestMatchedFilter_MatchesUpstream compares the lag estimates of every
// update and the final state of the winning filter with the upstream
// MatchedFilter at the pinned commit, built with the portable filter core
// and fed the same signals. Each line of the reference file is keyed by
// "name delay pre_echo_delay".
func TestMatchedFilter_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/matched_filter_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	for _, scenario := range []struct{ delay, preEchoDelay int }{{100, -1}, {700, -1}, {300, 280}} {
		key := fmt.Sprintf("%d %d", scenario.delay, scenario.preEchoDelay)
		t.Run(key, func(t *testing.T) {
			filter := referenceMatchedFilter()
			lags := runReferenceScenario(filter, scenario.delay, scenario.preEchoDelay)

			assert.Equal(t, reference["lags "+key], lags)
			winner := filter.lastDetectedBestLagFilter
			require.Equal(t, reference["winner "+key], []float32{float32(winner)})
			assert.InDeltaSlice(t, reference["filter "+key], filter.filters[winner], matchedFilterReferenceDelta)
			assert.InDeltaSlice(t, reference["accumulated "+key], filter.accumulatedError[winner],
				matchedFilterReferenceDelta)
		})
	}
}

func TestMatchedFilter_Reset(t *testing.T) {
	filter := referenceMatchedFilter()
	runReferenceScenario(filter, 100, -1)
	_, ok := filter.bestLagEstimate()
	require.True(t, ok)

	filter.reset(true)
	_, ok = filter.bestLagEstimate()
	assert.False(t, ok)
	assert.Equal(t, make([]float32, len(filter.filters[0])), filter.filters[0])
	assert.InDelta(t, 1, filter.accumulatedError[0][0], 0)
	assert.Zero(t, filter.numberPreEchoUpdates)
	assert.Equal(t, 5*24*16+32*16, filter.maxFilterLag())
}

// Port of MatchedFilter.MaxSquarePeakIndex.
func TestMaxSquarePeakIndex(t *testing.T) {
	random := newTestRandom(42)
	const (
		maxLength              = 128
		numIterationsPerLength = 256
	)
	for length := 1; length < maxLength; length++ {
		values := make([]float32, length)
		for range numIterationsPerLength {
			randomizeSampleVector(random, values)

			want := 0
			for i, value := range values {
				if values[want]*values[want] < value*value {
					want = i
				}
			}
			require.Equal(t, want, maxSquarePeakIndex(values), "length %d", length)
		}
	}
}

// The upstream tests use 10 matched filters.
const testNumMatchedFilters = 10

func newTestMatchedFilter(cfg delayConfig, subBlockSize int, detectPreEcho bool) *matchedFilter {
	return newMatchedFilter(matchedFilterConfig{
		subBlockSize:            subBlockSize,
		windowSizeSubBlocks:     kMatchedFilterWindowSizeSubBlocks,
		numMatchedFilters:       testNumMatchedFilters,
		alignmentShiftSubBlocks: kMatchedFilterAlignmentShiftSizeSubBlocks,
		excitationLimit:         150,
		smoothingFast:           cfg.delayEstimateSmoothing,
		smoothingSlow:           cfg.delayEstimateSmoothingDelayFound,
		matchingFilterThreshold: cfg.delayCandidateDetectionThreshold,
		detectPreEcho:           detectPreEcho,
	})
}

// forEachMatchedFilterVariant runs test for both values of the
// detect_pre_echo parameter of the upstream tests and for both
// downsampling factors.
func forEachMatchedFilterVariant(t *testing.T, test func(t *testing.T, detectPreEcho bool, downSamplingFactor int)) {
	t.Helper()
	for _, detectPreEcho := range []bool{true, false} {
		for _, downSamplingFactor := range []int{4, 8} {
			t.Run(fmt.Sprintf("pre_echo_%t_ds%d", detectPreEcho, downSamplingFactor), func(t *testing.T) {
				test(t, detectPreEcho, downSamplingFactor)
			})
		}
	}
}

// Port of MatchedFilterTest.LagEstimation: the matched filter produces
// proper lag estimates for artificially delayed signals.
func TestMatchedFilter_LagEstimation(t *testing.T) {
	const sampleRateHz = 48000
	forEachMatchedFilterVariant(t, func(t *testing.T, detectPreEcho bool, downSamplingFactor int) {
		t.Helper()
		random := newTestRandom(42)
		subBlockSize := kBlockSize / downSamplingFactor
		render := newBlock(numBandsForRate(sampleRateHz), 1)
		capture := make([]float32, kBlockSize)
		downsampledCapture := make([]float32, subBlockSize)

		for _, delaySamples := range []int{5, 64, 150, 200, 800, 1000} {
			config := defaultEchoCanceller3Config()
			config.delay.downSamplingFactor = downSamplingFactor
			config.delay.numFilters = testNumMatchedFilters
			captureDecimator := newDecimator(downSamplingFactor)
			signalDelayBuffer := newTestDelayBuffer(downSamplingFactor * delaySamples)
			filter := newTestMatchedFilter(config.delay, subBlockSize, detectPreEcho)
			renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)

			// Analyze the correlation between render and capture.
			for k := range 600 + delaySamples/subBlockSize {
				randomizeSampleVector(random, render.data)
				signalDelayBuffer.delay(render.view(0, 0), capture)
				renderDelayBuffer.insert(render)
				if k == 0 {
					renderDelayBuffer.reset()
				}
				renderDelayBuffer.prepareCaptureProcessing()
				captureDecimator.decimate(capture, downsampledCapture)
				filter.update(renderDelayBuffer.getDownsampledRenderBuffer(), downsampledCapture, false)
			}

			// Verify that the expected most accurate lag estimate is correct.
			estimate, ok := filter.bestLagEstimate()
			require.True(t, ok, "delay %d", delaySamples)
			assert.Equal(t, lagEstimate{lag: delaySamples, preEchoLag: delaySamples}, estimate)
		}
	})
}

// Port of MatchedFilterTest.PreEchoEstimation.
func TestMatchedFilter_PreEchoEstimation(t *testing.T) {
	const sampleRateHz = 48000
	forEachMatchedFilterVariant(t, func(t *testing.T, detectPreEcho bool, downSamplingFactor int) {
		t.Helper()
		random := newTestRandom(42)
		subBlockSize := kBlockSize / downSamplingFactor
		render := newBlock(numBandsForRate(sampleRateHz), 1)
		capture := make([]float32, kBlockSize)
		captureWithPreEcho := make([]float32, kBlockSize)
		downsampledCapture := make([]float32, subBlockSize)

		// 20 ms and 50 ms.
		preEchoDelaySamples := 320 / downSamplingFactor
		echoDelaySamples := 800 / downSamplingFactor
		config := defaultEchoCanceller3Config()
		config.delay.downSamplingFactor = downSamplingFactor
		config.delay.numFilters = testNumMatchedFilters
		captureDecimator := newDecimator(downSamplingFactor)
		signalEchoDelayBuffer := newTestDelayBuffer(downSamplingFactor * echoDelaySamples)
		signalPreEchoDelayBuffer := newTestDelayBuffer(downSamplingFactor * preEchoDelaySamples)
		filter := newTestMatchedFilter(config.delay, subBlockSize, detectPreEcho)
		renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)

		// Analyze the correlation between render and capture.
		for k := range 600 + echoDelaySamples/subBlockSize {
			randomizeSampleVector(random, render.data)
			signalEchoDelayBuffer.delay(render.view(0, 0), capture)
			signalPreEchoDelayBuffer.delay(render.view(0, 0), captureWithPreEcho)
			for l := range capture {
				const gainPreEcho = 0.8
				capture[l] += gainPreEcho * captureWithPreEcho[l]
			}
			renderDelayBuffer.insert(render)
			if k == 0 {
				renderDelayBuffer.reset()
			}
			renderDelayBuffer.prepareCaptureProcessing()
			captureDecimator.decimate(capture, downsampledCapture)
			filter.update(renderDelayBuffer.getDownsampledRenderBuffer(), downsampledCapture, false)
		}

		// Verify that the expected most accurate lag estimate is correct.
		estimate, ok := filter.bestLagEstimate()
		require.True(t, ok)
		assert.Equal(t, echoDelaySamples, estimate.lag)
		if detectPreEcho {
			// The pre echo delay is estimated in a subsampled domain and a
			// larger error is allowed.
			assert.InDelta(t, preEchoDelaySamples, estimate.preEchoLag, 4)
		} else {
			// The pre echo delay falls back to the highest matched filter
			// peak when its detection is disabled.
			assert.Equal(t, echoDelaySamples, estimate.preEchoLag)
		}
	})
}

// Port of MatchedFilterTest.LagNotReliableForUncorrelatedRenderAndCapture.
func TestMatchedFilter_LagNotReliableForUncorrelatedRenderAndCapture(t *testing.T) {
	const sampleRateHz = 48000
	forEachMatchedFilterVariant(t, func(t *testing.T, detectPreEcho bool, downSamplingFactor int) {
		t.Helper()
		random := newTestRandom(42)
		config := defaultEchoCanceller3Config()
		config.delay.downSamplingFactor = downSamplingFactor
		config.delay.numFilters = testNumMatchedFilters
		subBlockSize := kBlockSize / downSamplingFactor

		render := newBlock(numBandsForRate(sampleRateHz), 1)
		capture := make([]float32, subBlockSize)
		renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)
		filter := newTestMatchedFilter(config.delay, subBlockSize, detectPreEcho)

		// Analyze the correlation between render and capture.
		for range 100 {
			randomizeSampleVector(random, render.view(0, 0))
			randomizeSampleVector(random, capture)
			renderDelayBuffer.insert(render)
			filter.update(renderDelayBuffer.getDownsampledRenderBuffer(), capture, false)
		}

		// Verify that no lag estimates are reliable.
		_, ok := filter.bestLagEstimate()
		assert.False(t, ok)
	})
}

// Port of MatchedFilterTest.LagNotUpdatedForLowLevelRender.
func TestMatchedFilter_LagNotUpdatedForLowLevelRender(t *testing.T) {
	const sampleRateHz = 48000
	forEachMatchedFilterVariant(t, func(t *testing.T, detectPreEcho bool, downSamplingFactor int) {
		t.Helper()
		random := newTestRandom(42)
		subBlockSize := kBlockSize / downSamplingFactor
		render := newBlock(numBandsForRate(sampleRateHz), 1)
		capture := make([]float32, kBlockSize)
		downsampledCapture := make([]float32, subBlockSize)
		config := defaultEchoCanceller3Config()
		filter := newTestMatchedFilter(config.delay, subBlockSize, detectPreEcho)
		renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, 1)
		captureDecimator := newDecimator(downSamplingFactor)

		// Analyze the correlation between render and capture.
		for range 100 {
			randomizeSampleVector(random, render.view(0, 0))
			for i := range render.view(0, 0) {
				render.view(0, 0)[i] *= 149.0 / 32767.0
			}
			copy(capture, render.view(0, 0))
			captureDecimator.decimate(capture, downsampledCapture)
			filter.update(renderDelayBuffer.getDownsampledRenderBuffer(), downsampledCapture, false)
		}

		// Verify that no lag estimate has been produced.
		_, ok := filter.bestLagEstimate()
		assert.False(t, ok)
	})
}
