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
	erle         erleConfig
	epStrength   epStrengthConfig
	renderLevels renderLevelsConfig

	echoAudibility     echoAudibilityConfig
	echoRemovalControl echoRemovalControlConfig
	echoModel          echoModelConfig
	suppressor         suppressorConfig
}

// defaultEchoCanceller3Config returns the upstream defaults.
func defaultEchoCanceller3Config() echoCanceller3Config {
	return echoCanceller3Config{
		buffering:    defaultBufferingConfig(),
		delay:        defaultDelayConfig(),
		filter:       defaultFilterConfig(),
		erle:         defaultErleConfig(),
		epStrength:   defaultEpStrengthConfig(),
		renderLevels: defaultRenderLevelsConfig(),

		echoAudibility: defaultEchoAudibilityConfig(),
		echoModel:      defaultEchoModelConfig(),
		suppressor:     defaultSuppressorConfig(),
	}
}

// epStrengthConfig mirrors EchoCanceller3Config::EpStrength.
type epStrengthConfig struct {
	defaultGain                            float32
	defaultLen                             float32
	nearendLen                             float32
	echoCanSaturate                        bool
	boundedErl                             bool
	erleOnsetCompensationInDominantNearend bool
	useConservativeTailFrequencyResponse   bool
}

// defaultEpStrengthConfig returns the upstream defaults.
func defaultEpStrengthConfig() epStrengthConfig {
	return epStrengthConfig{
		defaultGain:                          1,
		defaultLen:                           0.83,
		nearendLen:                           0.83,
		echoCanSaturate:                      true,
		useConservativeTailFrequencyResponse: true,
	}
}

// erleConfig mirrors EchoCanceller3Config::Erle.
type erleConfig struct {
	min                        float32
	maxL                       float32
	maxH                       float32
	onsetDetection             bool
	numSections                int
	clampQualityEstimateToZero bool
	clampQualityEstimateToOne  bool
}

// defaultErleConfig returns the upstream defaults.
func defaultErleConfig() erleConfig {
	return erleConfig{
		min:                        1,
		maxL:                       4,
		maxH:                       1.5,
		onsetDetection:             true,
		numSections:                1,
		clampQualityEstimateToZero: true,
		clampQualityEstimateToOne:  true,
	}
}

// echoRemovalControlConfig mirrors
// EchoCanceller3Config::EchoRemovalControl. The defaults are the zero
// values.
type echoRemovalControlConfig struct {
	hasClockDrift           bool
	linearAndStableEchoPath bool
}

// echoAudibilityConfig mirrors EchoCanceller3Config::EchoAudibility.
type echoAudibilityConfig struct {
	lowRenderLimit                  float32
	normalRenderLimit               float32
	floorPower                      float32
	audibilityThresholdLf           float32
	audibilityThresholdMf           float32
	audibilityThresholdHf           float32
	useStationarityProperties       bool
	useStationarityPropertiesAtInit bool
}

// defaultEchoAudibilityConfig returns the upstream defaults.
func defaultEchoAudibilityConfig() echoAudibilityConfig {
	return echoAudibilityConfig{
		lowRenderLimit:        4 * 64,
		normalRenderLimit:     64,
		floorPower:            2 * 64,
		audibilityThresholdLf: 10,
		audibilityThresholdMf: 10,
		audibilityThresholdHf: 10,
	}
}

// echoModelConfig mirrors EchoCanceller3Config::EchoModel.
type echoModelConfig struct {
	noiseFloorHold             int
	minNoiseFloorPower         float32
	stationaryGateSlope        float32
	noiseGatePower             float32
	noiseGateSlope             float32
	renderPreWindowSize        int
	renderPostWindowSize       int
	modelReverbInNonlinearMode bool
}

// defaultEchoModelConfig returns the upstream defaults.
func defaultEchoModelConfig() echoModelConfig {
	return echoModelConfig{
		noiseFloorHold:             50,
		minNoiseFloorPower:         1638400,
		stationaryGateSlope:        10,
		noiseGatePower:             27509.42,
		noiseGateSlope:             0.3,
		renderPreWindowSize:        1,
		renderPostWindowSize:       1,
		modelReverbInNonlinearMode: true,
	}
}

// maskingThresholds mirrors
// EchoCanceller3Config::Suppressor::MaskingThresholds.
type maskingThresholds struct {
	enrTransparent float32
	enrSuppress    float32
	emrTransparent float32
}

// suppressorTuning mirrors EchoCanceller3Config::Suppressor::Tuning.
type suppressorTuning struct {
	maskLf         maskingThresholds
	maskHf         maskingThresholds
	maxIncFactor   float32
	maxDecFactorLf float32
}

// dominantNearendDetectionConfig mirrors
// EchoCanceller3Config::Suppressor::DominantNearendDetection.
type dominantNearendDetectionConfig struct {
	enrThreshold             float32
	enrExitThreshold         float32
	snrThreshold             float32
	holdDuration             int
	triggerThreshold         int
	useDuringInitialPhase    bool
	useUnboundedEchoSpectrum bool
}

// subbandRegion mirrors
// EchoCanceller3Config::Suppressor::SubbandNearendDetection::SubbandRegion.
type subbandRegion struct {
	low  int
	high int
}

// subbandNearendDetectionConfig mirrors
// EchoCanceller3Config::Suppressor::SubbandNearendDetection.
type subbandNearendDetectionConfig struct {
	nearendAverageBlocks int
	subband1             subbandRegion
	subband2             subbandRegion
	nearendThreshold     float32
	snrThreshold         float32
}

// highBandsSuppressionConfig mirrors
// EchoCanceller3Config::Suppressor::HighBandsSuppression.
type highBandsSuppressionConfig struct {
	enrThreshold                   float32
	maxGainDuringEcho              float32
	antiHowlingActivationThreshold float32
	antiHowlingGain                float32
}

// highFrequencySuppressionConfig mirrors
// EchoCanceller3Config::Suppressor::HighFrequencySuppression.
type highFrequencySuppressionConfig struct {
	limitingGainBand    int
	bandsInLimitingGain int
}

// suppressorConfig mirrors EchoCanceller3Config::Suppressor.
type suppressorConfig struct {
	nearendAverageBlocks          int
	normalTuning                  suppressorTuning
	nearendTuning                 suppressorTuning
	lfSmoothingDuringInitialPhase bool
	lastPermanentLfSmoothingBand  int
	lastLfSmoothingBand           int
	lastLfBand                    int
	firstHfBand                   int
	dominantNearendDetection      dominantNearendDetectionConfig
	subbandNearendDetection       subbandNearendDetectionConfig
	useSubbandNearendDetection    bool
	highBandsSuppression          highBandsSuppressionConfig
	highFrequencySuppression      highFrequencySuppressionConfig
	floorFirstIncrease            float32
	conservativeHfSuppression     bool
}

// defaultSuppressorConfig returns the upstream defaults.
func defaultSuppressorConfig() suppressorConfig {
	return suppressorConfig{
		nearendAverageBlocks: 4,
		normalTuning: suppressorTuning{
			maskLf:         maskingThresholds{enrTransparent: 0.3, enrSuppress: 0.4, emrTransparent: 0.3},
			maskHf:         maskingThresholds{enrTransparent: 0.07, enrSuppress: 0.1, emrTransparent: 0.3},
			maxIncFactor:   2,
			maxDecFactorLf: 0.25,
		},
		nearendTuning: suppressorTuning{
			maskLf:         maskingThresholds{enrTransparent: 1.09, enrSuppress: 1.1, emrTransparent: 0.3},
			maskHf:         maskingThresholds{enrTransparent: 0.1, enrSuppress: 0.3, emrTransparent: 0.3},
			maxIncFactor:   2,
			maxDecFactorLf: 0.25,
		},
		lfSmoothingDuringInitialPhase: true,
		lastPermanentLfSmoothingBand:  0,
		lastLfSmoothingBand:           5,
		lastLfBand:                    5,
		firstHfBand:                   8,
		dominantNearendDetection: dominantNearendDetectionConfig{
			enrThreshold:             0.25,
			enrExitThreshold:         10,
			snrThreshold:             30,
			holdDuration:             50,
			triggerThreshold:         12,
			useDuringInitialPhase:    true,
			useUnboundedEchoSpectrum: true,
		},
		subbandNearendDetection: subbandNearendDetectionConfig{
			nearendAverageBlocks: 1,
			subband1:             subbandRegion{low: 1, high: 1},
			subband2:             subbandRegion{low: 1, high: 1},
			nearendThreshold:     1,
			snrThreshold:         1,
		},
		highBandsSuppression: highBandsSuppressionConfig{
			enrThreshold:                   1,
			maxGainDuringEcho:              1,
			antiHowlingActivationThreshold: 400,
			antiHowlingGain:                1,
		},
		highFrequencySuppression: highFrequencySuppressionConfig{limitingGainBand: 16, bandsInLimitingGain: 1},
		floorFirstIncrease:       0.00001,
	}
}
