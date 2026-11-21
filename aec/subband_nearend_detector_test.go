// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"
)

// TestSubbandNearendDetector_MatchesUpstream compares the steps where the
// nearend state changes with the upstream SubbandNearendDetector. The
// detector starts with an average over 3 blocks, the subbands 2 to 8 and
// 10 to 30, a nearend threshold of 1.5 and an SNR threshold of 20. Each
// line of the reference file is keyed by "name detector capture_channels".
func TestSubbandNearendDetector_MatchesUpstream(t *testing.T) {
	for _, numCaptureChannels := range []int{1, 2} {
		t.Run(fmt.Sprint(numCaptureChannels), func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/subband_nearend_detector_upstream.txt",
				fmt.Sprintf(" subband %d", numCaptureChannels))
			config := defaultSuppressorConfig()
			config.subbandNearendDetection = subbandNearendDetectionConfig{
				nearendAverageBlocks: 3,
				subband1:             subbandRegion{low: 2, high: 8},
				subband2:             subbandRegion{low: 10, high: 30},
				nearendThreshold:     1.5,
				snrThreshold:         20,
			}
			detector := newSubbandNearendDetector(config, numCaptureChannels)
			runNearendDetectorScenario(t, detector, config, numCaptureChannels, false, reference)
		})
	}
}
