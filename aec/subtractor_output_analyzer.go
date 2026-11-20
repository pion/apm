// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// subtractorOutputAnalyzer mirrors SubtractorOutputAnalyzer: it analyzes
// the properties of the subtractor output.
type subtractorOutputAnalyzer struct {
	filtersConverged []bool
}

func newSubtractorOutputAnalyzer(numCaptureChannels int) *subtractorOutputAnalyzer {
	return &subtractorOutputAnalyzer{filtersConverged: make([]bool, numCaptureChannels)}
}

// update mirrors Update: it analyzes the subtractor output.
func (a *subtractorOutputAnalyzer) update(
	outputs []subtractorOutput,
) (anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged bool) {
	const (
		convergenceThreshold         = 50 * 50 * kBlockSize
		convergenceThresholdLowLevel = 20 * 20 * kBlockSize
	)
	allFiltersDiverged = true
	for ch := range outputs {
		y2 := outputs[ch].y2
		e2Refined := outputs[ch].e2Refined
		e2Coarse := outputs[ch].e2Coarse

		refinedFilterConverged := e2Refined < 0.5*y2 && y2 > convergenceThreshold
		coarseFilterConvergedStrict := e2Coarse < 0.05*y2 && y2 > convergenceThreshold
		coarseFilterConvergedRelaxed := e2Coarse < 0.3*y2 && y2 > convergenceThresholdLowLevel
		minE2 := min(e2Refined, e2Coarse)
		filterDiverged := minE2 > 1.5*y2 && y2 > 30*30*kBlockSize
		a.filtersConverged[ch] = refinedFilterConverged || coarseFilterConvergedStrict

		anyFilterConverged = anyFilterConverged || a.filtersConverged[ch]
		anyCoarseFilterConverged = anyCoarseFilterConverged || coarseFilterConvergedRelaxed
		allFiltersDiverged = allFiltersDiverged && filterDiverged
	}

	return anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged
}

// convergedFilters mirrors ConvergedFilters.
func (a *subtractorOutputAnalyzer) convergedFilters() []bool { return a.filtersConverged }

// handleEchoPathChange mirrors HandleEchoPathChange.
func (a *subtractorOutputAnalyzer) handleEchoPathChange() {
	clear(a.filtersConverged)
}
