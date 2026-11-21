// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const defaultTransparentModeGain = 0.01

// getRenderIndexesToAnalyze matches GetRenderIndexesToAnalyze: the
// indexes that are used for computing the spectral power over the blocks
// surrounding the delay.
func getRenderIndexesToAnalyze(
	spectrumBuf *spectrumBuffer, echoModel echoModelConfig, filterDelayBlocks int,
) (idxStart, idxStop int) {
	windowStart := max(0, filterDelayBlocks-echoModel.renderPreWindowSize)
	windowEnd := filterDelayBlocks + echoModel.renderPostWindowSize

	return spectrumBuf.offsetIndex(spectrumBuf.read, windowStart), spectrumBuf.offsetIndex(spectrumBuf.read, windowEnd+1)
}

// linearEstimate matches LinearEstimate: it estimates the residual echo
// power based on the echo return loss enhancement and the linear power
// estimate.
func linearEstimate(s2Linear, erle, r2 [][kFftLengthBy2Plus1]float32) {
	for ch := range r2 {
		for k := range r2[ch] {
			r2[ch][k] = s2Linear[ch][k] / erle[ch][k]
		}
	}
}

// nonLinearEstimate matches NonLinearEstimate: it estimates the residual
// echo power based on the estimate of the echo path gain.
func nonLinearEstimate(echoPathGain float32, x2 *[kFftLengthBy2Plus1]float32, r2 [][kFftLengthBy2Plus1]float32) {
	for ch := range r2 {
		for k := range r2[ch] {
			r2[ch][k] = x2[k] * echoPathGain
		}
	}
}

// applyNoiseGate matches ApplyNoiseGate: it applies a soft noise gate to
// the echo generating power.
func applyNoiseGate(config echoModelConfig, x2 *[kFftLengthBy2Plus1]float32) {
	for k := range x2 {
		if config.noiseGatePower > x2[k] {
			x2[k] = max(0, x2[k]-config.noiseGateSlope*(config.noiseGatePower-x2[k]))
		}
	}
}

// sumChannels adds the spectra of the render channels.
func sumChannels(spectra [][kFftLengthBy2Plus1]float32) [kFftLengthBy2Plus1]float32 {
	var power [kFftLengthBy2Plus1]float32
	for ch := range spectra {
		for k := range power {
			power[k] += spectra[ch][k]
		}
	}

	return power
}

// echoGeneratingPower matches EchoGeneratingPower: it estimates the echo
// generating signal power as the maximal power over a time window.
func echoGeneratingPower(
	numRenderChannels int,
	spectrumBuf *spectrumBuffer,
	echoModel echoModelConfig,
	filterDelayBlocks int,
	x2 *[kFftLengthBy2Plus1]float32,
) {
	idxStart, idxStop := getRenderIndexesToAnalyze(spectrumBuf, echoModel, filterDelayBlocks)

	*x2 = [kFftLengthBy2Plus1]float32{}
	for idx := idxStart; idx != idxStop; idx = spectrumBuf.incIndex(idx) {
		renderPower := &spectrumBuf.buffer[idx][0]
		if numRenderChannels > 1 {
			sum := sumChannels(spectrumBuf.buffer[idx])
			renderPower = &sum
		}
		for k := range x2 {
			x2[k] = max(x2[k], renderPower[k])
		}
	}
}

// residualEchoEstimator mirrors ResidualEchoEstimator: it estimates the
// power of the echo that remains after the linear filter.
//
// The neural residual echo estimator that upstream can run instead is not
// ported, and neither are the field trials that lower the default gains.
type residualEchoEstimator struct {
	config                                 echoCanceller3Config
	numRenderChannels                      int
	earlyReflectionsTransparentModeGain    float32
	lateReflectionsTransparentModeGain     float32
	earlyReflectionsGeneralGain            float32
	lateReflectionsGeneralGain             float32
	erleOnsetCompensationInDominantNearend bool
	x2NoiseFloor                           [kFftLengthBy2Plus1]float32
	x2NoiseFloorCounter                    [kFftLengthBy2Plus1]int
	echoReverb                             reverbModel
}

func newResidualEchoEstimator(config echoCanceller3Config, numRenderChannels int) *residualEchoEstimator {
	estimator := &residualEchoEstimator{
		config:                                 config,
		numRenderChannels:                      numRenderChannels,
		earlyReflectionsTransparentModeGain:    defaultTransparentModeGain,
		lateReflectionsTransparentModeGain:     defaultTransparentModeGain,
		earlyReflectionsGeneralGain:            config.epStrength.defaultGain,
		lateReflectionsGeneralGain:             config.epStrength.defaultGain,
		erleOnsetCompensationInDominantNearend: config.epStrength.erleOnsetCompensationInDominantNearend,
	}
	estimator.reset()

	return estimator
}

// estimate mirrors Estimate: it writes the residual echo power to r2 and
// the one based on the ERLE that is not capped to r2Unbounded.
func (r *residualEchoEstimator) estimate(
	state *aecState,
	render *renderBuffer,
	s2Linear, y2 [][kFftLengthBy2Plus1]float32,
	dominantNearend bool,
	r2, r2Unbounded [][kFftLengthBy2Plus1]float32,
) {
	// Estimate the power of the stationary noise in the render signal.
	r.updateRenderNoisePower(render)

	if state.usableLinearEstimate() {
		r.estimateLinear(state, render, s2Linear, y2, dominantNearend, r2, r2Unbounded)
	} else {
		r.estimateNonLinear(state, render, y2, dominantNearend, r2, r2Unbounded)
	}

	if state.useStationarityProperties() {
		// Scale the echo according to the echo audibility.
		var residualScaling [kFftLengthBy2Plus1]float32
		state.getResidualEchoScaling(residualScaling[:])
		for ch := range r2 {
			for k := range residualScaling {
				r2[ch][k] *= residualScaling[k]
				r2Unbounded[ch][k] *= residualScaling[k]
			}
		}
	}
}

// estimateLinear estimates the residual echo power when the linear
// filter is usable.
func (r *residualEchoEstimator) estimateLinear(
	state *aecState,
	render *renderBuffer,
	s2Linear, y2 [][kFftLengthBy2Plus1]float32,
	dominantNearend bool,
	r2, r2Unbounded [][kFftLengthBy2Plus1]float32,
) {
	if state.saturatedEcho() {
		// When there is saturated echo, assume the same spectral content as
		// is present in the microphone signal.
		copy(r2, y2)
		copy(r2Unbounded, y2)
	} else {
		onsetCompensated := r.erleOnsetCompensationInDominantNearend || !dominantNearend
		linearEstimate(s2Linear, state.erle(onsetCompensated), r2)
		linearEstimate(s2Linear, state.erleUnbounded(), r2Unbounded)
	}

	r.updateReverb(true, state, render, dominantNearend)
	r.addReverb(r2)
	r.addReverb(r2Unbounded)
}

// estimateNonLinear estimates the residual echo power when the linear
// filter is not usable.
func (r *residualEchoEstimator) estimateNonLinear(
	state *aecState,
	render *renderBuffer,
	y2 [][kFftLengthBy2Plus1]float32,
	dominantNearend bool,
	r2, r2Unbounded [][kFftLengthBy2Plus1]float32,
) {
	echoPathGain := r.getEchoPathGain(state, true)

	if state.saturatedEcho() {
		// When there is saturated echo, assume the same spectral content as
		// is present in the microphone signal.
		copy(r2, y2)
		copy(r2Unbounded, y2)
	} else {
		// Estimate the echo generating signal power.
		var x2 [kFftLengthBy2Plus1]float32
		echoGeneratingPower(r.numRenderChannels, render.getSpectrumBuffer(), r.config.echoModel,
			state.minDirectPathFilterDelay(), &x2)
		if !state.useStationarityProperties() {
			applyNoiseGate(r.config.echoModel, &x2)
		}

		// Subtract the stationary noise power to avoid stationary noise
		// causing excessive echo suppression.
		for k := range x2 {
			x2[k] -= r.config.echoModel.stationaryGateSlope * r.x2NoiseFloor[k]
			x2[k] = max(0, x2[k])
		}

		nonLinearEstimate(echoPathGain, &x2, r2)
		nonLinearEstimate(echoPathGain, &x2, r2Unbounded)
	}

	if r.config.echoModel.modelReverbInNonlinearMode && !state.transparentModeActive() {
		r.updateReverb(false, state, render, dominantNearend)
		r.addReverb(r2)
		r.addReverb(r2Unbounded)
	}
}

// reset mirrors Reset.
func (r *residualEchoEstimator) reset() {
	r.echoReverb.reset()
	for k := range r.x2NoiseFloor {
		r.x2NoiseFloorCounter[k] = r.config.echoModel.noiseFloorHold
		r.x2NoiseFloor[k] = r.config.echoModel.minNoiseFloorPower
	}
}

// renderPower returns the power of the render signal of one of the FFTs
// in the buffer, added over the channels.
func (r *residualEchoEstimator) renderPower(render *renderBuffer, bufferOffsetFfts int) [kFftLengthBy2Plus1]float32 {
	x2 := render.spectrum(bufferOffsetFfts)
	if r.numRenderChannels > 1 {
		return sumChannels(x2[:r.numRenderChannels])
	}

	return x2[0]
}

// updateRenderNoisePower mirrors UpdateRenderNoisePower: it updates the
// estimate of the power of the stationary noise component in the render
// signal, in a minimum statistics manner.
func (r *residualEchoEstimator) updateRenderNoisePower(render *renderBuffer) {
	renderPower := r.renderPower(render, 0)

	for k, power := range renderPower {
		switch {
		case power < r.x2NoiseFloor[k]:
			// Decrease rapidly.
			r.x2NoiseFloor[k] = power
			r.x2NoiseFloorCounter[k] = 0
		case r.x2NoiseFloorCounter[k] >= r.config.echoModel.noiseFloorHold:
			// Increase in a delayed, leaky manner.
			r.x2NoiseFloor[k] = max(r.x2NoiseFloor[k]*1.1, r.config.echoModel.minNoiseFloorPower)
		default:
			r.x2NoiseFloorCounter[k]++
		}
	}
}

// updateReverb mirrors UpdateReverb: it updates the reverb estimation,
// for the linear or the nonlinear echo power model.
func (r *residualEchoEstimator) updateReverb(linear bool, state *aecState, render *renderBuffer, dominantNearend bool) {
	// Choose the reverb partition based on the type of echo power model.
	firstReverbPartition := state.minDirectPathFilterDelay() + 1
	if linear {
		firstReverbPartition = state.filterLengthBlocks() + 1
	}

	// Compute the render power for the reverb.
	renderPower := r.renderPower(render, firstReverbPartition)

	// Update the reverb estimate.
	reverbDecay := state.reverbDecay(dominantNearend)
	if linear {
		r.echoReverb.updateReverb(renderPower[:], state.getReverbFrequencyResponse()[:], reverbDecay)
	} else {
		r.echoReverb.updateReverbNoFreqShaping(renderPower[:], r.getEchoPathGain(state, false), reverbDecay)
	}
}

// addReverb mirrors AddReverb: it adds the estimated power of the reverb
// to the residual echo power.
func (r *residualEchoEstimator) addReverb(r2 [][kFftLengthBy2Plus1]float32) {
	reverbPower := r.echoReverb.getReverb()
	for ch := range r2 {
		for k := range reverbPower {
			r2[ch][k] += reverbPower[k]
		}
	}
}

// getEchoPathGain mirrors GetEchoPathGain: the echo path gain to apply.
func (r *residualEchoEstimator) getEchoPathGain(state *aecState, gainForEarlyReflections bool) float32 {
	gainAmplitude := r.lateReflectionsGeneralGain
	switch {
	case state.transparentModeActive() && gainForEarlyReflections:
		gainAmplitude = r.earlyReflectionsTransparentModeGain
	case state.transparentModeActive():
		gainAmplitude = r.lateReflectionsTransparentModeGain
	case gainForEarlyReflections:
		gainAmplitude = r.earlyReflectionsGeneralGain
	}

	return gainAmplitude * gainAmplitude
}
