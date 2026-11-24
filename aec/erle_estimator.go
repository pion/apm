// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// erleEstimator mirrors ErleEstimator: it estimates the echo return loss
// enhancement, per subband and over all the subbands.
type erleEstimator struct {
	startupPhaseLengthBlocks     int
	fullbandErleEstimator        *fullBandErleEstimator
	subbandErleEstimator         *subbandErleEstimator
	signalDependentErleEstimator *signalDependentErleEstimator
	blocksSinceReset             int
}

func newErleEstimator(
	startupPhaseLengthBlocks int, config echoCanceller3Config, numCaptureChannels int,
) *erleEstimator {
	estimator := &erleEstimator{
		startupPhaseLengthBlocks: startupPhaseLengthBlocks,
		fullbandErleEstimator:    newFullBandErleEstimator(config.erle, numCaptureChannels),
		subbandErleEstimator:     newSubbandErleEstimator(config, numCaptureChannels),
	}
	if config.erle.numSections > 1 {
		estimator.signalDependentErleEstimator = newSignalDependentErleEstimator(config, numCaptureChannels)
	}
	estimator.reset(true)

	return estimator
}

// reset mirrors Reset: it resets the fullband and the subband ERLE
// estimators.
func (e *erleEstimator) reset(delayChange bool) {
	e.fullbandErleEstimator.reset()
	e.subbandErleEstimator.reset()
	if e.signalDependentErleEstimator != nil {
		e.signalDependentErleEstimator.reset()
	}
	if delayChange {
		e.blocksSinceReset = 0
	}
}

// update mirrors Update: it updates the ERLE estimates.
func (e *erleEstimator) update(
	render *renderBuffer,
	filterFrequencyResponses [][][kFftLengthBy2Plus1]float32,
	avgRenderSpectrumWithReverb *[kFftLengthBy2Plus1]float32,
	captureSpectra, subtractorSpectra [][kFftLengthBy2Plus1]float32,
	convergedFilters []bool,
) {
	x2Reverb := avgRenderSpectrumWithReverb
	y2 := captureSpectra
	e2 := subtractorSpectra

	e.blocksSinceReset++
	if e.blocksSinceReset < e.startupPhaseLengthBlocks {
		return
	}

	e.subbandErleEstimator.update(x2Reverb, y2, e2, convergedFilters)

	if e.signalDependentErleEstimator != nil {
		e.signalDependentErleEstimator.update(render, filterFrequencyResponses, x2Reverb, y2, e2,
			e.subbandErleEstimator.getErle(false), e.subbandErleEstimator.getErle(true), convergedFilters)
	}

	e.fullbandErleEstimator.update(x2Reverb[:], y2, e2, convergedFilters)
}

// getErle mirrors Erle: the most recent subband ERLE estimates.
func (e *erleEstimator) getErle(onsetCompensated bool) [][kFftLengthBy2Plus1]float32 {
	if e.signalDependentErleEstimator != nil {
		return e.signalDependentErleEstimator.getErle(onsetCompensated)
	}

	return e.subbandErleEstimator.getErle(onsetCompensated)
}

// getErleUnbounded mirrors ErleUnbounded: the subband ERLE that is not
// capped. It is only available with the subband ERLE estimator, where
// the ERLE is often capped at low values. With the signal dependent ERLE
// estimator the capped ERLE is returned.
func (e *erleEstimator) getErleUnbounded() [][kFftLengthBy2Plus1]float32 {
	if e.signalDependentErleEstimator != nil {
		return e.signalDependentErleEstimator.getErle(false)
	}

	return e.subbandErleEstimator.getErleUnbounded()
}

// getErleDuringOnsets mirrors ErleDuringOnsets: the subband ERLE that is
// estimated during onsets.
func (e *erleEstimator) getErleDuringOnsets() [][kFftLengthBy2Plus1]float32 {
	return e.subbandErleEstimator.getErleDuringOnsets()
}

// fullbandErleLog2 mirrors FullbandErleLog2: the fullband ERLE estimate.
func (e *erleEstimator) fullbandErleLog2() float32 {
	return e.fullbandErleEstimator.fullbandErleLog2()
}

// getInstLinearQualityEstimates mirrors GetInstLinearQualityEstimates:
// an estimation of the current linear filter quality based on the
// current and past fullband ERLE estimates. A value of 1 means that the
// linear filter is reaching its maximum subtraction performance.
func (e *erleEstimator) getInstLinearQualityEstimates() []optionalFloat32 {
	return e.fullbandErleEstimator.getInstLinearQualityEstimates()
}
