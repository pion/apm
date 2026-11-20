// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// averageDecayWithinFilter matches AverageDecayWithinFilter: the ratio of
// the energies between the direct path and the tail. The energy is
// computed in the power spectrum domain, discarding the DC.
func averageDecayWithinFilter(freqRespDirectPath, freqRespTail *[kFftLengthBy2Plus1]float32) float32 {
	const skipBins = 1
	directPathEnergy := sum(freqRespDirectPath[skipBins:])
	if directPathEnergy == 0 {
		return 0
	}

	return sum(freqRespTail[skipBins:]) / directPathEnergy
}

// reverbFrequencyResponse mirrors ReverbFrequencyResponse: it updates the
// frequency response for the reverb.
type reverbFrequencyResponse struct {
	useConservativeTailFrequencyResponse bool
	averageDecay                         float32
	tailResponse                         [kFftLengthBy2Plus1]float32
}

func newReverbFrequencyResponse(useConservativeTailFrequencyResponse bool) reverbFrequencyResponse {
	return reverbFrequencyResponse{useConservativeTailFrequencyResponse: useConservativeTailFrequencyResponse}
}

// update mirrors Update: it updates the frequency response estimate of
// the reverb.
func (r *reverbFrequencyResponse) update(
	frequencyResponse [][kFftLengthBy2Plus1]float32,
	filterDelayBlocks int,
	linearFilterQuality optionalFloat32,
	stationaryBlock bool,
) {
	if stationaryBlock || !linearFilterQuality.valid {
		return
	}

	freqRespTail := &frequencyResponse[len(frequencyResponse)-1]
	freqRespDirectPath := &frequencyResponse[filterDelayBlocks]

	averageDecay := averageDecayWithinFilter(freqRespDirectPath, freqRespTail)

	smoothing := 0.2 * linearFilterQuality.value
	r.averageDecay += smoothing * (averageDecay - r.averageDecay)

	for k := range r.tailResponse {
		r.tailResponse[k] = freqRespDirectPath[k] * r.averageDecay
	}

	if r.useConservativeTailFrequencyResponse {
		for k := range r.tailResponse {
			r.tailResponse[k] = max(freqRespTail[k], r.tailResponse[k])
		}
	}

	for k := 1; k < kFftLengthBy2; k++ {
		avgNeighbour := 0.5 * (r.tailResponse[k-1] + r.tailResponse[k+1])
		r.tailResponse[k] = max(r.tailResponse[k], avgNeighbour)
	}
}

// frequencyResponse mirrors FrequencyResponse: the estimated frequency
// response for the reverb.
func (r *reverbFrequencyResponse) frequencyResponse() *[kFftLengthBy2Plus1]float32 {
	return &r.tailResponse
}
