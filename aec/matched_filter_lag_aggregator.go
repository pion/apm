// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	// lagHistogramDataSize is the number of lags the aggregators remember.
	lagHistogramDataSize = 250
	// preEchoHistogramDataNotUpdated marks an unused slot of the pre-echo
	// history.
	preEchoHistogramDataNotUpdated = -1
)

// delayEstimateQuality mirrors DelayEstimate::Quality.
type delayEstimateQuality int

const (
	delayQualityCoarse delayEstimateQuality = iota
	delayQualityRefined
)

// delayEstimate mirrors DelayEstimate. The delay is in downsampled
// samples when it comes out of the lag aggregator.
type delayEstimate struct {
	quality delayEstimateQuality
	delay   int
}

// getDownSamplingBlockSizeLog2 matches GetDownSamplingBlockSizeLog2.
func getDownSamplingBlockSizeLog2(downSamplingFactor int) int {
	downSamplingFactorLog2 := 0
	downSamplingFactor >>= 1
	for downSamplingFactor > 0 {
		downSamplingFactorLog2++
		downSamplingFactor >>= 1
	}

	return max(kBlockSizeLog2-downSamplingFactorLog2, 0)
}

// maxElementIndex returns the index of the first largest value, like
// std::max_element.
func maxElementIndex(values []int) int {
	best := 0
	for i, value := range values {
		if value > values[best] {
			best = i
		}
	}

	return best
}

// highestPeakAggregator mirrors
// MatchedFilterLagAggregator::HighestPeakAggregator: a histogram of the
// last lags, whose peak is the candidate.
type highestPeakAggregator struct {
	histogram          []int
	histogramData      [lagHistogramDataSize]int
	histogramDataIndex int
	candidate          int
}

func newHighestPeakAggregator(maxFilterLag int) *highestPeakAggregator {
	return &highestPeakAggregator{histogram: make([]int, maxFilterLag+1), candidate: -1}
}

func (a *highestPeakAggregator) reset() {
	clear(a.histogram)
	a.histogramData = [lagHistogramDataSize]int{}
	a.histogramDataIndex = 0
}

func (a *highestPeakAggregator) aggregate(lag int) {
	a.histogram[a.histogramData[a.histogramDataIndex]]--
	a.histogramData[a.histogramDataIndex] = lag
	a.histogram[lag]++
	a.histogramDataIndex = (a.histogramDataIndex + 1) % lagHistogramDataSize
	a.candidate = maxElementIndex(a.histogram)
}

// preEchoLagAggregator mirrors
// MatchedFilterLagAggregator::PreEchoLagAggregator: a histogram of the
// last pre-echo lags, in blocks.
type preEchoLagAggregator struct {
	blockSizeLog2      int
	histogramData      [lagHistogramDataSize]int
	histogram          []int
	histogramDataIndex int
	preEchoCandidate   int
	numberUpdates      int
}

func newPreEchoLagAggregator(maxFilterLag, downSamplingFactor int) *preEchoLagAggregator {
	aggregator := &preEchoLagAggregator{
		blockSizeLog2: getDownSamplingBlockSizeLog2(downSamplingFactor),
		histogram:     make([]int, ((maxFilterLag+1)*downSamplingFactor)>>kBlockSizeLog2),
	}
	aggregator.reset()

	return aggregator
}

// reset clears the histogram. Like upstream it keeps the update count.
func (a *preEchoLagAggregator) reset() {
	clear(a.histogram)
	for i := range a.histogramData {
		a.histogramData[i] = preEchoHistogramDataNotUpdated
	}
	a.histogramDataIndex = 0
	a.preEchoCandidate = 0
}

func (a *preEchoLagAggregator) aggregate(preEchoLag int) {
	preEchoBlockSize := min(max(preEchoLag>>a.blockSizeLog2, 0), len(a.histogram)-1)

	// Remove the oldest point from the histogram. The initial points,
	// where the history is not filled yet, are ignored.
	if a.histogramData[a.histogramDataIndex] != preEchoHistogramDataNotUpdated {
		a.histogram[a.histogramData[a.histogramDataIndex]]--
	}
	a.histogramData[a.histogramDataIndex] = preEchoBlockSize
	a.histogram[preEchoBlockSize]++
	a.histogramDataIndex = (a.histogramDataIndex + 1) % lagHistogramDataSize

	preEchoCandidateBlockSize := 0
	if a.numberUpdates < kNumBlocksPerSecond*2 {
		a.numberUpdates++
		// During the first seconds, prefer the peaks at short delays.
		penalizationPerDelay := float32(1)
		maxHistogramValue := float32(-1)
		for start := 0; len(a.histogram)-start >= kMatchedFilterWindowSizeSubBlocks; start += kMatchedFilterWindowSizeSubBlocks {
			window := a.histogram[start : start+kMatchedFilterWindowSizeSubBlocks]
			peak := maxElementIndex(window)
			weightedMaxValue := float32(window[peak]) * penalizationPerDelay
			if weightedMaxValue > maxHistogramValue {
				maxHistogramValue = weightedMaxValue
				preEchoCandidateBlockSize = start + peak
			}
			penalizationPerDelay *= 0.7
		}
	} else {
		preEchoCandidateBlockSize = maxElementIndex(a.histogram)
	}
	a.preEchoCandidate = preEchoCandidateBlockSize << a.blockSizeLog2
}

// matchedFilterLagAggregator mirrors MatchedFilterLagAggregator: it
// aggregates the lag estimates of the matched filter into a delay
// estimate that is reported once it is seen often enough.
type matchedFilterLagAggregator struct {
	significantCandidateFound bool
	thresholds                delaySelectionThresholds
	headroom                  int
	highestPeak               *highestPeakAggregator
	preEchoLag                *preEchoLagAggregator
}

func newMatchedFilterLagAggregator(maxFilterLag int, cfg delayConfig) *matchedFilterLagAggregator {
	aggregator := &matchedFilterLagAggregator{
		thresholds:  cfg.delaySelectionThresholds,
		headroom:    cfg.delayHeadroomSamples / cfg.downSamplingFactor,
		highestPeak: newHighestPeakAggregator(maxFilterLag),
	}
	if cfg.detectPreEcho {
		aggregator.preEchoLag = newPreEchoLagAggregator(maxFilterLag, cfg.downSamplingFactor)
	}

	return aggregator
}

// reset clears the aggregated lags. A hard reset also forgets that a
// reliable delay was found.
func (a *matchedFilterLagAggregator) reset(hardReset bool) {
	a.highestPeak.reset()
	if a.preEchoLag != nil {
		a.preEchoLag.reset()
	}
	if hardReset {
		a.significantCandidateFound = false
	}
}

// aggregate mirrors Aggregate. hasLag tells whether the matched filter
// produced an estimate. It returns a delay estimate once one is
// reliable enough.
func (a *matchedFilterLagAggregator) aggregate(lag lagEstimate, hasLag bool) (delayEstimate, bool) {
	if !hasLag {
		return delayEstimate{}, false
	}
	if a.preEchoLag != nil {
		a.preEchoLag.aggregate(max(0, lag.preEchoLag-a.headroom))
	}

	a.highestPeak.aggregate(max(0, lag.lag-a.headroom))
	candidate := a.highestPeak.candidate
	count := a.highestPeak.histogram[candidate]
	a.significantCandidateFound = a.significantCandidateFound || count > a.thresholds.converged
	if count <= a.thresholds.converged && (count <= a.thresholds.initial || a.significantCandidateFound) {
		return delayEstimate{}, false
	}

	estimate := delayEstimate{quality: delayQualityCoarse, delay: candidate}
	if a.significantCandidateFound {
		estimate.quality = delayQualityRefined
	}
	if a.preEchoLag != nil {
		estimate.delay = a.preEchoLag.preEchoCandidate
	}

	return estimate, true
}

// reliableDelayFound mirrors ReliableDelayFound.
func (a *matchedFilterLagAggregator) reliableDelayFound() bool { return a.significantCandidateFound }

// delayAtHighestPeak mirrors GetDelayAtHighestPeak.
func (a *matchedFilterLagAggregator) delayAtHighestPeak() int { return a.highestPeak.candidate }
