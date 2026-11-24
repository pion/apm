// SPDX-FileCopyrightText: 2025 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import "github.com/pion/apm/internal/dsp/biquad"

// postFilterCoefficients48kHz matches kPostFilterCoefficients48kHz: it
// removes the frequencies above 19.5 kHz.
func postFilterCoefficients48kHz() []biquad.Coefficients {
	return []biquad.Coefficients{
		{B: [3]float32{0.56142156, 1.11499931, 0.56142156}, A: [2]float32{1.57914249, 0.63379496}},
		{B: [3]float32{1.00000000, 1.88944170, 1.00000000}, A: [2]float32{1.55130066, 0.68708719}},
		{B: [3]float32{1.00000000, 1.76057310, 1.00000000}, A: [2]float32{1.53001328, 0.78591224}},
		{B: [3]float32{1.00000000, 1.67448535, 1.00000000}, A: [2]float32{1.56506670, 0.92096576}},
	}
}

// postFilter mirrors PostFilter: it compensates for the artifacts of the
// prior processing, on the full band.
type postFilter struct {
	filters []*biquad.Cascade
}

// newPostFilterIfNeeded matches PostFilter::CreateIfNeeded: it returns
// nil when the sample rate needs no filter.
func newPostFilterIfNeeded(sampleRateHz, numChannels int) *postFilter {
	if sampleRateHz != 48000 {
		return nil
	}
	filter := &postFilter{filters: make([]*biquad.Cascade, numChannels)}
	for ch := range filter.filters {
		filter.filters[ch] = biquad.NewCascade(postFilterCoefficients48kHz())
	}

	return filter
}

// process filters every channel in place.
func (p *postFilter) process(channels [][]float32) {
	for ch, channel := range channels {
		p.filters[ch].Process(channel)
	}
}
