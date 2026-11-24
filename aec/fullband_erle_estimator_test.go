// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// erleReferenceDelta bounds the difference to the upstream estimates.
const erleReferenceDelta = 1e-6

// optionalValue returns the value of an optional, or -1 when it has none.
func optionalValue(optional optionalFloat32) float32 {
	if !optional.valid {
		return -1
	}

	return optional.value
}

// TestFullBandErleEstimator_MatchesUpstream compares the fullband ERLE
// and the quality estimates of every block with the upstream
// FullBandErleEstimator at the pinned commit. The estimator is reset at
// block 850.
func TestFullBandErleEstimator_MatchesUpstream(t *testing.T) {
	for _, tc := range erleScenarioCases() {
		suffix := erleScenarioSuffix(tc.numRender, tc.numCapture)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/fullband_erle_estimator_upstream.txt", suffix)
			scenario := newErleScenario(tc.numRender, tc.numCapture)
			estimator := newFullBandErleEstimator(defaultErleConfig(), tc.numCapture)

			for index := range erleScenarioBlocks {
				scenario.step(index)
				estimator.update(scenario.x2[:], scenario.y2, scenario.e2, scenario.converged)

				// The block index, the ERLE, then the quality of every
				// filter.
				got := []float32{float32(index), estimator.fullbandErleLog2()}
				for _, quality := range estimator.getInstLinearQualityEstimates() {
					got = append(got, optionalValue(quality))
				}
				require.InDeltaSlice(t, reference.next("fullband"), got, erleReferenceDelta, "block %d", index)

				if index == erleScenarioResetBlock {
					estimator.reset()
				}
			}
		})
	}
}
