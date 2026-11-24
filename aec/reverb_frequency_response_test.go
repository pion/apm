// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// reverbReferenceDelta bounds the difference to the upstream estimates.
const reverbReferenceDelta = 1e-6

// TestReverbFrequencyResponse_MatchesUpstream compares the frequency
// response of the reverb with the upstream ReverbFrequencyResponse at
// the pinned commit, with and without the conservative tail response.
func TestReverbFrequencyResponse_MatchesUpstream(t *testing.T) {
	suffix := reverbScenarioSuffix(13, 0.83, "")
	reference := newReferenceCursor(t, "testdata/reverb_frequency_response_upstream.txt", suffix)
	scenario := newReverbScenario(13, 0.83, "")
	conservative := newReverbFrequencyResponse(true)
	plain := newReverbFrequencyResponse(false)

	for index := range reverbScenarioUpdates {
		scenario.step(index)
		for _, response := range []*reverbFrequencyResponse{&conservative, &plain} {
			response.update(scenario.h2[0], scenario.delays[0], scenario.qualities[0], scenario.stationary)
		}

		if reverbScenarioDumpsSpectra(index) {
			require.Equal(t, []float32{float32(index)}, reference.next("step"))
			require.InDeltaSlice(t, reference.next("frequency_response"), conservative.frequencyResponse()[:],
				reverbReferenceDelta, "update %d", index)
			require.InDeltaSlice(t, reference.next("frequency_response_plain"), plain.frequencyResponse()[:],
				reverbReferenceDelta, "update %d", index)
		}
	}
	require.Empty(t, reference.entries)
}
