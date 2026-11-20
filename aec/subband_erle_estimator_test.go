// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// assertErleSpectra compares the spectra of every channel with the next
// lines of the reference that have this name.
func assertErleSpectra(
	t *testing.T, reference *referenceCursor, name string, spectra [][kFftLengthBy2Plus1]float32, index int,
) {
	t.Helper()
	for ch := range spectra {
		require.InDeltaSlice(t, reference.next(name), spectra[ch][:], erleReferenceDelta,
			"%s, block %d, channel %d", name, index, ch)
	}
}

// TestSubbandErleEstimator_MatchesUpstream compares the ERLE estimates
// with the upstream SubbandErleEstimator at the pinned commit. The
// estimator is reset at block 850.
func TestSubbandErleEstimator_MatchesUpstream(t *testing.T) {
	for _, tc := range erleScenarioCases() {
		suffix := erleScenarioSuffix(tc.numRender, tc.numCapture)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/subband_erle_estimator_upstream.txt", suffix)
			scenario := newErleScenario(tc.numRender, tc.numCapture)
			estimator := newSubbandErleEstimator(defaultEchoCanceller3Config(), tc.numCapture)

			for index := range erleScenarioBlocks {
				scenario.step(index)
				estimator.update(&scenario.x2, scenario.y2, scenario.e2, scenario.converged)

				if erleScenarioDumpsSpectra(index) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					assertErleSpectra(t, reference, "subband", estimator.getErle(false), index)
					assertErleSpectra(t, reference, "subband_onset_compensated", estimator.getErle(true), index)
					assertErleSpectra(t, reference, "subband_unbounded", estimator.getErleUnbounded(), index)
					assertErleSpectra(t, reference, "subband_during_onsets", estimator.getErleDuringOnsets(), index)
				}
				if index == erleScenarioResetBlock {
					estimator.reset()
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}

// TestSubbandErleEstimator_ErleDuringOnsetsMatchesUpstream compares the
// ERLE estimated during onsets with the upstream SubbandErleEstimator
// when the field trial WebRTC-Aec3MinErleDuringOnsetsKillSwitch is
// enabled.
func TestSubbandErleEstimator_ErleDuringOnsetsMatchesUpstream(t *testing.T) {
	reference := newReferenceCursor(t, "testdata/subband_erle_estimator_upstream.txt", " 2 2 trial")
	scenario := newErleScenario(2, 2)
	estimator := newSubbandErleEstimator(defaultEchoCanceller3Config(), 2)
	estimator.useMinErleDuringOnsets = false

	for index := range erleScenarioBlocks {
		scenario.step(index)
		estimator.update(&scenario.x2, scenario.y2, scenario.e2, scenario.converged)

		if index%50 == 49 || (index >= 690 && index < 760 && index%5 == 0) {
			require.Equal(t, []float32{float32(index)}, reference.next("step"))
			assertErleSpectra(t, reference, "subband_onset_compensated", estimator.getErle(true), index)
			assertErleSpectra(t, reference, "subband_during_onsets", estimator.getErleDuringOnsets(), index)
		}
		if index == erleScenarioResetBlock {
			estimator.reset()
		}
	}
	require.Empty(t, reference.entries)
}
