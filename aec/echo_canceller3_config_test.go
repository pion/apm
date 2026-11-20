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

// loadConfigReference reads the values of a default-constructed upstream
// EchoCanceller3Config at the pinned commit, one line per struct with
// the fields in declaration order.
func loadConfigReference(t *testing.T) map[string][]float32 {
	t.Helper()
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/config_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	return reference
}

func TestDefaultDelayConfig_MatchesUpstream(t *testing.T) {
	reference := loadConfigReference(t)

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

func TestDefaultRenderLevelsConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultRenderLevelsConfig()
	assert.Equal(t, loadConfigReference(t)["render_levels"], []float32{
		cfg.activeRenderLimit,
		cfg.poorExcitationRenderLimit,
		cfg.poorExcitationRenderLimitDs8,
		cfg.renderPowerGainDB,
	})
}

func TestDefaultBufferingConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultBufferingConfig()
	assert.Equal(t, loadConfigReference(t)["buffering"], []float32{
		float32(cfg.excessRenderDetectionIntervalBlocks),
		float32(cfg.maxAllowedExcessRenderBlocks),
	})
}

func refinedFilterValues(cfg refinedFilterConfig) []float32 {
	return []float32{
		float32(cfg.lengthBlocks), cfg.leakageConverged, cfg.leakageDiverged,
		cfg.errorFloor, cfg.errorCeil, cfg.noiseGate,
	}
}

func coarseFilterValues(cfg coarseFilterConfig) []float32 {
	return []float32{float32(cfg.lengthBlocks), cfg.rate, cfg.noiseGate}
}

func TestDefaultFilterConfig_MatchesUpstream(t *testing.T) {
	reference := loadConfigReference(t)
	cfg := defaultFilterConfig()

	assert.Equal(t, reference["filter_refined"], refinedFilterValues(cfg.refined))
	assert.Equal(t, reference["filter_refined_initial"], refinedFilterValues(cfg.refinedInitial))
	assert.Equal(t, reference["filter_coarse"], coarseFilterValues(cfg.coarse))
	assert.Equal(t, reference["filter_coarse_initial"], coarseFilterValues(cfg.coarseInitial))
	assert.Equal(t, reference["filter"], []float32{
		float32(cfg.configChangeDurationBlocks),
		cfg.initialStateSeconds,
		float32(cfg.coarseResetHangoverBlocks),
		boolValue(cfg.conservativeInitialPhase),
		boolValue(cfg.enableCoarseFilterOutputUsage),
		boolValue(cfg.useLinearFilter),
		boolValue(cfg.highPassFilterEchoReference),
		boolValue(cfg.exportLinearAecOutput),
	})
}
