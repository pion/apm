// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// linearEchoPower matches LinearEchoPower: the power of the difference
// between the capture and the output of the linear filter.
func linearEchoPower(eFft, yFft *fftData, s2 *[kFftLengthBy2Plus1]float32) {
	for k := range s2 {
		s2[k] = (yFft.re[k]-eFft.re[k])*(yFft.re[k]-eFft.re[k]) + (yFft.im[k]-eFft.im[k])*(yFft.im[k]-eFft.im[k])
	}
}

// signalTransition matches SignalTransition: it fades between two input
// signals with a transition of fixed size. Upstream copies the target
// when both signals are the same one, which sameSignal tells here.
func signalTransition(from, to *[kBlockSize]float32, sameSignal bool, out *[kBlockSize]float32) {
	const (
		transitionSize             = 30
		oneByTransitionSizePlusOne = 1.0 / (transitionSize + 1)
	)
	if sameSignal {
		*out = *to

		return
	}

	for k := range transitionSize {
		a := float32(k+1) * oneByTransitionSizePlusOne
		out[k] = a*to[k] + (1-a)*from[k]
	}
	copy(out[transitionSize:], to[transitionSize:])
}

// windowedPaddedFft matches WindowedPaddedFft: it computes a padded FFT
// with a square root Hanning window and updates the related memory.
func windowedPaddedFft(fft *aec3Fft, v, vOld *[kBlockSize]float32, out *fftData) {
	fft.paddedFft(v, vOld, windowSqrtHanning, out)
	*vOld = *v
}

// useRefinedOutput matches UseRefinedOutput: it detects whether the
// output of the refined filter is more appropriate to use than the output
// of the coarse filter.
func useRefinedOutput(output *subtractorOutput) bool {
	// As the output of the refined adaptive filter generally should be
	// better than the coarse filter output, add a margin and a threshold
	// for choosing the coarse filter output.
	if output.e2Coarse < 0.9*output.e2Refined && output.y2 > 30*30*kBlockSize &&
		(output.s2Refined > 60*60*kBlockSize || output.s2Coarse > 60*60*kBlockSize) {
		return false
	}

	// If the refined filter is diverged, choose the filter output that has
	// the lowest power.
	if output.e2Coarse < output.e2Refined && output.y2 < output.e2Refined {
		return false
	}

	return true
}

// formLinearFilterOutput matches FormLinearFilterOutput: it forms the
// linear filter output by smoothly transitioning between the refined and
// the coarse filter outputs, according to which of the filters is to be
// used.
func formLinearFilterOutput(
	refinedFilterOutputLastSelected, useRefined bool, output *subtractorOutput, out *[kBlockSize]float32,
) {
	from, to := &output.eCoarse, &output.eCoarse
	if refinedFilterOutputLastSelected {
		from = &output.eRefined
	}
	if useRefined {
		to = &output.eRefined
	}
	signalTransition(from, to, refinedFilterOutputLastSelected == useRefined, out)
}

// echoRemover mirrors EchoRemoverImpl: it removes the echo from the
// capture signal.
//
// The neural residual echo estimator and the metrics that upstream
// reports to histograms are not ported.
type echoRemover struct {
	config                          echoCanceller3Config
	fft                             *aec3Fft
	numCaptureChannels              int
	useCoarseFilterOutput           bool
	subtractor                      *subtractor
	suppressionGain                 *suppressionGain
	cng                             *comfortNoiseGenerator
	suppressionFilter               *suppressionFilter
	renderSignalAnalyzer            *renderSignalAnalyzer
	residualEchoEstimator           *residualEchoEstimator
	captureOutputUsed               bool
	aecState                        *aecState
	eOld                            [][kBlockSize]float32
	yOld                            [][kBlockSize]float32
	blockCounter                    int
	gainChangeHangover              int
	refinedFilterOutputLastSelected bool

	// The data of the capture channels of the current block.
	e                    [][kBlockSize]float32
	y2                   [][kFftLengthBy2Plus1]float32
	e2                   [][kFftLengthBy2Plus1]float32
	r2                   [][kFftLengthBy2Plus1]float32
	r2Unbounded          [][kFftLengthBy2Plus1]float32
	s2Linear             [][kFftLengthBy2Plus1]float32
	yFft                 []fftData
	eFft                 []fftData
	comfortNoise         []fftData
	highBandComfortNoise []fftData
	subtractorOutput     []subtractorOutput
}

// newEchoRemover mirrors EchoRemover::Create.
func newEchoRemover(
	config echoCanceller3Config, sampleRateHz, numRenderChannels, numCaptureChannels int,
) *echoRemover {
	spectra := func() [][kFftLengthBy2Plus1]float32 {
		return make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	}

	return &echoRemover{
		config:                          config,
		fft:                             newAec3Fft(),
		numCaptureChannels:              numCaptureChannels,
		useCoarseFilterOutput:           config.filter.enableCoarseFilterOutputUsage,
		subtractor:                      newSubtractor(config, numRenderChannels, numCaptureChannels),
		suppressionGain:                 newSuppressionGain(config, numCaptureChannels),
		cng:                             newComfortNoiseGenerator(config, numCaptureChannels),
		suppressionFilter:               newSuppressionFilter(sampleRateHz, numCaptureChannels),
		renderSignalAnalyzer:            newRenderSignalAnalyzer(config),
		residualEchoEstimator:           newResidualEchoEstimator(config, numRenderChannels),
		captureOutputUsed:               true,
		aecState:                        newAecState(config, numCaptureChannels),
		eOld:                            make([][kBlockSize]float32, numCaptureChannels),
		yOld:                            make([][kBlockSize]float32, numCaptureChannels),
		refinedFilterOutputLastSelected: true,
		e:                               make([][kBlockSize]float32, numCaptureChannels),
		y2:                              spectra(),
		e2:                              spectra(),
		r2:                              spectra(),
		r2Unbounded:                     spectra(),
		s2Linear:                        spectra(),
		yFft:                            make([]fftData, numCaptureChannels),
		eFft:                            make([]fftData, numCaptureChannels),
		comfortNoise:                    make([]fftData, numCaptureChannels),
		highBandComfortNoise:            make([]fftData, numCaptureChannels),
		subtractorOutput:                make([]subtractorOutput, numCaptureChannels),
	}
}

// getMetrics mirrors GetMetrics: the echo return loss and the echo
// return loss enhancement, in dB.
func (r *echoRemover) getMetrics() (echoReturnLoss, echoReturnLossEnhancement float64) {
	// The echo return loss is inverted to go from gain to attenuation.
	echoReturnLoss = -10 * math.Log10(float64(r.aecState.erlTimeDomain()))
	echoReturnLossEnhancement = float64(log2TodB(r.aecState.fullBandErleLog2()))

	return echoReturnLoss, echoReturnLossEnhancement
}

// setCaptureOutputUsage mirrors SetCaptureOutputUsage: it tells whether
// the capture output will be used, so that some of the processing can be
// skipped when it is not, for instance when the endpoint is muted.
func (r *echoRemover) setCaptureOutputUsage(captureOutputUsed bool) {
	r.captureOutputUsed = captureOutputUsed
}

// handleEchoPathChange acts on a change of the echo path. The gain
// change is only acted on once per frame.
func (r *echoRemover) handleEchoPathChange(variability echoPathVariability) {
	const maxBlocksPerFrame = 3
	if variability.gainChange {
		if r.gainChangeHangover == 0 {
			r.gainChangeHangover = maxBlocksPerFrame
		} else {
			variability.gainChange = false
		}
	}

	r.subtractor.handleEchoPathChange(variability)
	r.aecState.handleEchoPathChange(variability)

	if variability.delayChange != delayAdjustmentNone {
		r.suppressionGain.setInitialState(true)
	}
}

// computeSpectra forms the linear filter output and computes the spectra
// of the capture channels.
func (r *echoRemover) computeSpectra(capture *block) {
	useRefined := true
	if r.useCoarseFilterOutput {
		useRefined = false
		for ch := range r.subtractorOutput {
			if useRefinedOutput(&r.subtractorOutput[ch]) {
				useRefined = true

				break
			}
		}
	}

	for ch := range r.numCaptureChannels {
		formLinearFilterOutput(r.refinedFilterOutputLastSelected, useRefined, &r.subtractorOutput[ch], &r.e[ch])
		windowedPaddedFft(r.fft, (*[kBlockSize]float32)(capture.view(0, ch)), &r.yOld[ch], &r.yFft[ch])
		windowedPaddedFft(r.fft, &r.e[ch], &r.eOld[ch], &r.eFft[ch])
		linearEchoPower(&r.eFft[ch], &r.yFft[ch], &r.s2Linear[ch])
		r.yFft[ch].spectrum(&r.y2[ch])
		r.eFft[ch].spectrum(&r.e2[ch])
	}
	r.refinedFilterOutputLastSelected = useRefined
}

// processCapture mirrors ProcessCapture: it removes the echo from a block
// of the capture signal. The render signal is assumed to be aligned with
// the capture signal. linearOutput receives the output of the linear
// filter when it is not nil.
func (r *echoRemover) processCapture(
	variability echoPathVariability,
	captureSignalSaturation bool,
	externalDelay delayEstimate,
	hasExternalDelay bool,
	render *renderBuffer,
	linearOutput, capture *block,
) {
	r.blockCounter++
	renderBlock := render.getBlock(0)

	r.aecState.updateCaptureSaturation(captureSignalSaturation)

	if variability.audioPathChanged() {
		r.handleEchoPathChange(variability)
	}
	if r.gainChangeHangover > 0 {
		r.gainChangeHangover--
	}

	// Analyze the render signal.
	r.renderSignalAnalyzer.update(render, r.aecState.minDirectPathFilterDelay(), true)

	// State transition.
	if r.aecState.transitionTriggered() {
		r.subtractor.exitInitialState()
		r.suppressionGain.setInitialState(false)
	}

	// Perform the linear echo cancellation.
	r.subtractor.process(render, capture, r.renderSignalAnalyzer, r.aecState.saturatedCapture(), r.subtractorOutput)

	// Compute the spectra.
	r.computeSpectra(capture)
	nearendSpectrum := r.y2
	if r.aecState.usableLinearEstimate() {
		nearendSpectrum = r.e2
	}

	// Optionally return the linear filter output.
	if linearOutput != nil {
		for ch := range r.numCaptureChannels {
			copy(linearOutput.view(0, ch), r.e[ch][:])
		}
	}

	// Update the AEC state information.
	r.aecState.update(externalDelay, hasExternalDelay, r.subtractor.filterFrequencyResponses(),
		r.subtractor.filterImpulseResponses(), render, r.e2, r.y2, r.subtractorOutput)

	// Choose the linear output.
	captureFft := r.yFft
	if r.aecState.useLinearFilterOutput() {
		captureFft = r.eFft
	}

	// Estimate the comfort noise.
	r.cng.compute(r.aecState.saturatedCapture(), nearendSpectrum, r.comfortNoise, r.highBandComfortNoise)

	// Only do the below processing if the output of the audio processing
	// module is used.
	if !r.captureOutputUsed {
		return
	}

	r.suppress(variability, render, renderBlock, nearendSpectrum, captureFft, capture)
}

// suppress estimates the residual echo, computes the suppression gains
// and applies them to the capture signal.
func (r *echoRemover) suppress(
	variability echoPathVariability,
	render *renderBuffer,
	renderBlock *block,
	nearendSpectrum [][kFftLengthBy2Plus1]float32,
	captureFft []fftData,
	capture *block,
) {
	// Estimate the residual echo power.
	r.residualEchoEstimator.estimate(r.aecState, render, r.s2Linear, r.y2, r.suppressionGain.isDominantNearend(),
		r.r2, r.r2Unbounded)

	// Suppressor nearend and echo estimates.
	echoSpectrum := r.r2
	if r.aecState.usableLinearEstimate() {
		// E2 is bound by Y2.
		for ch := range r.e2 {
			for k := range r.e2[ch] {
				r.e2[ch][k] = min(r.e2[ch][k], r.y2[ch][k])
			}
		}
		echoSpectrum = r.s2Linear
	}

	// Determine if the suppressor should assume clock drift.
	clockDrift := r.config.echoRemovalControl.hasClockDrift || variability.clockDrift

	// Compute the preferred gains.
	var gain [kFftLengthBy2Plus1]float32
	highBandsGain := r.suppressionGain.getGain(r.config.suppressor, false, nearendSpectrum, echoSpectrum, r.r2,
		r.r2Unbounded, r.cng.noiseSpectrum(), r.renderSignalAnalyzer, r.aecState, renderBlock, clockDrift, &gain)

	r.suppressionFilter.applyGain(r.comfortNoise, r.highBandComfortNoise, &gain, highBandsGain, captureFft, capture)
}
