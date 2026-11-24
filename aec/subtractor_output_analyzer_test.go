// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubtractorOutputAnalyzer_Update(t *testing.T) {
	const loud = 100 * 100 * kBlockSize // Above the convergence threshold.
	for _, tc := range []struct {
		name                                 string
		y2, e2Refined, e2Coarse              float32
		converged, coarseConverged, diverged bool
	}{
		{"refined converged", loud, 0.4 * loud, loud, true, false, false},
		{"coarse converged strictly", loud, loud, 0.04 * loud, true, true, false},
		{"coarse converged relaxed only", loud, loud, 0.2 * loud, false, true, false},
		{"low level", 25 * 25 * kBlockSize, 0, 0.2 * 25 * 25 * kBlockSize, false, true, false},
		{"too quiet", 10 * 10 * kBlockSize, 0, 0, false, false, false},
		{"diverged", loud, 2 * loud, 1.6 * loud, false, false, true},
		{"neither", loud, 0.9 * loud, 0.9 * loud, false, false, false},
	} {
		analyzer := newSubtractorOutputAnalyzer(1)
		outputs := []subtractorOutput{{y2: tc.y2, e2Refined: tc.e2Refined, e2Coarse: tc.e2Coarse}}

		converged, coarseConverged, diverged := analyzer.update(outputs)
		assert.Equal(t, tc.converged, converged, tc.name)
		assert.Equal(t, tc.coarseConverged, coarseConverged, tc.name)
		assert.Equal(t, tc.diverged, diverged, tc.name)
		assert.Equal(t, []bool{tc.converged}, analyzer.convergedFilters(), tc.name)
	}
}

func TestSubtractorOutputAnalyzer_MultiChannel(t *testing.T) {
	const loud = 100 * 100 * kBlockSize
	analyzer := newSubtractorOutputAnalyzer(2)
	outputs := []subtractorOutput{
		{y2: loud, e2Refined: 2 * loud, e2Coarse: 2 * loud},
		{y2: loud, e2Refined: 0.1 * loud, e2Coarse: loud},
	}

	// Any channel converging counts; all must diverge to count as diverged.
	converged, _, diverged := analyzer.update(outputs)
	assert.True(t, converged)
	assert.False(t, diverged)
	assert.Equal(t, []bool{false, true}, analyzer.convergedFilters())

	analyzer.handleEchoPathChange()
	assert.Equal(t, []bool{false, false}, analyzer.convergedFilters())
}
