// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// echoPathDelayEstimator mirrors EchoPathDelayEstimator: it estimates the
// delay of the echo path from the downsampled render and capture signals.
type echoPathDelayEstimator struct {
	downSamplingFactor       int
	subBlockSize             int
	captureMixer             *alignmentMixer
	captureDecimator         *decimator
	matchedFilter            *matchedFilter
	lagAggregator            *matchedFilterLagAggregator
	oldAggregatedLag         delayEstimate
	hasOldAggregatedLag      bool
	consistentEstimateCount  int
	clockdriftDetector       clockdriftDetector
	downmixedCapture         [kBlockSize]float32
	downsampledCaptureBuffer [kBlockSize]float32
}

func newEchoPathDelayEstimator(
	delay delayConfig, renderLevels renderLevelsConfig, numCaptureChannels int,
) *echoPathDelayEstimator {
	subBlockSize := kBlockSize
	if delay.downSamplingFactor != 0 {
		subBlockSize = kBlockSize / delay.downSamplingFactor
	}
	excitationLimit := renderLevels.poorExcitationRenderLimit
	if delay.downSamplingFactor == 8 {
		excitationLimit = renderLevels.poorExcitationRenderLimitDs8
	}
	filter := newMatchedFilter(matchedFilterConfig{
		subBlockSize:            subBlockSize,
		windowSizeSubBlocks:     kMatchedFilterWindowSizeSubBlocks,
		numMatchedFilters:       delay.numFilters,
		alignmentShiftSubBlocks: kMatchedFilterAlignmentShiftSizeSubBlocks,
		excitationLimit:         excitationLimit,
		smoothingFast:           delay.delayEstimateSmoothing,
		smoothingSlow:           delay.delayEstimateSmoothingDelayFound,
		matchingFilterThreshold: delay.delayCandidateDetectionThreshold,
		detectPreEcho:           delay.detectPreEcho,
	})

	return &echoPathDelayEstimator{
		downSamplingFactor: delay.downSamplingFactor,
		subBlockSize:       subBlockSize,
		captureMixer:       newAlignmentMixer(numCaptureChannels, delay.captureAlignmentMixing),
		captureDecimator:   newDecimator(delay.downSamplingFactor),
		matchedFilter:      filter,
		lagAggregator:      newMatchedFilterLagAggregator(filter.maxFilterLag(), delay),
	}
}

// reset mirrors Reset(bool): it resets the estimate. If the delay
// confidence is reset, the reset behavior is as if the call is restarted.
func (e *echoPathDelayEstimator) reset(resetDelayConfidence bool) {
	e.resetInternal(true, resetDelayConfidence)
}

// estimateDelay mirrors EstimateDelay: it produces a delay estimate, in
// samples, if one is available.
func (e *echoPathDelayEstimator) estimateDelay(
	renderBuffer *downsampledRenderBuffer, capture *block,
) (delayEstimate, bool) {
	downsampledCapture := e.downsampledCaptureBuffer[:e.subBlockSize]
	e.captureMixer.produceOutput(capture, e.downmixedCapture[:])
	e.captureDecimator.decimate(e.downmixedCapture[:], downsampledCapture)
	e.matchedFilter.update(renderBuffer, downsampledCapture, e.lagAggregator.reliableDelayFound())

	aggregatedLag, hasLag := e.lagAggregator.aggregate(e.matchedFilter.bestLagEstimate())

	// Run clockdrift detection.
	if hasLag && aggregatedLag.quality == delayQualityRefined {
		e.clockdriftDetector.update(e.lagAggregator.delayAtHighestPeak())
	}

	// Return the detected delay in samples as the aggregated matched
	// filter lag compensated by the down sampling factor for the signal
	// being correlated.
	if hasLag {
		aggregatedLag.delay *= e.downSamplingFactor
	}

	if e.hasOldAggregatedLag && hasLag && e.oldAggregatedLag.delay == aggregatedLag.delay {
		e.consistentEstimateCount++
	} else {
		e.consistentEstimateCount = 0
	}
	e.oldAggregatedLag, e.hasOldAggregatedLag = aggregatedLag, hasLag
	if e.consistentEstimateCount > kNumBlocksPerSecond/2 {
		e.resetInternal(false, false)
	}

	return aggregatedLag, hasLag
}

// clockdrift mirrors Clockdrift.
func (e *echoPathDelayEstimator) clockdrift() clockdriftLevel {
	return e.clockdriftDetector.clockdriftLevel()
}

// resetInternal mirrors Reset(bool, bool).
func (e *echoPathDelayEstimator) resetInternal(resetLagAggregator, resetDelayConfidence bool) {
	if resetLagAggregator {
		e.lagAggregator.reset(resetDelayConfidence)
	}
	e.matchedFilter.reset(resetLagAggregator)
	e.hasOldAggregatedLag = false
	e.consistentEstimateCount = 0
}
