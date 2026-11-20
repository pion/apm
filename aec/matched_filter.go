// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// kAccumulatedErrorSubSampleRate is the subsample rate used for computing
// the accumulated error. The core function depends on it being 4.
const kAccumulatedErrorSubSampleRate = 4

// updateAccumulatedError matches UpdateAccumulatedError.
func updateAccumulatedError(instantaneousAccumulatedError, accumulatedError []float32, oneOverErrorSumAnchor float32) {
	const smoothConstantIncreases = 0.015
	for k, instantaneous := range instantaneousAccumulatedError {
		errorNorm := instantaneous * oneOverErrorSumAnchor
		if errorNorm < accumulatedError[k] {
			accumulatedError[k] = errorNorm
		} else {
			accumulatedError[k] += smoothConstantIncreases * (errorNorm - accumulatedError[k])
		}
	}
}

// computePreEchoLag matches ComputePreEchoLag.
func computePreEchoLag(accumulatedError []float32, lag, alignmentShiftWinner int) int {
	const preEchoThreshold = 0.5
	preEchoLagEstimate := lag - alignmentShiftWinner
	maximumPreEchoLag := min(preEchoLagEstimate/kAccumulatedErrorSubSampleRate, len(accumulatedError))
	for k := maximumPreEchoLag - 1; k >= 0; k-- {
		if accumulatedError[k] > preEchoThreshold {
			break
		}
		preEchoLagEstimate = (k+1)*kAccumulatedErrorSubSampleRate - 1
	}

	return preEchoLagEstimate + alignmentShiftWinner
}

// matchedFilterCore matches MatchedFilterCore, the portable version of
// the filter core. render is the circular render buffer, read from
// startIndex; capture is the capture sub-block. It returns whether the
// filter was updated and the sum of the squared errors.
func matchedFilterCore(
	startIndex int,
	x2SumThreshold, smoothing float32,
	render, capture, filter []float32,
	computeAccumulatedError bool,
	accumulatedError []float32,
) (filtersUpdated bool, errorSum float32) {
	if computeAccumulatedError {
		clear(accumulatedError)
	} else {
		accumulatedError = nil
	}

	// Process for all samples in the sub-block.
	for _, sample := range capture {
		x2Sum, estimate := applyMatchedFilter(startIndex, render, filter, sample, accumulatedError)

		// Compute the matched filter error.
		filterError := sample - estimate
		saturation := sample >= 32000 || sample <= -32000
		errorSum += filterError * filterError

		// Update the matched filter estimate in an NLMS manner.
		if x2Sum > x2SumThreshold && !saturation {
			adaptMatchedFilter(startIndex, render, filter, smoothing*filterError/x2Sum)
			filtersUpdated = true
		}

		startIndex--
		if startIndex < 0 {
			startIndex = len(render) - 1
		}
	}

	return filtersUpdated, errorSum
}

// applyMatchedFilter applies the matched filter as filter * x and
// computes x * x, for the capture sample. A non-nil accumulatedError
// collects the squared error of the partial estimates, every
// kAccumulatedErrorSubSampleRate taps.
func applyMatchedFilter(
	startIndex int, render, filter []float32, sample float32, accumulatedError []float32,
) (x2Sum, estimate float32) {
	index := startIndex
	for k, tap := range filter {
		x2Sum += render[index] * render[index]
		estimate += tap * render[index]
		index++
		if index == len(render) {
			index = 0
		}
		if accumulatedError != nil && (k+1)&0b11 == 0 {
			accumulatedError[k>>2] += (sample - estimate) * (sample - estimate)
		}
	}

	return x2Sum, estimate
}

// adaptMatchedFilter computes
// filter = filter + smoothing * (y - filter * x) * x / x * x.
func adaptMatchedFilter(startIndex int, render, filter []float32, alpha float32) {
	index := startIndex
	for k := range filter {
		filter[k] += alpha * render[index]
		index++
		if index == len(render) {
			index = 0
		}
	}
}

// maxSquarePeakIndex matches MaxSquarePeakIndex. Even and odd elements
// are tracked separately as upstream does, which decides ties.
func maxSquarePeakIndex(filter []float32) int {
	if len(filter) < 2 {
		return 0
	}
	maxElement1 := filter[0] * filter[0]
	maxElement2 := filter[1] * filter[1]
	lagEstimate1 := 0
	lagEstimate2 := 1
	lastIndex := len(filter) - 1
	for k := 2; k < lastIndex; k += 2 {
		element1 := filter[k] * filter[k]
		element2 := filter[k+1] * filter[k+1]
		if element1 > maxElement1 {
			maxElement1 = element1
			lagEstimate1 = k
		}
		if element2 > maxElement2 {
			maxElement2 = element2
			lagEstimate2 = k + 1
		}
	}
	if maxElement2 > maxElement1 {
		maxElement1 = maxElement2
		lagEstimate1 = lagEstimate2
	}

	// In case of odd size, the last element is not checked yet.
	lastElement := filter[lastIndex] * filter[lastIndex]
	if lastElement > maxElement1 {
		return lastIndex
	}

	return lagEstimate1
}

// lagEstimate mirrors MatchedFilter::LagEstimate, in downsampled samples.
type lagEstimate struct {
	lag        int
	preEchoLag int
}

// matchedFilterConfig holds the constructor arguments of MatchedFilter.
type matchedFilterConfig struct {
	subBlockSize            int
	windowSizeSubBlocks     int
	numMatchedFilters       int
	alignmentShiftSubBlocks int
	excitationLimit         float32
	smoothingFast           float32
	smoothingSlow           float32
	matchingFilterThreshold float32
	detectPreEcho           bool
}

// matchedFilter mirrors MatchedFilter: a bank of NLMS filters, each
// covering a different range of delays between the downsampled render and
// capture signals. The filter with the lowest error gives the lag.
type matchedFilter struct {
	cfg                         matchedFilterConfig
	filterIntraLagShift         int
	filters                     [][]float32
	accumulatedError            [][]float32
	instantaneousAccumulatedErr []float32
	reportedLagEstimate         lagEstimate
	hasReportedLagEstimate      bool
	lastDetectedBestLagFilter   int
	numberPreEchoUpdates        int
}

func newMatchedFilter(cfg matchedFilterConfig) *matchedFilter {
	filterSize := cfg.windowSizeSubBlocks * cfg.subBlockSize
	filter := &matchedFilter{
		cfg:                       cfg,
		filterIntraLagShift:       cfg.alignmentShiftSubBlocks * cfg.subBlockSize,
		filters:                   make([][]float32, cfg.numMatchedFilters),
		lastDetectedBestLagFilter: -1,
	}
	for n := range filter.filters {
		filter.filters[n] = make([]float32, filterSize)
	}
	if cfg.detectPreEcho {
		filter.accumulatedError = make([][]float32, cfg.numMatchedFilters)
		for n := range filter.accumulatedError {
			filter.accumulatedError[n] = make([]float32, filterSize/kAccumulatedErrorSubSampleRate)
			fill(filter.accumulatedError[n], 1)
		}
		filter.instantaneousAccumulatedErr = make([]float32, filterSize/kAccumulatedErrorSubSampleRate)
	}

	return filter
}

// reset clears the filters. A full reset also clears the pre-echo state.
func (m *matchedFilter) reset(fullReset bool) {
	for _, filter := range m.filters {
		clear(filter)
	}
	m.hasReportedLagEstimate = false
	if fullReset {
		for _, accumulated := range m.accumulatedError {
			fill(accumulated, 1)
		}
		m.numberPreEchoUpdates = 0
	}
}

// bestLagEstimate mirrors GetBestLagEstimate: the estimate of the last
// update, if it found a reliable one.
func (m *matchedFilter) bestLagEstimate() (lagEstimate, bool) {
	return m.reportedLagEstimate, m.hasReportedLagEstimate
}

// maxFilterLag mirrors GetMaxFilterLag.
func (m *matchedFilter) maxFilterLag() int {
	return len(m.filters)*m.filterIntraLagShift + len(m.filters[0])
}

// update mirrors Update: it adapts every filter on one capture sub-block
// and picks the best lag estimate.
func (m *matchedFilter) update(renderBuffer *downsampledRenderBuffer, capture []float32, useSlowSmoothing bool) {
	smoothing := m.cfg.smoothingFast
	if useSlowSmoothing {
		smoothing = m.cfg.smoothingSlow
	}
	x2SumThreshold := float32(len(m.filters[0])) * m.cfg.excitationLimit * m.cfg.excitationLimit

	// Compute anchor for the matched filter error.
	var errorSumAnchor float32
	for _, sample := range capture {
		errorSumAnchor += sample * sample
	}

	// Apply all matched filters.
	winnerErrorSum := errorSumAnchor
	winnerLag := 0
	m.hasReportedLagEstimate = false
	alignmentShift := 0
	previousLagEstimate := -1
	winnerIndex := -1
	for n := range m.filters {
		startIndex := (renderBuffer.read + alignmentShift + m.cfg.subBlockSize - 1) % len(renderBuffer.buffer)
		peak, errorSum, candidate := m.updateFilter(
			n, startIndex, x2SumThreshold, smoothing, renderBuffer.buffer, capture, errorSumAnchor)

		// Find the best estimate.
		lag := peak + alignmentShift
		if candidate && errorSum < winnerErrorSum {
			winnerErrorSum = errorSum
			winnerIndex = n
			// In case that 2 matched filters return the same winner
			// candidate (overlap region), the one with the smaller index
			// is chosen in order to search for pre-echoes.
			if previousLagEstimate == lag {
				winnerIndex = n - 1
			}
			winnerLag = lag
		}
		previousLagEstimate = lag
		alignmentShift += m.filterIntraLagShift
	}

	if winnerIndex != -1 {
		m.reportLagEstimate(winnerIndex, winnerLag, errorSumAnchor)
	}
}

// updateFilter adapts filter n on the capture sub-block. It returns the
// peak of the filter, the sum of its squared errors and whether the peak
// is a candidate for the lag: the filter was updated and is reliable.
func (m *matchedFilter) updateFilter(
	n, startIndex int, x2SumThreshold, smoothing float32, render, capture []float32, errorSumAnchor float32,
) (peak int, errorSum float32, candidate bool) {
	filter := m.filters[n]
	computePreEcho := m.cfg.detectPreEcho && n == m.lastDetectedBestLagFilter
	filtersUpdated, errorSum := matchedFilterCore(
		startIndex, x2SumThreshold, smoothing, render, capture, filter,
		computePreEcho, m.instantaneousAccumulatedErr)

	// Estimate the lag in the matched filter as the distance to the
	// portion in the filter that contributes the most to the matched
	// filter output. This is detected as the peak of the matched filter.
	peak = maxSquarePeakIndex(filter)
	reliable := peak > 2 && peak < len(filter)-10 &&
		errorSum < m.cfg.matchingFilterThreshold*errorSumAnchor

	return peak, errorSum, filtersUpdated && reliable
}

// reportLagEstimate records the winning lag of an update and, when the
// same filter won the previous update, refines its pre-echo lag.
func (m *matchedFilter) reportLagEstimate(winnerIndex, winnerLag int, errorSumAnchor float32) {
	m.reportedLagEstimate = lagEstimate{lag: winnerLag, preEchoLag: winnerLag}
	m.hasReportedLagEstimate = true
	if m.cfg.detectPreEcho && m.lastDetectedBestLagFilter == winnerIndex {
		const energyThreshold = 1.0
		if errorSumAnchor > energyThreshold {
			updateAccumulatedError(m.instantaneousAccumulatedErr, m.accumulatedError[winnerIndex], 1/errorSumAnchor)
			m.numberPreEchoUpdates++
		}
		if m.numberPreEchoUpdates >= 50 {
			m.reportedLagEstimate.preEchoLag = computePreEchoLag(
				m.accumulatedError[winnerIndex], winnerLag, winnerIndex*m.filterIntraLagShift)
		}
	}
	m.lastDetectedBestLagFilter = winnerIndex
}

// fill sets every element of values to value.
func fill(values []float32, value float32) {
	for i := range values {
		values[i] = value
	}
}
