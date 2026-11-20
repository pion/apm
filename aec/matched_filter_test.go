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

// The filter bank finds the delay between render and capture, in the
// filter that covers it. The reference scenario reads the render buffer
// one sub-block late, so the lag is one sub-block short of the delay.
func TestMatchedFilter_FindsDelay(t *testing.T) {
	for _, tc := range []struct{ delay, wantFilter int }{{100, 0}, {700, 1}, {1200, 2}} {
		filter := referenceMatchedFilter()
		lags := runReferenceScenario(filter, tc.delay, -1)

		assert.InDelta(t, tc.delay-referenceSubBlockSize, lags[len(lags)-2], 0, "delay %d", tc.delay)
		assert.Equal(t, tc.wantFilter, filter.lastDetectedBestLagFilter, "delay %d", tc.delay)
	}
}

// Energy that arrives before the main peak moves the pre-echo lag ahead
// of the lag.
func TestMatchedFilter_DetectsPreEcho(t *testing.T) {
	filter := referenceMatchedFilter()
	lags := runReferenceScenario(filter, 300, 280)

	lag, preEchoLag := lags[len(lags)-2], lags[len(lags)-1]
	assert.InDelta(t, 300-referenceSubBlockSize, lag, 0)
	assert.Less(t, preEchoLag, lag)
	assert.InDelta(t, 280-referenceSubBlockSize, preEchoLag, kAccumulatedErrorSubSampleRate)
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

func TestMaxSquarePeakIndex(t *testing.T) {
	assert.Equal(t, 0, maxSquarePeakIndex(nil))
	assert.Equal(t, 0, maxSquarePeakIndex([]float32{3}))
	assert.Equal(t, 1, maxSquarePeakIndex([]float32{1, -2}))
	assert.Equal(t, 2, maxSquarePeakIndex([]float32{1, -2, 3}))
	assert.Equal(t, 3, maxSquarePeakIndex([]float32{1, 2, 3, -4, 1}))
	// On a tie the even index wins, as upstream.
	assert.Equal(t, 2, maxSquarePeakIndex([]float32{0, 0, 5, 5, 0, 0}))
}
