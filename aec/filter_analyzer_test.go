// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of FilterAnalyzer.FilterResize: the filter analyzer handles filter
// resizes properly.
func TestFilterAnalyzer_FilterResize(t *testing.T) {
	config := defaultEchoCanceller3Config()
	for _, numCaptureChannels := range []int{1, 2, 4} {
		analyzer := newFilterAnalyzer(config, numCaptureChannels)
		analyzer.setRegionToAnalyze(65)
		analyzer.setRegionToAnalyze(65)
		analyzer.setRegionToAnalyze(32)
		assert.Equal(t, filterRegion{startSample: 0, endSample: 31}, analyzer.region)
	}
}

// TestFilterAnalyzer_MatchesUpstream runs the analyzer on the filters of
// the subtractor reference scenario, without the delay change, and
// compares the analysis of every block with the upstream FilterAnalyzer
// at the pinned commit. The analyzer is reset at block 1400. Each line
// of the reference file is keyed by
// "name render_channels capture_channels delay".
func TestFilterAnalyzer_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, tc := range []struct{ numRender, numCapture, delaySamples int }{{1, 1, 150}, {2, 2, 64}} {
		suffix := fmt.Sprintf(" %d %d %d", tc.numRender, tc.numCapture, tc.delaySamples)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/filter_analyzer_upstream.txt", suffix)
			scenario := newSubtractorScenario(tc.numRender, tc.numCapture, tc.delaySamples)
			scenario.delayChangeBlock = -1
			analyzer := newFilterAnalyzer(defaultEchoCanceller3Config(), tc.numCapture)

			for index := range 1500 {
				scenario.step(index)
				anyFilterConsistent, maxEchoPathGain := analyzer.update(
					scenario.subtractor.filterImpulseResponses(), scenario.renderDelayBuffer.getRenderBuffer())

				// The block index, whether a filter is consistent, the
				// gain, the minimum delay, the filter length, then the
				// delay of every filter.
				want := reference.next("analysis")
				got := []float32{
					float32(index), boolValue(anyFilterConsistent), maxEchoPathGain,
					float32(analyzer.getMinFilterDelayBlocks()), float32(analyzer.filterLengthBlocks()),
				}
				for _, delay := range analyzer.getFilterDelaysBlocks() {
					got = append(got, float32(delay))
				}
				require.InDeltaSlice(t, want, got, 1e-3, "block %d", index)

				if index == 1400 {
					analyzer.reset()
				}
				if index == 700 || index == 1499 {
					assertMatchesUpstream(t, reference.next("adjusted_filter"), analyzer.getAdjustedFilters()[tc.numCapture-1],
						"adjusted_filter")
				}
			}
		})
	}
}
