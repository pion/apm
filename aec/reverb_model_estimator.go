// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// reverbModelEstimator mirrors ReverbModelEstimator: it estimates the
// model parameters for the reverberant echo.
type reverbModelEstimator struct {
	reverbDecayEstimators    []*reverbDecayEstimator
	reverbFrequencyResponses []reverbFrequencyResponse
}

func newReverbModelEstimator(config echoCanceller3Config, numCaptureChannels int) *reverbModelEstimator {
	estimator := &reverbModelEstimator{
		reverbDecayEstimators:    make([]*reverbDecayEstimator, numCaptureChannels),
		reverbFrequencyResponses: make([]reverbFrequencyResponse, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		estimator.reverbDecayEstimators[ch] = newReverbDecayEstimator(config)
		estimator.reverbFrequencyResponses[ch] = newReverbFrequencyResponse(
			config.epStrength.useConservativeTailFrequencyResponse)
	}

	return estimator
}

// update mirrors Update: it updates the estimates based on new data.
func (r *reverbModelEstimator) update(
	impulseResponses [][]float32,
	frequencyResponses [][][kFftLengthBy2Plus1]float32,
	linearFilterQualities []optionalFloat32,
	filterDelaysBlocks []int,
	usableLinearEstimates []bool,
	stationaryBlock bool,
) {
	for ch := range r.reverbDecayEstimators {
		// Estimate the frequency response for the reverb.
		r.reverbFrequencyResponses[ch].update(
			frequencyResponses[ch], filterDelaysBlocks[ch], linearFilterQualities[ch], stationaryBlock)

		// Estimate the reverb decay.
		r.reverbDecayEstimators[ch].update(
			impulseResponses[ch], linearFilterQualities[ch], filterDelaysBlocks[ch], usableLinearEstimates[ch],
			stationaryBlock)
	}
}

// reverbDecay mirrors ReverbDecay: the exponential decay of the
// reverberant echo, the default one or a milder one. Like upstream, it is
// the one of the first channel.
func (r *reverbModelEstimator) reverbDecay(mild bool) float32 {
	return r.reverbDecayEstimators[0].getDecay(mild)
}

// getReverbFrequencyResponse mirrors GetReverbFrequencyResponse: the
// frequency response of the reverberant echo. Like upstream, it is the
// one of the first channel.
func (r *reverbModelEstimator) getReverbFrequencyResponse() *[kFftLengthBy2Plus1]float32 {
	return r.reverbFrequencyResponses[0].frequencyResponse()
}
