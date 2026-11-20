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

// referenceMaxFilterLag is the maximum lag of the default matched filter.
const referenceMaxFilterLag = 5*24*16 + 32*16

// referenceLag returns the lag estimate of one call of the upstream
// reference sequence, for the random value in [0, 100) of that call.
func referenceLag(call, random int) (lagEstimate, bool) {
	if call < 10 || random < 10 {
		return lagEstimate{}, false
	}
	base := 20
	if call < 300 {
		base = 284
	} else if call < 900 {
		base = 700
	}
	jitter := 0
	if random < 30 {
		jitter = random % 7
	}
	preEcho := base
	if base >= 17+jitter && random%3 != 0 {
		preEcho = base - 17
	}

	return lagEstimate{lag: base + jitter, preEchoLag: preEcho}, true
}

// runLagAggregatorReference replays the lag sequence of the upstream
// reference: no estimate for the first calls and 10 % of the time,
// outliers 20 % of the time, a delay that moves from 284 to 700 and then
// to 20, a soft reset at call 500 and a hard reset at call 900. It
// returns, for every call, the quality of the returned estimate (-1 for
// none), its delay, reliableDelayFound and delayAtHighestPeak.
func runLagAggregatorReference(aggregator *matchedFilterLagAggregator) []float32 {
	const numCalls = 1200
	results := make([]float32, 0, 4*numCalls)
	seed := uint32(1)
	for call := range numCalls {
		seed = seed*1664525 + 1013904223
		random := int(seed>>16) % 100
		switch call {
		case 500:
			aggregator.reset(false)
		case 900:
			aggregator.reset(true)
		}

		lag, hasLag := referenceLag(call, random)

		quality, delay := -1, -1
		if estimate, ok := aggregator.aggregate(lag, hasLag); ok {
			quality, delay = int(estimate.quality), estimate.delay
		}
		reliable := 0
		if aggregator.reliableDelayFound() {
			reliable = 1
		}
		results = append(results, float32(quality), float32(delay), float32(reliable),
			float32(aggregator.delayAtHighestPeak()))
	}

	return results
}

// TestMatchedFilterLagAggregator_MatchesUpstream compares the result of
// every call with the upstream MatchedFilterLagAggregator at the pinned
// commit, with and without pre-echo detection. Each line of the
// reference file is keyed by "aggregate detect_pre_echo".
func TestMatchedFilterLagAggregator_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/lag_aggregator_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	require.Len(t, reference, 2)

	for _, detectPreEcho := range []bool{true, false} {
		cfg := defaultDelayConfig()
		cfg.detectPreEcho = detectPreEcho
		aggregator := newMatchedFilterLagAggregator(referenceMaxFilterLag, cfg)

		key := fmt.Sprintf("aggregate %d", int(boolValue(detectPreEcho)))
		assert.Equal(t, reference[key], runLagAggregatorReference(aggregator), "detect pre-echo %t", detectPreEcho)
	}
}

// A lag is reported as coarse once it is seen more often than the initial
// threshold, and as refined above the converged threshold. The headroom
// is taken off the lag.
func TestMatchedFilterLagAggregator_ReportsAfterThresholds(t *testing.T) {
	cfg := defaultDelayConfig()
	cfg.detectPreEcho = false
	aggregator := newMatchedFilterLagAggregator(referenceMaxFilterLag, cfg)
	headroom := cfg.delayHeadroomSamples / cfg.downSamplingFactor
	lag := lagEstimate{lag: 100, preEchoLag: 100}

	_, ok := aggregator.aggregate(lagEstimate{}, false)
	assert.False(t, ok)
	assert.Equal(t, -1, aggregator.delayAtHighestPeak())

	for call := 1; call <= cfg.delaySelectionThresholds.converged+1; call++ {
		estimate, ok := aggregator.aggregate(lag, true)
		switch {
		case call <= cfg.delaySelectionThresholds.initial:
			assert.False(t, ok, "call %d", call)
		case call <= cfg.delaySelectionThresholds.converged:
			require.True(t, ok, "call %d", call)
			assert.Equal(t, delayEstimate{quality: delayQualityCoarse, delay: 100 - headroom}, estimate)
			assert.False(t, aggregator.reliableDelayFound())
		default:
			require.True(t, ok, "call %d", call)
			assert.Equal(t, delayEstimate{quality: delayQualityRefined, delay: 100 - headroom}, estimate)
			assert.True(t, aggregator.reliableDelayFound())
		}
	}

	aggregator.reset(false)
	assert.True(t, aggregator.reliableDelayFound())
	aggregator.reset(true)
	assert.False(t, aggregator.reliableDelayFound())
}

func TestGetDownSamplingBlockSizeLog2(t *testing.T) {
	assert.Equal(t, 6, getDownSamplingBlockSizeLog2(1))
	assert.Equal(t, 4, getDownSamplingBlockSizeLog2(4))
	assert.Equal(t, 3, getDownSamplingBlockSizeLog2(8))
	assert.Equal(t, 0, getDownSamplingBlockSizeLog2(128))
}
