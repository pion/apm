// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

func boolValue(b bool) float32 {
	if b {
		return 1
	}

	return 0
}

func alignmentMixingValues(mixing alignmentMixing) []float32 {
	return []float32{
		boolValue(mixing.downmix),
		boolValue(mixing.adaptiveSelection),
		mixing.activityPowerThreshold,
		boolValue(mixing.preferFirstTwoChannels),
	}
}

// TestDefaultDelayConfig_MatchesUpstream compares the defaults with the
// values of a default-constructed upstream EchoCanceller3Config at the
// pinned commit, printed field by field in declaration order.
func TestDefaultDelayConfig_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/delay_config_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	cfg := defaultDelayConfig()
	assert.Equal(t, reference["delay"], []float32{
		float32(cfg.defaultDelay),
		float32(cfg.downSamplingFactor),
		float32(cfg.numFilters),
		float32(cfg.delayHeadroomSamples),
		float32(cfg.hysteresisLimitBlocks),
		float32(cfg.fixedCaptureDelaySamples),
		cfg.delayEstimateSmoothing,
		cfg.delayEstimateSmoothingDelayFound,
		cfg.delayCandidateDetectionThreshold,
		float32(cfg.delaySelectionThresholds.initial),
		float32(cfg.delaySelectionThresholds.converged),
		boolValue(cfg.useExternalDelayEstimator),
		boolValue(cfg.logWarningOnDelayChanges),
		boolValue(cfg.detectPreEcho),
	})
	assert.Equal(t, reference["render_alignment_mixing"], alignmentMixingValues(cfg.renderAlignmentMixing))
	assert.Equal(t, reference["capture_alignment_mixing"], alignmentMixingValues(cfg.captureAlignmentMixing))
}
