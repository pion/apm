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

func TestDefaultEpStrengthConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultEpStrengthConfig()
	assert.Equal(t, loadConfigReference(t)["ep_strength"], []float32{
		cfg.defaultGain,
		cfg.defaultLen,
		cfg.nearendLen,
		boolValue(cfg.echoCanSaturate),
		boolValue(cfg.boundedErl),
		boolValue(cfg.erleOnsetCompensationInDominantNearend),
		boolValue(cfg.useConservativeTailFrequencyResponse),
	})
}

func TestDefaultErleConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultErleConfig()
	assert.Equal(t, loadConfigReference(t)["erle"], []float32{
		cfg.min,
		cfg.maxL,
		cfg.maxH,
		boolValue(cfg.onsetDetection),
		float32(cfg.numSections),
		boolValue(cfg.clampQualityEstimateToZero),
		boolValue(cfg.clampQualityEstimateToOne),
	})
}

func TestDefaultEchoRemovalControlConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultEchoCanceller3Config().echoRemovalControl
	assert.Equal(t, loadConfigReference(t)["echo_removal_control"], []float32{
		boolValue(cfg.hasClockDrift),
		boolValue(cfg.linearAndStableEchoPath),
	})
}

func TestDefaultEchoAudibilityConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultEchoAudibilityConfig()
	assert.Equal(t, loadConfigReference(t)["echo_audibility"], []float32{
		cfg.lowRenderLimit,
		cfg.normalRenderLimit,
		cfg.floorPower,
		cfg.audibilityThresholdLf,
		cfg.audibilityThresholdMf,
		cfg.audibilityThresholdHf,
		boolValue(cfg.useStationarityProperties),
		boolValue(cfg.useStationarityPropertiesAtInit),
	})
}

func TestDefaultEchoModelConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultEchoModelConfig()
	assert.Equal(t, loadConfigReference(t)["echo_model"], []float32{
		float32(cfg.noiseFloorHold),
		cfg.minNoiseFloorPower,
		cfg.stationaryGateSlope,
		cfg.noiseGatePower,
		cfg.noiseGateSlope,
		float32(cfg.renderPreWindowSize),
		float32(cfg.renderPostWindowSize),
		boolValue(cfg.modelReverbInNonlinearMode),
	})
}

func suppressorTuningValues(tuning suppressorTuning) []float32 {
	return []float32{
		tuning.maskLf.enrTransparent, tuning.maskLf.enrSuppress, tuning.maskLf.emrTransparent,
		tuning.maskHf.enrTransparent, tuning.maskHf.enrSuppress, tuning.maskHf.emrTransparent,
		tuning.maxIncFactor, tuning.maxDecFactorLf,
	}
}

func TestDefaultSuppressorConfig_MatchesUpstream(t *testing.T) {
	reference := loadConfigReference(t)
	cfg := defaultSuppressorConfig()

	assert.Equal(t, reference["suppressor"], []float32{
		float32(cfg.nearendAverageBlocks),
		boolValue(cfg.lfSmoothingDuringInitialPhase),
		float32(cfg.lastPermanentLfSmoothingBand),
		float32(cfg.lastLfSmoothingBand),
		float32(cfg.lastLfBand),
		float32(cfg.firstHfBand),
		boolValue(cfg.useSubbandNearendDetection),
		cfg.floorFirstIncrease,
		boolValue(cfg.conservativeHfSuppression),
	})
	assert.Equal(t, reference["suppressor_normal_tuning"], suppressorTuningValues(cfg.normalTuning))
	assert.Equal(t, reference["suppressor_nearend_tuning"], suppressorTuningValues(cfg.nearendTuning))

	dominant := cfg.dominantNearendDetection
	assert.Equal(t, reference["suppressor_dominant_nearend_detection"], []float32{
		dominant.enrThreshold,
		dominant.enrExitThreshold,
		dominant.snrThreshold,
		float32(dominant.holdDuration),
		float32(dominant.triggerThreshold),
		boolValue(dominant.useDuringInitialPhase),
		boolValue(dominant.useUnboundedEchoSpectrum),
	})

	subband := cfg.subbandNearendDetection
	assert.Equal(t, reference["suppressor_subband_nearend_detection"], []float32{
		float32(subband.nearendAverageBlocks),
		float32(subband.subband1.low), float32(subband.subband1.high),
		float32(subband.subband2.low), float32(subband.subband2.high),
		subband.nearendThreshold,
		subband.snrThreshold,
	})

	highBands := cfg.highBandsSuppression
	assert.Equal(t, reference["suppressor_high_bands_suppression"], []float32{
		highBands.enrThreshold,
		highBands.maxGainDuringEcho,
		highBands.antiHowlingActivationThreshold,
		highBands.antiHowlingGain,
	})
	assert.Equal(t, reference["suppressor_high_frequency_suppression"], []float32{
		float32(cfg.highFrequencySuppression.limitingGainBand),
		float32(cfg.highFrequencySuppression.bandsInLimitingGain),
	})
}

func TestDefaultComfortNoiseConfig_MatchesUpstream(t *testing.T) {
	cfg := defaultEchoCanceller3Config().comfortNoise
	assert.Equal(t, loadConfigReference(t)["comfort_noise"], []float32{cfg.noiseFloorDbfs})
}
