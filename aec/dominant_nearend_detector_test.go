// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"
)

// TestDominantNearendDetector_MatchesUpstream compares the steps where
// the nearend state changes with the upstream DominantNearendDetector.
// Each line of the reference file is keyed by
// "name detector capture_channels".
func TestDominantNearendDetector_MatchesUpstream(t *testing.T) {
	for _, numCaptureChannels := range []int{1, 2} {
		t.Run(fmt.Sprint(numCaptureChannels), func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/dominant_nearend_detector_upstream.txt",
				fmt.Sprintf(" dominant %d", numCaptureChannels))
			config := defaultSuppressorConfig()
			detector := newDominantNearendDetector(config.dominantNearendDetection, numCaptureChannels)
			runNearendDetectorScenario(t, detector, config, numCaptureChannels, true, reference)
		})
	}
}
