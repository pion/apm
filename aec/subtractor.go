// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// predictionError matches PredictionError: e = y - s, with s the
// time-domain output of the filter.
func predictionError(fft *aec3Fft, echo *fftData, y []float32, e, s *[kBlockSize]float32) {
	var tmp [kFftLength]float32
	fft.inv(echo, &tmp)
	const scale = 1.0 / float32(kFftLengthBy2)
	for k := range kBlockSize {
		e[k] = y[k] - tmp[kFftLengthBy2+k]*scale
		s[k] = scale * tmp[kFftLengthBy2+k]
	}
}

// scaleFilterOutput matches ScaleFilterOutput.
func scaleFilterOutput(y []float32, factor float32, e, s *[kBlockSize]float32) {
	for k := range y {
		s[k] *= factor
		e[k] = y[k] - s[k]
	}
}

// filterMisadjustmentEstimator mirrors
// Subtractor::FilterMisadjustmentEstimator.
type filterMisadjustmentEstimator struct {
	nBlocksAcum      int
	e2Acum           float32
	y2Acum           float32
	invMisadjustment float32
	overhang         int
}

const misadjustmentBlocks = 4

// update mirrors Update: it updates the misadjustment estimator.
func (m *filterMisadjustmentEstimator) update(output *subtractorOutput) {
	m.e2Acum += output.e2Refined
	m.y2Acum += output.y2
	m.nBlocksAcum++
	if m.nBlocksAcum != misadjustmentBlocks {
		return
	}
	if m.y2Acum > misadjustmentBlocks*200*200*kBlockSize {
		update := m.e2Acum / m.y2Acum
		if m.e2Acum > misadjustmentBlocks*7500*7500*kBlockSize {
			// Duration equal to blockSizeMs * n_blocks * 4.
			m.overhang = 4
		} else {
			m.overhang = max(m.overhang-1, 0)
		}

		if update < m.invMisadjustment || m.overhang > 0 {
			m.invMisadjustment += 0.1 * (update - m.invMisadjustment)
		}
	}
	m.e2Acum = 0
	m.y2Acum = 0
	m.nBlocksAcum = 0
}

// getMisadjustment mirrors GetMisadjustment: the factor to apply to the
// filter to correct the misadjustment.
func (m *filterMisadjustmentEstimator) getMisadjustment() float32 {
	return 2 / float32(math.Sqrt(float64(m.invMisadjustment)))
}

// isAdjustmentNeeded mirrors IsAdjustmentNeeded.
func (m *filterMisadjustmentEstimator) isAdjustmentNeeded() bool {
	return m.invMisadjustment > 10
}

func (m *filterMisadjustmentEstimator) reset() {
	*m = filterMisadjustmentEstimator{}
}

// subtractor mirrors Subtractor: it removes the linear echo with a
// refined and a coarse adaptive filter per capture channel.
type subtractor struct {
	fft                           *aec3Fft
	config                        echoCanceller3Config
	numCaptureChannels            int
	refinedFilters                []*adaptiveFirFilter
	coarseFilter                  []*adaptiveFirFilter
	refinedGains                  []*refinedFilterUpdateGain
	coarseGains                   []*coarseFilterUpdateGain
	filterMisadjustmentEstimators []filterMisadjustmentEstimator
	poorCoarseFilterCounters      []int
	coarseFilterResetHangover     []int
	refinedFrequencyResponses     [][][kFftLengthBy2Plus1]float32
	refinedImpulseResponses       [][]float32
}

func newSubtractor(config echoCanceller3Config, numRenderChannels, numCaptureChannels int) *subtractor {
	filterConfig := config.filter
	maxRefinedLength := max(filterConfig.refinedInitial.lengthBlocks, filterConfig.refined.lengthBlocks)
	sub := &subtractor{
		fft:                           newAec3Fft(),
		config:                        config,
		numCaptureChannels:            numCaptureChannels,
		refinedFilters:                make([]*adaptiveFirFilter, numCaptureChannels),
		coarseFilter:                  make([]*adaptiveFirFilter, numCaptureChannels),
		refinedGains:                  make([]*refinedFilterUpdateGain, numCaptureChannels),
		coarseGains:                   make([]*coarseFilterUpdateGain, numCaptureChannels),
		filterMisadjustmentEstimators: make([]filterMisadjustmentEstimator, numCaptureChannels),
		poorCoarseFilterCounters:      make([]int, numCaptureChannels),
		coarseFilterResetHangover:     make([]int, numCaptureChannels),
		refinedFrequencyResponses:     make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		refinedImpulseResponses:       make([][]float32, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		sub.refinedFrequencyResponses[ch] = make([][kFftLengthBy2Plus1]float32, maxRefinedLength)
		sub.refinedImpulseResponses[ch] = make([]float32, getTimeDomainLength(maxRefinedLength))
		sub.refinedFilters[ch] = newAdaptiveFirFilter(
			filterConfig.refined.lengthBlocks, filterConfig.refinedInitial.lengthBlocks,
			filterConfig.configChangeDurationBlocks, numRenderChannels)
		sub.coarseFilter[ch] = newAdaptiveFirFilter(
			filterConfig.coarse.lengthBlocks, filterConfig.coarseInitial.lengthBlocks,
			filterConfig.configChangeDurationBlocks, numRenderChannels)
		sub.refinedGains[ch] = newRefinedFilterUpdateGain(
			filterConfig.refinedInitial, filterConfig.configChangeDurationBlocks)
		sub.coarseGains[ch] = newCoarseFilterUpdateGain(
			filterConfig.coarseInitial, filterConfig.configChangeDurationBlocks)
	}

	return sub
}

// handleEchoPathChange mirrors HandleEchoPathChange.
func (s *subtractor) handleEchoPathChange(variability echoPathVariability) {
	if variability.delayChange != delayAdjustmentNone {
		// Full reset.
		for ch := range s.numCaptureChannels {
			s.refinedFilters[ch].handleEchoPathChange()
			s.coarseFilter[ch].handleEchoPathChange()
			s.refinedGains[ch].handleEchoPathChange(variability)
			s.coarseGains[ch].handleEchoPathChange()
			s.refinedGains[ch].setConfig(s.config.filter.refinedInitial, true)
			s.coarseGains[ch].setConfig(s.config.filter.coarseInitial, true)
			s.refinedFilters[ch].setSizePartitions(s.config.filter.refinedInitial.lengthBlocks, true)
			s.coarseFilter[ch].setSizePartitions(s.config.filter.coarseInitial.lengthBlocks, true)
		}
	}

	if variability.gainChange {
		for ch := range s.numCaptureChannels {
			s.refinedGains[ch].handleEchoPathChange(variability)
		}
	}
}

// exitInitialState mirrors ExitInitialState.
func (s *subtractor) exitInitialState() {
	for ch := range s.numCaptureChannels {
		s.refinedGains[ch].setConfig(s.config.filter.refined, false)
		s.coarseGains[ch].setConfig(s.config.filter.coarse, false)
		s.refinedFilters[ch].setSizePartitions(s.config.filter.refined.lengthBlocks, false)
		s.coarseFilter[ch].setSizePartitions(s.config.filter.coarse.lengthBlocks, false)
	}
}

// process mirrors Process: it performs the echo subtraction.
// saturatedCapture is AecState::SaturatedCapture.
func (s *subtractor) process(
	render *renderBuffer,
	capture *block,
	analyzer *renderSignalAnalyzer,
	saturatedCapture bool,
	outputs []subtractorOutput,
) {
	// Compute the render powers.
	var x2Refined, x2Coarse [kFftLengthBy2Plus1]float32
	refinedSize := s.refinedFilters[0].sizePartitions()
	coarseSize := s.coarseFilter[0].sizePartitions()
	switch {
	case refinedSize == coarseSize:
		render.spectralSum(refinedSize, &x2Refined)
		x2Coarse = x2Refined
	case refinedSize > coarseSize:
		render.spectralSums(coarseSize, refinedSize, &x2Coarse, &x2Refined)
	default:
		render.spectralSums(refinedSize, coarseSize, &x2Refined, &x2Coarse)
	}

	// Process all capture channels.
	for ch := range s.numCaptureChannels {
		s.processChannel(ch, render, capture.view(0, ch), analyzer, saturatedCapture,
			&x2Refined, &x2Coarse, &outputs[ch])
	}
}

func (s *subtractor) processChannel(
	ch int,
	render *renderBuffer,
	capture []float32,
	analyzer *renderSignalAnalyzer,
	saturatedCapture bool,
	x2Refined, x2Coarse *[kFftLengthBy2Plus1]float32,
	output *subtractorOutput,
) {
	var errorCoarse, gain fftData

	// Form the outputs of the refined and coarse filters.
	s.refinedFilters[ch].filter(render, &gain)
	predictionError(s.fft, &gain, capture, &output.eRefined, &output.sRefined)
	s.coarseFilter[ch].filter(render, &gain)
	predictionError(s.fft, &gain, capture, &output.eCoarse, &output.sCoarse)

	// Compute the signal powers in the subtractor output.
	output.computeMetrics(capture)

	// Adjust the filter if needed.
	refinedFiltersAdjusted := false
	estimator := &s.filterMisadjustmentEstimators[ch]
	estimator.update(output)
	if estimator.isAdjustmentNeeded() {
		scale := estimator.getMisadjustment()
		s.refinedFilters[ch].scaleFilter(scale)
		for k := range s.refinedImpulseResponses[ch] {
			s.refinedImpulseResponses[ch][k] *= scale
		}
		scaleFilterOutput(capture, scale, &output.eRefined, &output.sRefined)
		estimator.reset()
		refinedFiltersAdjusted = true
	}

	// Compute the FFTs of the refined and coarse filter outputs.
	s.fft.zeroPaddedFft(&output.eRefined, windowHanning, &output.ERefined)
	s.fft.zeroPaddedFft(&output.eCoarse, windowHanning, &errorCoarse)

	// Compute spectra for future use.
	errorCoarse.spectrum(&output.E2Coarse)
	output.ERefined.spectrum(&output.E2Refined)

	// Update the refined filter.
	if !refinedFiltersAdjusted {
		// Do not allow the performance of the coarse filter to affect the
		// adaptation speed of the refined filter just after the coarse
		// filter has been reset.
		disallowLeakageDiverged := s.coarseFilterResetHangover[ch] > 0

		var erl [kFftLengthBy2Plus1]float32
		computeErl(s.refinedFrequencyResponses[ch], &erl)
		s.refinedGains[ch].compute(x2Refined, analyzer, output, &erl, s.refinedFilters[ch].sizePartitions(),
			saturatedCapture, disallowLeakageDiverged, &gain)
	} else {
		gain.clear()
	}
	s.refinedFilters[ch].adaptUpdateImpulse(render, &gain, &s.refinedImpulseResponses[ch])
	s.refinedFrequencyResponses[ch] = s.refinedFilters[ch].computeFrequencyResponse(s.refinedFrequencyResponses[ch])

	// Update the coarse filter.
	if output.e2Refined < output.e2Coarse {
		s.poorCoarseFilterCounters[ch]++
	} else {
		s.poorCoarseFilterCounters[ch] = 0
	}
	if s.poorCoarseFilterCounters[ch] < 5 {
		s.coarseGains[ch].compute(x2Coarse, analyzer, &errorCoarse, s.coarseFilter[ch].sizePartitions(),
			saturatedCapture, &gain)
		s.coarseFilterResetHangover[ch] = max(s.coarseFilterResetHangover[ch]-1, 0)
	} else {
		s.poorCoarseFilterCounters[ch] = 0
		s.coarseFilter[ch].setFilter(s.refinedFilters[ch].sizePartitions(), s.refinedFilters[ch].getFilter())
		s.coarseGains[ch].compute(x2Coarse, analyzer, &output.ERefined, s.coarseFilter[ch].sizePartitions(),
			saturatedCapture, &gain)
		s.coarseFilterResetHangover[ch] = s.config.filter.coarseResetHangoverBlocks
	}
	s.coarseFilter[ch].adapt(render, &gain)

	for k := range output.eRefined {
		output.eRefined[k] = min(max(output.eRefined[k], -32768), 32767)
	}
}

// filterFrequencyResponses mirrors FilterFrequencyResponses: the
// frequency responses of the refined filters.
func (s *subtractor) filterFrequencyResponses() [][][kFftLengthBy2Plus1]float32 {
	return s.refinedFrequencyResponses
}

// filterImpulseResponses mirrors FilterImpulseResponses: the estimates of
// the impulse responses of the refined filters.
func (s *subtractor) filterImpulseResponses() [][]float32 {
	return s.refinedImpulseResponses
}
