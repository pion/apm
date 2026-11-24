// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/require"
)

// reverbScenarioRun is a run of the reference: a filter length, a default
// decay and a variant. A negative default decay enables the estimation.
type reverbScenarioRun struct {
	lengthBlocks int
	defaultLen   float32
	variant      string
}

func reverbScenarioRuns() []reverbScenarioRun {
	return []reverbScenarioRun{
		{13, 0.83, ""}, {40, -0.83, ""}, {13, -0.83, ""}, {40, -0.83, reverbVariantSlow}, {40, -0.03, reverbVariantFast},
	}
}

// TestReverbDecayEstimator_MatchesUpstream compares the decay of every
// 4th update with the upstream ReverbDecayEstimator at the pinned commit.
func TestReverbDecayEstimator_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, run := range reverbScenarioRuns() {
		suffix := reverbScenarioSuffix(run.lengthBlocks, run.defaultLen, run.variant)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/reverb_decay_estimator_upstream.txt", suffix)
			scenario := newReverbScenario(run.lengthBlocks, run.defaultLen, run.variant)
			estimator := newReverbDecayEstimator(scenario.config)

			for index := range reverbScenarioUpdates {
				scenario.step(index)
				estimator.update(scenario.h[0], scenario.qualities[0], scenario.delays[0], scenario.usable[0],
					scenario.stationary)

				if index%4 == 0 {
					got := []float32{float32(index), estimator.getDecay(false), estimator.getDecay(true)}
					require.InDeltaSlice(t, reference.next("decay"), got, reverbReferenceDelta, "update %d", index)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
