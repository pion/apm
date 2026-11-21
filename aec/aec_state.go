// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// averageChannels averages the spectra of the render channels.
func averageChannels(spectra [][kFftLengthBy2Plus1]float32, renderPower *[kFftLengthBy2Plus1]float32) {
	*renderPower = [kFftLengthBy2Plus1]float32{}
	for ch := range spectra {
		for k := range renderPower {
			renderPower[k] += spectra[ch][k]
		}
	}
	normalizer := 1 / float32(len(spectra))
	for k := range renderPower {
		renderPower[k] *= normalizer
	}
}

// computeAvgRenderReverb matches ComputeAvgRenderReverb: the render
// spectrum at the delay, averaged over the channels, plus its reverb.
func computeAvgRenderReverb(
	spectrumBuf *spectrumBuffer,
	delayBlocks int,
	reverbDecay float32,
	model *reverbModel,
	reverbPowerSpectrum *[kFftLengthBy2Plus1]float32,
) {
	idxAtDelay := spectrumBuf.offsetIndex(spectrumBuf.read, delayBlocks)
	idxPast := spectrumBuf.incIndex(idxAtDelay)

	x2 := &spectrumBuf.buffer[idxAtDelay][0]
	if len(spectrumBuf.buffer[0]) > 1 {
		var x2Data [kFftLengthBy2Plus1]float32
		averageChannels(spectrumBuf.buffer[idxPast], &x2Data)
		model.updateReverbNoFreqShaping(x2Data[:], 1, reverbDecay)

		averageChannels(spectrumBuf.buffer[idxAtDelay], &x2Data)
		x2 = &x2Data
	} else {
		model.updateReverbNoFreqShaping(spectrumBuf.buffer[idxPast][0][:], 1, reverbDecay)
	}

	reverbPower := model.getReverb()
	for k := range x2 {
		reverbPowerSpectrum[k] = x2[k] + reverbPower[k]
	}
}

// aecInitialState mirrors AecState::InitialState: it controls the
// transition from the initial state, which in turn controls when the
// filter parameters for the initial state are used.
type aecInitialState struct {
	conservativeInitialPhase       bool
	initialStateSeconds            float32
	transitionTriggered            bool
	initialState                   bool
	strongNotSaturatedRenderBlocks int
}

func newAecInitialState(config echoCanceller3Config) aecInitialState {
	return aecInitialState{
		conservativeInitialPhase: config.filter.conservativeInitialPhase,
		initialStateSeconds:      config.filter.initialStateSeconds,
		initialState:             true,
	}
}

// reset mirrors Reset: it resets the state to again begin in the initial
// state.
func (s *aecInitialState) reset() {
	s.initialState = true
	s.strongNotSaturatedRenderBlocks = 0
}

// update mirrors Update.
func (s *aecInitialState) update(activeRender, saturatedCapture bool) {
	if activeRender && !saturatedCapture {
		s.strongNotSaturatedRenderBlocks++
	}

	// Flag whether the initial state is still active.
	prevInitialState := s.initialState
	if s.conservativeInitialPhase {
		s.initialState = s.strongNotSaturatedRenderBlocks < 5*kNumBlocksPerSecond
	} else {
		s.initialState = float32(s.strongNotSaturatedRenderBlocks) < s.initialStateSeconds*kNumBlocksPerSecond
	}

	// Flag whether the transition from the initial state has started.
	s.transitionTriggered = !s.initialState && prevInitialState
}

// aecFilterDelay mirrors AecState::FilterDelay: it chooses the direct
// path delay relative to the beginning of the filter, and holds the other
// data related to the delay.
type aecFilterDelay struct {
	delayHeadroomBlocks int
	filterDelaysBlocks  []int
	minFilterDelay      int
	externalDelay       delayEstimate
	hasExternalDelay    bool
}

func newAecFilterDelay(config echoCanceller3Config, numCaptureChannels int) aecFilterDelay {
	delayHeadroomBlocks := config.delay.delayHeadroomSamples / kBlockSize
	delay := aecFilterDelay{
		delayHeadroomBlocks: delayHeadroomBlocks,
		filterDelaysBlocks:  make([]int, numCaptureChannels),
		minFilterDelay:      delayHeadroomBlocks,
	}
	for ch := range delay.filterDelaysBlocks {
		delay.filterDelaysBlocks[ch] = delayHeadroomBlocks
	}

	return delay
}

// update mirrors Update: it updates the delay estimates based on new
// data.
func (d *aecFilterDelay) update(
	analyzerFilterDelayEstimatesBlocks []int,
	externalDelay delayEstimate,
	hasExternalDelay bool,
	blocksWithProperFilterAdaptation int,
) {
	// Update the delay based on the external delay.
	if hasExternalDelay {
		d.externalDelay = externalDelay
		d.hasExternalDelay = true
	}

	// Override the estimated delay if it is not certain that the filter
	// has had time to converge.
	delayEstimatorMayNotHaveConverged := blocksWithProperFilterAdaptation < 2*kNumBlocksPerSecond
	if delayEstimatorMayNotHaveConverged && d.hasExternalDelay {
		for ch := range d.filterDelaysBlocks {
			d.filterDelaysBlocks[ch] = d.delayHeadroomBlocks
		}
	} else {
		copy(d.filterDelaysBlocks, analyzerFilterDelayEstimatesBlocks)
	}

	d.minFilterDelay = d.filterDelaysBlocks[0]
	for _, delay := range d.filterDelaysBlocks[1:] {
		d.minFilterDelay = min(d.minFilterDelay, delay)
	}
}

// filteringQualityAnalyzer mirrors AecState::FilteringQualityAnalyzer: it
// analyzes how well the linear filter performs, and can be expected to
// perform, on the current signals. This selects the echo suppression
// functionality and the input to the echo suppressor.
type filteringQualityAnalyzer struct {
	useLinearFilter              bool
	overallUsableLinearEstimates bool
	filterUpdateBlocksSinceReset int
	filterUpdateBlocksSinceStart int
	convergenceSeen              bool
	usableLinearFilterEstimates  []bool
}

func newFilteringQualityAnalyzer(config echoCanceller3Config, numCaptureChannels int) filteringQualityAnalyzer {
	return filteringQualityAnalyzer{
		useLinearFilter:             config.filter.useLinearFilter,
		usableLinearFilterEstimates: make([]bool, numCaptureChannels),
	}
}

// reset mirrors Reset.
func (f *filteringQualityAnalyzer) reset() {
	clear(f.usableLinearFilterEstimates)
	f.overallUsableLinearEstimates = false
	f.filterUpdateBlocksSinceReset = 0
}

// update mirrors Update: it updates the analysis based on new data.
func (f *filteringQualityAnalyzer) update(
	activeRender, transparentMode, saturatedCapture, hasExternalDelay, anyFilterConverged bool,
) {
	// Update the blocks counters.
	if activeRender && !saturatedCapture {
		f.filterUpdateBlocksSinceReset++
		f.filterUpdateBlocksSinceStart++
	}

	// Store the convergence flag when observed.
	f.convergenceSeen = f.convergenceSeen || anyFilterConverged

	// The linear filter can only be used if it has had time to converge.
	f.overallUsableLinearEstimates = f.sufficientDataToConverge()

	// The linear filter can only be used if an external delay or
	// convergence have been identified.
	if !hasExternalDelay && !f.convergenceSeen {
		f.overallUsableLinearEstimates = false
	}

	// If the transparent mode is on, deactivate the use of the linear
	// filter.
	if transparentMode {
		f.overallUsableLinearEstimates = false
	}

	if f.useLinearFilter {
		for ch := range f.usableLinearFilterEstimates {
			f.usableLinearFilterEstimates[ch] = f.overallUsableLinearEstimates
		}
	}
}

// sufficientDataToConverge verifies the requirements for achieving a
// decent filter. They are more restrictive for the filter adaptation at
// call startup than after an in-call reset.
func (f *filteringQualityAnalyzer) sufficientDataToConverge() bool {
	sufficientDataToConvergeAtStartup := float32(f.filterUpdateBlocksSinceStart) > kNumBlocksPerSecond*float32(0.4)
	sufficientDataToConvergeAtReset := sufficientDataToConvergeAtStartup &&
		float32(f.filterUpdateBlocksSinceReset) > kNumBlocksPerSecond*float32(0.2)

	return sufficientDataToConvergeAtStartup && sufficientDataToConvergeAtReset
}

// saturationDetector mirrors AecState::SaturationDetector: it detects
// whether the echo is to be considered saturated.
type saturationDetector struct {
	saturatedEcho bool
}

// update mirrors Update: it updates the detection decision based on new
// data.
func (s *saturationDetector) update(
	render *block,
	saturatedCapture, usableLinearEstimate bool,
	outputs []subtractorOutput,
	echoPathGain float32,
) {
	s.saturatedEcho = false
	if !saturatedCapture {
		return
	}

	if usableLinearEstimate {
		const saturationThreshold = 20000
		for ch := range outputs {
			s.saturatedEcho = s.saturatedEcho ||
				outputs[ch].sRefinedMaxAbs > saturationThreshold || outputs[ch].sCoarseMaxAbs > saturationThreshold
		}

		return
	}

	var maxSample float32
	for ch := range render.numChannels {
		maxSample = max(maxSample, maxAbs(render.view(0, ch)))
	}

	const margin = 10
	peakEchoAmplitude := maxSample * echoPathGain * margin
	s.saturatedEcho = peakEchoAmplitude > 32000
}

// aecState mirrors AecState: it handles the state and the conditions for
// the echo removal functionality.
//
// Upstream does not reset the subtractor output analyzer at an echo path
// change when the field trial
// WebRTC-Aec3AecStateSubtractorAnalyzerResetKillSwitch is enabled. Field
// trials are not ported.
type aecState struct {
	config                         echoCanceller3Config
	initialState                   aecInitialState
	delayState                     aecFilterDelay
	transparentState               transparentMode
	filterQualityState             filteringQualityAnalyzer
	saturationDetector             saturationDetector
	erlEstimator                   *erlEstimator
	erleEstimator                  *erleEstimator
	strongNotSaturatedRenderBlocks int
	blocksWithActiveRender         int
	captureSignalSaturation        bool
	filterAnalyzer                 *filterAnalyzer
	echoAudibility                 *echoAudibility
	reverbModelEstimator           *reverbModelEstimator
	avgRenderReverb                reverbModel
	subtractorOutputAnalyzer       *subtractorOutputAnalyzer
}

func newAecState(config echoCanceller3Config, numCaptureChannels int) *aecState {
	return &aecState{
		config:                   config,
		initialState:             newAecInitialState(config),
		delayState:               newAecFilterDelay(config, numCaptureChannels),
		transparentState:         newTransparentMode(config),
		filterQualityState:       newFilteringQualityAnalyzer(config, numCaptureChannels),
		erlEstimator:             newErlEstimator(2 * kNumBlocksPerSecond),
		erleEstimator:            newErleEstimator(2*kNumBlocksPerSecond, config, numCaptureChannels),
		filterAnalyzer:           newFilterAnalyzer(config, numCaptureChannels),
		echoAudibility:           newEchoAudibility(config.echoAudibility.useStationarityPropertiesAtInit),
		reverbModelEstimator:     newReverbModelEstimator(config, numCaptureChannels),
		subtractorOutputAnalyzer: newSubtractorOutputAnalyzer(numCaptureChannels),
	}
}

// usableLinearEstimate mirrors UsableLinearEstimate: whether the echo
// subtractor can be used to determine the residual echo.
func (a *aecState) usableLinearEstimate() bool {
	return a.filterQualityState.overallUsableLinearEstimates && a.config.filter.useLinearFilter
}

// useLinearFilterOutput mirrors UseLinearFilterOutput: whether the echo
// subtractor output should be used as output.
func (a *aecState) useLinearFilterOutput() bool {
	return a.filterQualityState.overallUsableLinearEstimates && a.config.filter.useLinearFilter
}

// activeRender mirrors ActiveRender: whether the render signal is
// currently active.
func (a *aecState) activeRender() bool { return a.blocksWithActiveRender > 200 }

// getResidualEchoScaling mirrors GetResidualEchoScaling: the scaling of
// the residual echo to match the audibility.
func (a *aecState) getResidualEchoScaling(residualScaling []float32) {
	limit := float32(0.8 * kNumBlocksPerSecond)
	if a.config.filter.conservativeInitialPhase {
		limit = 1.5 * kNumBlocksPerSecond
	}
	filterHasHadTimeToConverge := float32(a.strongNotSaturatedRenderBlocks) >= limit
	a.echoAudibility.getResidualEchoScaling(filterHasHadTimeToConverge, residualScaling)
}

// useStationarityProperties mirrors UseStationarityProperties: whether
// the stationarity properties of the signals are used.
func (a *aecState) useStationarityProperties() bool {
	return a.config.echoAudibility.useStationarityProperties
}

// erle mirrors Erle.
func (a *aecState) erle(onsetCompensated bool) [][kFftLengthBy2Plus1]float32 {
	return a.erleEstimator.getErle(onsetCompensated)
}

// erleUnbounded mirrors ErleUnbounded: the ERLE that is not capped.
func (a *aecState) erleUnbounded() [][kFftLengthBy2Plus1]float32 {
	return a.erleEstimator.getErleUnbounded()
}

// fullBandErleLog2 mirrors FullBandErleLog2: the fullband ERLE estimate
// in log2 units.
func (a *aecState) fullBandErleLog2() float32 { return a.erleEstimator.fullbandErleLog2() }

// erl mirrors Erl.
func (a *aecState) erl() *[kFftLengthBy2Plus1]float32 { return a.erlEstimator.getErl() }

// erlTimeDomain mirrors ErlTimeDomain.
func (a *aecState) erlTimeDomain() float32 { return a.erlEstimator.getErlTimeDomain() }

// minDirectPathFilterDelay mirrors MinDirectPathFilterDelay: the delay
// estimate based on the linear filter.
func (a *aecState) minDirectPathFilterDelay() int { return a.delayState.minFilterDelay }

// saturatedCapture mirrors SaturatedCapture.
func (a *aecState) saturatedCapture() bool { return a.captureSignalSaturation }

// saturatedEcho mirrors SaturatedEcho.
func (a *aecState) saturatedEcho() bool { return a.saturationDetector.saturatedEcho }

// updateCaptureSaturation mirrors UpdateCaptureSaturation.
func (a *aecState) updateCaptureSaturation(captureSignalSaturation bool) {
	a.captureSignalSaturation = captureSignalSaturation
}

// transparentModeActive mirrors TransparentModeActive.
func (a *aecState) transparentModeActive() bool {
	return a.transparentState != nil && a.transparentState.active()
}

// handleEchoPathChange mirrors HandleEchoPathChange.
func (a *aecState) handleEchoPathChange(variability echoPathVariability) {
	if variability.delayChange != delayAdjustmentNone {
		a.fullReset()
	} else if variability.gainChange {
		a.erleEstimator.reset(false)
	}
	a.subtractorOutputAnalyzer.handleEchoPathChange()
}

func (a *aecState) fullReset() {
	a.filterAnalyzer.reset()
	a.captureSignalSaturation = false
	a.strongNotSaturatedRenderBlocks = 0
	a.blocksWithActiveRender = 0
	a.initialState.reset()
	if a.transparentState != nil {
		a.transparentState.reset()
	}
	a.erleEstimator.reset(true)
	a.erlEstimator.reset()
	a.filterQualityState.reset()
}

// reverbDecay mirrors ReverbDecay: the decay factor for the echo
// reverberation, the default one or a milder one that can be used during
// nearend regions.
func (a *aecState) reverbDecay(mild bool) float32 { return a.reverbModelEstimator.reverbDecay(mild) }

// getReverbFrequencyResponse mirrors GetReverbFrequencyResponse: the
// frequency response of the reverberant echo.
func (a *aecState) getReverbFrequencyResponse() *[kFftLengthBy2Plus1]float32 {
	return a.reverbModelEstimator.getReverbFrequencyResponse()
}

// transitionTriggered mirrors TransitionTriggered: whether the
// transition out of the initial state has been triggered.
func (a *aecState) transitionTriggered() bool { return a.initialState.transitionTriggered }

// filterLengthBlocks mirrors FilterLengthBlocks.
func (a *aecState) filterLengthBlocks() int { return a.filterAnalyzer.filterLengthBlocks() }

// externalDelayBlocks mirrors ExternalDelayBlocks.
func (a *aecState) externalDelayBlocks() (delayEstimate, bool) {
	return a.delayState.externalDelay, a.delayState.hasExternalDelay
}

// detectActiveRender tells whether a channel of the render block is
// active.
func (a *aecState) detectActiveRender(alignedRenderBlock *block) bool {
	limit := a.config.renderLevels.activeRenderLimit
	for ch := range alignedRenderBlock.numChannels {
		if energy(alignedRenderBlock.view(0, ch)) > (limit*limit)*kFftLengthBy2 {
			return true
		}
	}

	return false
}

// update mirrors Update: it updates the state.
func (a *aecState) update(
	externalDelay delayEstimate,
	hasExternalDelay bool,
	adaptiveFilterFrequencyResponses [][][kFftLengthBy2Plus1]float32,
	adaptiveFilterImpulseResponses [][]float32,
	render *renderBuffer,
	e2Refined, y2 [][kFftLengthBy2Plus1]float32,
	outputs []subtractorOutput,
) {
	// Analyze the filter outputs and filters.
	anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged := a.subtractorOutputAnalyzer.update(outputs)
	anyFilterConsistent, maxEchoPathGain := a.filterAnalyzer.update(adaptiveFilterImpulseResponses, render)

	// Estimate the direct path delay of the filter.
	if a.config.filter.useLinearFilter {
		a.delayState.update(a.filterAnalyzer.getFilterDelaysBlocks(), externalDelay, hasExternalDelay,
			a.strongNotSaturatedRenderBlocks)
	}
	minDelay := a.delayState.minFilterDelay
	alignedRenderBlock := render.getBlock(-minDelay)

	// Update the render counters.
	activeRender := a.detectActiveRender(alignedRenderBlock)
	if activeRender {
		a.blocksWithActiveRender++
		if !a.saturatedCapture() {
			a.strongNotSaturatedRenderBlocks++
		}
	}

	var avgRenderSpectrumWithReverb [kFftLengthBy2Plus1]float32
	computeAvgRenderReverb(render.getSpectrumBuffer(), minDelay, a.reverbDecay(false), &a.avgRenderReverb,
		&avgRenderSpectrumWithReverb)

	if a.config.echoAudibility.useStationarityProperties {
		// Update the echo audibility evaluator.
		a.echoAudibility.update(render, a.avgRenderReverb.getReverb(), minDelay, a.delayState.hasExternalDelay)
	}

	// Update the ERL and ERLE measures.
	if a.initialState.transitionTriggered {
		a.erleEstimator.reset(false)
	}
	convergedFilters := a.subtractorOutputAnalyzer.convergedFilters()
	a.erleEstimator.update(render, adaptiveFilterFrequencyResponses, &avgRenderSpectrumWithReverb, y2, e2Refined,
		convergedFilters)
	a.erlEstimator.update(convergedFilters, render.spectrum(minDelay), y2)

	// Detect and flag echo saturation.
	if a.config.epStrength.echoCanSaturate {
		a.saturationDetector.update(alignedRenderBlock, a.saturatedCapture(), a.usableLinearEstimate(), outputs,
			maxEchoPathGain)
	}

	// Update the decision on whether to use the initial state parameter
	// set.
	a.initialState.update(activeRender, a.saturatedCapture())

	// Detect whether the transparent mode should be activated.
	if a.transparentState != nil {
		a.transparentState.update(minDelay, anyFilterConsistent, anyFilterConverged, anyCoarseFilterConverged,
			allFiltersDiverged, activeRender, a.saturatedCapture())
	}

	// Analyze the quality of the filter.
	a.filterQualityState.update(activeRender, a.transparentModeActive(), a.saturatedCapture(), hasExternalDelay,
		anyFilterConverged)

	// Update the reverb estimate.
	stationaryBlock := a.config.echoAudibility.useStationarityProperties && a.echoAudibility.isBlockStationary()
	a.reverbModelEstimator.update(a.filterAnalyzer.getAdjustedFilters(), adaptiveFilterFrequencyResponses,
		a.erleEstimator.getInstLinearQualityEstimates(), a.delayState.filterDelaysBlocks,
		a.filterQualityState.usableLinearFilterEstimates, stationaryBlock)
}
