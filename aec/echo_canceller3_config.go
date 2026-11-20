// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// delaySelectionThresholds mirrors
// EchoCanceller3Config::Delay::DelaySelectionThresholds.
type delaySelectionThresholds struct {
	initial   int
	converged int
}

// alignmentMixing mirrors EchoCanceller3Config::Delay::AlignmentMixing.
type alignmentMixing struct {
	downmix                bool
	adaptiveSelection      bool
	activityPowerThreshold float32
	preferFirstTwoChannels bool
}

// delayConfig mirrors EchoCanceller3Config::Delay.
type delayConfig struct {
	defaultDelay                     int
	downSamplingFactor               int
	numFilters                       int
	delayHeadroomSamples             int
	hysteresisLimitBlocks            int
	fixedCaptureDelaySamples         int
	delayEstimateSmoothing           float32
	delayEstimateSmoothingDelayFound float32
	delayCandidateDetectionThreshold float32
	delaySelectionThresholds         delaySelectionThresholds
	useExternalDelayEstimator        bool
	logWarningOnDelayChanges         bool
	renderAlignmentMixing            alignmentMixing
	captureAlignmentMixing           alignmentMixing
	detectPreEcho                    bool
}

// defaultDelayConfig returns the upstream defaults.
func defaultDelayConfig() delayConfig {
	return delayConfig{
		defaultDelay:                     5,
		downSamplingFactor:               4,
		numFilters:                       5,
		delayHeadroomSamples:             32,
		hysteresisLimitBlocks:            1,
		fixedCaptureDelaySamples:         0,
		delayEstimateSmoothing:           0.7,
		delayEstimateSmoothingDelayFound: 0.7,
		delayCandidateDetectionThreshold: 0.2,
		delaySelectionThresholds:         delaySelectionThresholds{initial: 5, converged: 20},
		renderAlignmentMixing: alignmentMixing{
			downmix:                false,
			adaptiveSelection:      true,
			activityPowerThreshold: 10000,
			preferFirstTwoChannels: true,
		},
		captureAlignmentMixing: alignmentMixing{
			downmix:                false,
			adaptiveSelection:      true,
			activityPowerThreshold: 10000,
			preferFirstTwoChannels: false,
		},
		detectPreEcho: true,
	}
}

// renderLevelsConfig mirrors EchoCanceller3Config::RenderLevels.
type renderLevelsConfig struct {
	activeRenderLimit            float32
	poorExcitationRenderLimit    float32
	poorExcitationRenderLimitDs8 float32
	renderPowerGainDB            float32
}

// defaultRenderLevelsConfig returns the upstream defaults.
func defaultRenderLevelsConfig() renderLevelsConfig {
	return renderLevelsConfig{
		activeRenderLimit:            100,
		poorExcitationRenderLimit:    150,
		poorExcitationRenderLimitDs8: 20,
		renderPowerGainDB:            0,
	}
}

// bufferingConfig mirrors EchoCanceller3Config::Buffering.
type bufferingConfig struct {
	excessRenderDetectionIntervalBlocks int
	maxAllowedExcessRenderBlocks        int
}

// defaultBufferingConfig returns the upstream defaults.
func defaultBufferingConfig() bufferingConfig {
	return bufferingConfig{
		excessRenderDetectionIntervalBlocks: 250,
		maxAllowedExcessRenderBlocks:        8,
	}
}

// refinedFilterConfig mirrors
// EchoCanceller3Config::Filter::RefinedConfiguration.
type refinedFilterConfig struct {
	lengthBlocks     int
	leakageConverged float32
	leakageDiverged  float32
	errorFloor       float32
	errorCeil        float32
	noiseGate        float32
}

// coarseFilterConfig mirrors
// EchoCanceller3Config::Filter::CoarseConfiguration.
type coarseFilterConfig struct {
	lengthBlocks int
	rate         float32
	noiseGate    float32
}

// filterConfig mirrors EchoCanceller3Config::Filter.
type filterConfig struct {
	refined                       refinedFilterConfig
	coarse                        coarseFilterConfig
	refinedInitial                refinedFilterConfig
	coarseInitial                 coarseFilterConfig
	configChangeDurationBlocks    int
	initialStateSeconds           float32
	coarseResetHangoverBlocks     int
	conservativeInitialPhase      bool
	enableCoarseFilterOutputUsage bool
	useLinearFilter               bool
	highPassFilterEchoReference   bool
	exportLinearAecOutput         bool
}

// defaultFilterConfig returns the upstream defaults.
func defaultFilterConfig() filterConfig {
	return filterConfig{
		refined: refinedFilterConfig{
			lengthBlocks:     13,
			leakageConverged: 0.00005,
			leakageDiverged:  0.05,
			errorFloor:       0.001,
			errorCeil:        2,
			noiseGate:        20075344,
		},
		coarse: coarseFilterConfig{lengthBlocks: 13, rate: 0.7, noiseGate: 20075344},
		refinedInitial: refinedFilterConfig{
			lengthBlocks:     12,
			leakageConverged: 0.005,
			leakageDiverged:  0.5,
			errorFloor:       0.001,
			errorCeil:        2,
			noiseGate:        20075344,
		},
		coarseInitial:                 coarseFilterConfig{lengthBlocks: 12, rate: 0.9, noiseGate: 20075344},
		configChangeDurationBlocks:    250,
		initialStateSeconds:           2.5,
		coarseResetHangoverBlocks:     25,
		conservativeInitialPhase:      false,
		enableCoarseFilterOutputUsage: true,
		useLinearFilter:               true,
		highPassFilterEchoReference:   false,
		exportLinearAecOutput:         false,
	}
}

// echoCanceller3Config mirrors the ported parts of EchoCanceller3Config.
type echoCanceller3Config struct {
	buffering    bufferingConfig
	delay        delayConfig
	filter       filterConfig
	renderLevels renderLevelsConfig
}

// defaultEchoCanceller3Config returns the upstream defaults.
func defaultEchoCanceller3Config() echoCanceller3Config {
	return echoCanceller3Config{
		buffering:    defaultBufferingConfig(),
		delay:        defaultDelayConfig(),
		filter:       defaultFilterConfig(),
		renderLevels: defaultRenderLevelsConfig(),
	}
}
