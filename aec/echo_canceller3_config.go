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
