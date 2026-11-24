// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// findPeakIndex matches FindPeakIndex.
func findPeakIndex(filterTimeDomain []float32, peakIndexIn, startSample, endSample int) int {
	peakIndexOut := peakIndexIn
	maxH2 := filterTimeDomain[peakIndexOut] * filterTimeDomain[peakIndexOut]
	for k := startSample; k <= endSample; k++ {
		tmp := filterTimeDomain[k] * filterTimeDomain[k]
		if tmp > maxH2 {
			peakIndexOut = k
			maxH2 = tmp
		}
	}

	return peakIndexOut
}

// filterRegion mirrors FilterAnalyzer::FilterRegion.
type filterRegion struct {
	startSample int
	endSample   int
}

// consistentFilterDetector mirrors
// FilterAnalyzer::ConsistentFilterDetector.
type consistentFilterDetector struct {
	significantPeak           bool
	filterFloorAccum          float32
	filterSecondaryPeak       float32
	filterFloorLowLimit       int
	filterFloorHighLimit      int
	activeRenderThreshold     float32
	consistentEstimateCounter int
	consistentDelayReference  int
}

func newConsistentFilterDetector(config echoCanceller3Config) consistentFilterDetector {
	limit := config.renderLevels.activeRenderLimit
	detector := consistentFilterDetector{activeRenderThreshold: limit * limit * kFftLengthBy2}
	detector.reset()

	return detector
}

func (d *consistentFilterDetector) reset() {
	d.significantPeak = false
	d.filterFloorAccum = 0
	d.filterSecondaryPeak = 0
	d.filterFloorLowLimit = 0
	d.filterFloorHighLimit = 0
	d.consistentEstimateCounter = 0
	d.consistentDelayReference = -10
}

// detect mirrors Detect.
func (d *consistentFilterDetector) detect(
	filterToAnalyze []float32, region filterRegion, xBlock *block, peakIndex, delayBlocks int,
) bool {
	if region.startSample == 0 {
		d.filterFloorAccum = 0
		d.filterSecondaryPeak = 0
		d.filterFloorLowLimit = 0
		if peakIndex >= 64 {
			d.filterFloorLowLimit = peakIndex - 64
		}
		// Upstream compares against an unsigned size - 129, which wraps
		// for filters shorter than 129 samples.
		d.filterFloorHighLimit = 0
		if len(filterToAnalyze) < 129 || peakIndex <= len(filterToAnalyze)-129 {
			d.filterFloorHighLimit = peakIndex + 128
		}
	}

	accumulate := func(from, to int) {
		for k := from; k < to; k++ {
			absH := abs32(filterToAnalyze[k])
			d.filterFloorAccum += absH
			d.filterSecondaryPeak = max(d.filterSecondaryPeak, absH)
		}
	}
	accumulate(region.startSample, min(region.endSample+1, d.filterFloorLowLimit))
	accumulate(max(d.filterFloorHighLimit, region.startSample), region.endSample+1)

	if region.endSample == len(filterToAnalyze)-1 {
		filterFloor := d.filterFloorAccum /
			float32(d.filterFloorLowLimit+len(filterToAnalyze)-d.filterFloorHighLimit)
		absPeak := abs32(filterToAnalyze[peakIndex])
		d.significantPeak = absPeak > 10*filterFloor && absPeak > 2*d.filterSecondaryPeak
	}

	if d.significantPeak {
		d.updateConsistency(xBlock, delayBlocks)
	}

	return float32(d.consistentEstimateCounter) > 1.5*kNumBlocksPerSecond
}

func (d *consistentFilterDetector) updateConsistency(xBlock *block, delayBlocks int) {
	activeRenderBlock := false
	for ch := range xBlock.numChannels {
		if energy(xBlock.view(0, ch)) > d.activeRenderThreshold {
			activeRenderBlock = true

			break
		}
	}

	if d.consistentDelayReference == delayBlocks {
		if activeRenderBlock {
			d.consistentEstimateCounter++
		}
	} else {
		d.consistentEstimateCounter = 0
		d.consistentDelayReference = delayBlocks
	}
}

// filterAnalysisState mirrors FilterAnalyzer::FilterAnalysisState.
type filterAnalysisState struct {
	gain                     float32
	peakIndex                int
	filterLengthBlocks       int
	consistentEstimate       bool
	consistentFilterDetector consistentFilterDetector
}

func (s *filterAnalysisState) reset(defaultGain float32) {
	s.peakIndex = 0
	s.gain = defaultGain
	s.consistentFilterDetector.reset()
}

// filterAnalyzer mirrors FilterAnalyzer: it analyzes the properties of
// the linear filter.
type filterAnalyzer struct {
	boundedErl           bool
	defaultGain          float32
	hHighpass            [][]float32
	blocksSinceReset     int
	region               filterRegion
	filterAnalysisStates []filterAnalysisState
	filterDelaysBlocks   []int
	minFilterDelayBlocks int
}

func newFilterAnalyzer(config echoCanceller3Config, numCaptureChannels int) *filterAnalyzer {
	analyzer := &filterAnalyzer{
		boundedErl:           config.epStrength.boundedErl,
		defaultGain:          config.epStrength.defaultGain,
		hHighpass:            make([][]float32, numCaptureChannels),
		filterAnalysisStates: make([]filterAnalysisState, numCaptureChannels),
		filterDelaysBlocks:   make([]int, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		analyzer.hHighpass[ch] = make([]float32, getTimeDomainLength(config.filter.refined.lengthBlocks))
		analyzer.filterAnalysisStates[ch] = filterAnalysisState{
			filterLengthBlocks:       config.filter.refinedInitial.lengthBlocks,
			consistentFilterDetector: newConsistentFilterDetector(config),
		}
	}
	analyzer.reset()

	return analyzer
}

// reset mirrors Reset: it resets the analysis.
func (a *filterAnalyzer) reset() {
	a.blocksSinceReset = 0
	a.region = filterRegion{}
	for ch := range a.filterAnalysisStates {
		a.filterAnalysisStates[ch].reset(a.defaultGain)
	}
	clear(a.filterDelaysBlocks)
}

// update mirrors Update: it updates the estimates with new input data.
func (a *filterAnalyzer) update(
	filtersTimeDomain [][]float32, render *renderBuffer,
) (anyFilterConsistent bool, maxEchoPathGain float32) {
	a.blocksSinceReset++
	a.setRegionToAnalyze(len(filtersTimeDomain[0]))
	a.analyzeRegion(filtersTimeDomain, render)

	// Aggregate the results for all capture channels.
	anyFilterConsistent = a.filterAnalysisStates[0].consistentEstimate
	maxEchoPathGain = a.filterAnalysisStates[0].gain
	a.minFilterDelayBlocks = a.filterDelaysBlocks[0]
	for ch := 1; ch < len(filtersTimeDomain); ch++ {
		state := &a.filterAnalysisStates[ch]
		anyFilterConsistent = anyFilterConsistent || state.consistentEstimate
		maxEchoPathGain = max(maxEchoPathGain, state.gain)
		a.minFilterDelayBlocks = min(a.minFilterDelayBlocks, a.filterDelaysBlocks[ch])
	}

	return anyFilterConsistent, maxEchoPathGain
}

// getFilterDelaysBlocks mirrors FilterDelaysBlocks: the delay in blocks
// for each filter.
func (a *filterAnalyzer) getFilterDelaysBlocks() []int { return a.filterDelaysBlocks }

// getMinFilterDelayBlocks mirrors MinFilterDelayBlocks: the minimum delay
// of all filters in terms of blocks.
func (a *filterAnalyzer) getMinFilterDelayBlocks() int { return a.minFilterDelayBlocks }

// filterLengthBlocks mirrors FilterLengthBlocks: the number of blocks of
// the current filter.
func (a *filterAnalyzer) filterLengthBlocks() int {
	return a.filterAnalysisStates[0].filterLengthBlocks
}

// getAdjustedFilters mirrors GetAdjustedFilters: the preprocessed filters.
func (a *filterAnalyzer) getAdjustedFilters() [][]float32 { return a.hHighpass }

// setRegionToAnalyze mirrors SetRegionToAnalyze.
func (a *filterAnalyzer) setRegionToAnalyze(filterSize int) {
	const numberBlocksToUpdate = 1
	if a.region.endSample >= filterSize-1 {
		a.region.startSample = 0
	} else {
		a.region.startSample = a.region.endSample + 1
	}
	a.region.endSample = min(a.region.startSample+numberBlocksToUpdate*kBlockSize-1, filterSize-1)
}

func (a *filterAnalyzer) analyzeRegion(filtersTimeDomain [][]float32, render *renderBuffer) {
	// Preprocess the filter to avoid issues with low-frequency components
	// in the filter.
	a.preProcessFilters(filtersTimeDomain)

	for ch := range filtersTimeDomain {
		state := &a.filterAnalysisStates[ch]
		highpass := a.hHighpass[ch]

		state.peakIndex = min(state.peakIndex, len(highpass)-1)
		state.peakIndex = findPeakIndex(highpass, state.peakIndex, a.region.startSample, a.region.endSample)
		a.filterDelaysBlocks[ch] = state.peakIndex >> kBlockSizeLog2
		a.updateFilterGain(highpass, state)
		state.filterLengthBlocks = len(filtersTimeDomain[ch]) / kBlockSize
		state.consistentEstimate = state.consistentFilterDetector.detect(
			highpass, a.region, render.getBlock(-a.filterDelaysBlocks[ch]), state.peakIndex,
			a.filterDelaysBlocks[ch])
	}
}

func (a *filterAnalyzer) updateFilterGain(filterTimeDomain []float32, state *filterAnalysisState) {
	sufficientTimeToConverge := a.blocksSinceReset > 5*kNumBlocksPerSecond
	if sufficientTimeToConverge && state.consistentEstimate {
		state.gain = abs32(filterTimeDomain[state.peakIndex])
	} else if state.gain != 0 {
		state.gain = max(state.gain, abs32(filterTimeDomain[state.peakIndex]))
	}

	if a.boundedErl && state.gain != 0 {
		state.gain = max(state.gain, 0.01)
	}
}

// highpassTaps is a minimum phase high-pass filter with cutoff frequency
// at about 600 Hz.
func highpassTaps() [3]float32 { return [3]float32{0.7929742, -0.36072128, -0.47047766} }

func (a *filterAnalyzer) preProcessFilters(filtersTimeDomain [][]float32) {
	taps := highpassTaps()
	for ch, filter := range filtersTimeDomain {
		a.hHighpass[ch] = a.hHighpass[ch][:len(filter)]
		highpass := a.hHighpass[ch]

		clear(highpass[a.region.startSample : a.region.endSample+1])
		for k := max(len(taps)-1, a.region.startSample); k <= a.region.endSample; k++ {
			tmp := highpass[k]
			for j, tap := range taps {
				tmp += filter[k-j] * tap
			}
			highpass[k] = tmp
		}
	}
}
