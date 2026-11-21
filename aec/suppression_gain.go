// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// limitLowFrequencyGains matches LimitLowFrequencyGains: it limits the
// low frequency gains to avoid the impact of the high-pass filter on the
// lower-frequency gain influencing the overall achieved gain.
func limitLowFrequencyGains(gain *[kFftLengthBy2Plus1]float32) {
	gain[1] = min(gain[1], gain[2])
	gain[0] = gain[1]
}

// limitHighFrequencyGains matches LimitHighFrequencyGains: it limits the
// high frequency gains to avoid echo leakage due to an imperfect filter.
func limitHighFrequencyGains(
	highFrequencySuppression highFrequencySuppressionConfig,
	conservativeHfSuppression bool,
	gain *[kFftLengthBy2Plus1]float32,
) {
	limitingGainBand := highFrequencySuppression.limitingGainBand
	bandsInLimitingGain := highFrequencySuppression.bandsInLimitingGain
	if bandsInLimitingGain > 0 {
		minUpperGain := float32(1)
		for _, value := range gain[limitingGainBand : limitingGainBand+bandsInLimitingGain] {
			minUpperGain = min(minUpperGain, value)
		}
		for k := limitingGainBand + 1; k < len(gain); k++ {
			gain[k] = min(gain[k], minUpperGain)
		}
	}
	gain[kFftLengthBy2] = gain[kFftLengthBy2-1]

	if conservativeHfSuppression {
		// Limit the gain in the frequencies for which the adaptive filter
		// has not converged.
		const (
			upperAccurateBandPlus1 = 29
			oneByBandsInSum        = 1.0 / (upperAccurateBandPlus1 - 20)
		)
		hfGainBound := sum(gain[20:upperAccurateBandPlus1]) * oneByBandsInSum
		for k := upperAccurateBandPlus1; k < len(gain); k++ {
			gain[k] = min(gain[k], hfGainBound)
		}
	}
}

// weighEcho scales the echo of the bands below a threshold.
func weighEcho(threshold, normalizer float32, echo, weightedEcho []float32) {
	for k, value := range echo {
		weightedEcho[k] = value
		if value < threshold {
			tmp := (threshold - value) * normalizer
			weightedEcho[k] = value * max(0, 1-tmp*tmp)
		}
	}
}

// weightEchoForAudibility matches WeightEchoForAudibility: it scales the
// echo according to the assessed audibility at the other end.
func weightEchoForAudibility(
	echoAudibility echoAudibilityConfig, echo *[kFftLengthBy2Plus1]float32, weightedEcho *[kFftLengthBy2Plus1]float32,
) {
	for _, region := range []struct {
		audibilityThreshold float32
		begin, end          int
	}{
		{echoAudibility.audibilityThresholdLf, 0, 3},
		{echoAudibility.audibilityThresholdMf, 3, 7},
		{echoAudibility.audibilityThresholdHf, 7, kFftLengthBy2Plus1},
	} {
		threshold := echoAudibility.floorPower * region.audibilityThreshold
		normalizer := 1 / (threshold - echoAudibility.floorPower)
		weighEcho(threshold, normalizer, echo[region.begin:region.end], weightedEcho[region.begin:region.end])
	}
}

// lowNoiseRenderDetector mirrors SuppressionGain::LowNoiseRenderDetector.
type lowNoiseRenderDetector struct {
	averagePower float32
}

// detect mirrors Detect: it detects when the render signal can be
// considered to have low power and consist of stationary noise.
func (l *lowNoiseRenderDetector) detect(render *block) bool {
	const threshold = 50 * 50 * 64
	var x2Sum, x2Max float32
	for ch := range render.numChannels {
		for _, sample := range render.view(0, ch) {
			x2 := sample * sample
			x2Sum += x2
			x2Max = max(x2Max, x2)
		}
	}
	x2Sum /= float32(render.numChannels)

	lowNoiseRender := l.averagePower < threshold && x2Max < 3*l.averagePower
	l.averagePower = l.averagePower*0.9 + x2Sum*0.1

	return lowNoiseRender
}

// gainParameters mirrors SuppressionGain::GainParameters.
type gainParameters struct {
	maxIncFactor   float32
	maxDecFactorLf float32
	enrTransparent [kFftLengthBy2Plus1]float32
	enrSuppress    [kFftLengthBy2Plus1]float32
	emrTransparent [kFftLengthBy2Plus1]float32
}

func newGainParameters(lastLfBand, firstHfBand int, tuning suppressorTuning) gainParameters {
	var params gainParameters
	params.setConfig(lastLfBand, firstHfBand, tuning)

	return params
}

// setConfig mirrors SetConfig: it computes the masking thresholds per
// band.
func (g *gainParameters) setConfig(lastLfBand, firstHfBand int, tuning suppressorTuning) {
	g.maxIncFactor = tuning.maxIncFactor
	g.maxDecFactorLf = tuning.maxDecFactorLf
	lf := tuning.maskLf
	hf := tuning.maskHf
	for k := range kFftLengthBy2Plus1 {
		var a float32
		switch {
		case k <= lastLfBand:
			a = 0
		case k < firstHfBand:
			a = float32(k-lastLfBand) / float32(firstHfBand-lastLfBand)
		default:
			a = 1
		}
		g.enrTransparent[k] = (1-a)*lf.enrTransparent + a*hf.enrTransparent
		g.enrSuppress[k] = (1-a)*lf.enrSuppress + a*hf.enrSuppress
		g.emrTransparent[k] = (1-a)*lf.emrTransparent + a*hf.emrTransparent
	}
}

// suppressionGain mirrors SuppressionGain: it computes the gains that
// suppress the residual echo.
type suppressionGain struct {
	numCaptureChannels      int
	echoAudibilityConfig    echoAudibilityConfig
	lastGain                [kFftLengthBy2Plus1]float32
	lastNearend             [][kFftLengthBy2Plus1]float32
	lastEcho                [][kFftLengthBy2Plus1]float32
	lowRenderDetector       lowNoiseRenderDetector
	initialState            bool
	nearendSmoothers        []movingAverageSpectrum
	nearendParams           gainParameters
	normalParams            gainParameters
	dominantNearendDetector nearendDetector
}

func newSuppressionGain(config echoCanceller3Config, numCaptureChannels int) *suppressionGain {
	suppressor := config.suppressor
	gain := &suppressionGain{
		numCaptureChannels:   numCaptureChannels,
		echoAudibilityConfig: config.echoAudibility,
		lastNearend:          make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		lastEcho:             make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		lowRenderDetector:    lowNoiseRenderDetector{averagePower: 32768 * 32768},
		initialState:         true,
		nearendSmoothers:     make([]movingAverageSpectrum, numCaptureChannels),
		nearendParams:        newGainParameters(suppressor.lastLfBand, suppressor.firstHfBand, suppressor.nearendTuning),
		normalParams:         newGainParameters(suppressor.lastLfBand, suppressor.firstHfBand, suppressor.normalTuning),
	}
	fill(gain.lastGain[:], 1)
	for ch := range gain.nearendSmoothers {
		gain.nearendSmoothers[ch] = newMovingAverageSpectrum(kFftLengthBy2Plus1, suppressor.nearendAverageBlocks)
	}
	if suppressor.useSubbandNearendDetection {
		gain.dominantNearendDetector = newSubbandNearendDetector(suppressor, numCaptureChannels)
	} else {
		gain.dominantNearendDetector = newDominantNearendDetector(
			suppressor.dominantNearendDetection, numCaptureChannels)
	}

	return gain
}

// renderBandEnergies returns the largest energy over the channels of the
// first band, and over the channels and the bands of the upper bands.
func renderBandEnergies(render *block) (lowBandEnergy, highBandEnergy float32) {
	for ch := range render.numChannels {
		lowBandEnergy = max(lowBandEnergy, energy(render.view(0, ch)))
	}
	for band := 1; band < render.numBands; band++ {
		for ch := range render.numChannels {
			highBandEnergy = max(highBandEnergy, energy(render.view(band, ch)))
		}
	}

	return lowBandEnergy, highBandEnergy
}

// upperBandsGain mirrors UpperBandsGain: it computes the gain to apply
// for the bands beyond the first band.
func (s *suppressionGain) upperBandsGain(
	suppressor suppressorConfig,
	echoSpectrum, comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32,
	narrowPeakBand int,
	hasNarrowPeakBand, saturatedEcho bool,
	render *block,
	lowBandGain *[kFftLengthBy2Plus1]float32,
) float32 {
	const lowBandGainLimit = kFftLengthBy2 / 2
	if render.numBands == 1 {
		return 1
	}
	if hasNarrowPeakBand && narrowPeakBand > kFftLengthBy2Plus1-10 {
		return 0.001
	}

	gainBelow8kHz := lowBandGain[lowBandGainLimit]
	for _, value := range lowBandGain[lowBandGainLimit:] {
		gainBelow8kHz = min(gainBelow8kHz, value)
	}

	// Always attenuate the upper bands when there is saturated echo.
	if saturatedEcho {
		return min(0.001, gainBelow8kHz)
	}

	// If there is more power in the lower frequencies than the upper
	// frequencies, or if the power in the upper frequencies is low, do not
	// bound the gain in the upper bands. In all other cases, bound it.
	lowBandEnergy, highBandEnergy := renderBandEnergies(render)
	antiHowlingGain := float32(1)
	activationThreshold := kBlockSize * suppressor.highBandsSuppression.antiHowlingActivationThreshold
	if highBandEnergy >= max(lowBandEnergy, activationThreshold) {
		antiHowlingGain = suppressor.highBandsSuppression.antiHowlingGain *
			float32(math.Sqrt(float64(lowBandEnergy/highBandEnergy)))
	}

	// Choose the gain as the minimum of the lower and upper gains.
	return min(gainBelow8kHz, antiHowlingGain, s.upperGainBound(suppressor, echoSpectrum, comfortNoiseSpectrum))
}

// upperGainBound bounds the upper gain during significant echo activity.
func (s *suppressionGain) upperGainBound(
	suppressor suppressorConfig, echoSpectrum, comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32,
) float32 {
	if s.dominantNearendDetector.isNearendState() {
		return 1
	}
	cfg := suppressor.highBandsSuppression
	for ch := range s.numCaptureChannels {
		echoSum := sum(echoSpectrum[ch][1:16])
		noiseSum := sum(comfortNoiseSpectrum[ch][1:16])
		if echoSum > cfg.enrThreshold*noiseSum {
			return cfg.maxGainDuringEcho
		}
	}

	return 1
}

// activeParams returns the parameters of the current state.
func (s *suppressionGain) activeParams() *gainParameters {
	if s.dominantNearendDetector.isNearendState() {
		return &s.nearendParams
	}

	return &s.normalParams
}

// gainToNoAudibleEcho mirrors GainToNoAudibleEcho: it computes the gain
// to reduce the echo to a non audible level.
func (s *suppressionGain) gainToNoAudibleEcho(nearend, echo, masker, gain *[kFftLengthBy2Plus1]float32) {
	params := s.activeParams()
	for k := range gain {
		enr := echo[k] / (nearend[k] + 1) // Echo-to-nearend ratio.
		emr := echo[k] / (masker[k] + 1)  // Echo-to-masker (noise) ratio.
		gainK := float32(1)
		if enr > params.enrTransparent[k] && emr > params.emrTransparent[k] {
			gainK = (params.enrSuppress[k] - enr) / (params.enrSuppress[k] - params.enrTransparent[k])
			gainK = max(gainK, params.emrTransparent[k]/emr)
		}
		gain[k] = gainK
	}
}

// getMinGain mirrors GetMinGain: it computes the minimum gain as the
// attenuating gain to put the signal just above the zero sample values.
func (s *suppressionGain) getMinGain(
	suppressor suppressorConfig,
	weightedResidualEcho, lastNearend, lastEcho *[kFftLengthBy2Plus1]float32,
	lowNoiseRender, saturatedEcho bool,
	minGain *[kFftLengthBy2Plus1]float32,
) {
	if saturatedEcho {
		*minGain = [kFftLengthBy2Plus1]float32{}

		return
	}

	minEchoPower := s.echoAudibilityConfig.normalRenderLimit
	if lowNoiseRender {
		minEchoPower = s.echoAudibilityConfig.lowRenderLimit
	}
	for k := range minGain {
		minGain[k] = 1
		if weightedResidualEcho[k] > 0 {
			minGain[k] = min(minEchoPower/weightedResidualEcho[k], 1)
		}
	}

	if s.initialState && !suppressor.lfSmoothingDuringInitialPhase {
		return
	}
	dec := s.activeParams().maxDecFactorLf
	for k := 0; k <= suppressor.lastLfSmoothingBand; k++ {
		// Make sure the gains of the low frequencies do not decrease too
		// quickly after strong nearend.
		if lastNearend[k] > lastEcho[k] || k <= suppressor.lastPermanentLfSmoothingBand {
			minGain[k] = max(minGain[k], s.lastGain[k]*dec)
			minGain[k] = min(minGain[k], 1)
		}
	}
}

// getMaxGain mirrors GetMaxGain: it computes the maximum gain by limiting
// the gain increase from the previous gain.
func (s *suppressionGain) getMaxGain(floorFirstIncrease float32, maxGain *[kFftLengthBy2Plus1]float32) {
	inc := s.activeParams().maxIncFactor
	for k := range maxGain {
		maxGain[k] = min(max(s.lastGain[k]*inc, floorFirstIncrease), 1)
	}
}

// lowerBandGain mirrors LowerBandGain.
func (s *suppressionGain) lowerBandGain(
	suppressor suppressorConfig,
	lowNoiseRender, saturatedEcho bool,
	suppressorInput, residualEcho, comfortNoise [][kFftLengthBy2Plus1]float32,
	clockDrift bool,
	gain *[kFftLengthBy2Plus1]float32,
) {
	fill(gain[:], 1)
	var maxGain [kFftLengthBy2Plus1]float32
	s.getMaxGain(suppressor.floorFirstIncrease, &maxGain)

	for ch := range s.numCaptureChannels {
		var channelGain, nearend, weightedResidualEcho, minGain [kFftLengthBy2Plus1]float32
		s.nearendSmoothers[ch].average(suppressorInput[ch][:], nearend[:])

		// Weight the echo power in terms of audibility.
		weightEchoForAudibility(s.echoAudibilityConfig, &residualEcho[ch], &weightedResidualEcho)

		s.getMinGain(suppressor, &weightedResidualEcho, &s.lastNearend[ch], &s.lastEcho[ch], lowNoiseRender,
			saturatedEcho, &minGain)

		// Like upstream, the masker is the comfort noise of the first
		// channel.
		s.gainToNoAudibleEcho(&nearend, &weightedResidualEcho, &comfortNoise[0], &channelGain)

		// Clamp the gains.
		for k := range gain {
			channelGain[k] = max(min(channelGain[k], maxGain[k]), minGain[k])
			gain[k] = min(gain[k], channelGain[k])
		}

		// Store the data required for the gain computation of the next
		// block.
		s.lastNearend[ch] = nearend
		s.lastEcho[ch] = weightedResidualEcho
	}

	limitLowFrequencyGains(gain)
	// Use conservative high-frequency gains during clock-drift or when not
	// in dominant nearend.
	if !s.dominantNearendDetector.isNearendState() || clockDrift || suppressor.conservativeHfSuppression {
		limitHighFrequencyGains(suppressor.highFrequencySuppression, suppressor.conservativeHfSuppression, gain)
	}

	// Store the computed gains.
	s.lastGain = *gain

	// Transform the gains to the amplitude domain.
	for k := range gain {
		gain[k] = float32(math.Sqrt(float64(gain[k])))
	}
}

// getGain mirrors GetGain: it writes the gains of the first band to
// lowBandGain and returns the gain of the upper bands.
func (s *suppressionGain) getGain(
	suppressor suppressorConfig,
	configChanged bool,
	nearendSpectrum, echoSpectrum, residualEchoSpectrum, residualEchoSpectrumUnbounded [][kFftLengthBy2Plus1]float32,
	comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32,
	analyzer *renderSignalAnalyzer,
	state *aecState,
	render *block,
	clockDrift bool,
	lowBandGain *[kFftLengthBy2Plus1]float32,
) (highBandsGain float32) {
	if configChanged {
		s.updateStateDependingOnConfig(suppressor)
	}

	// Choose the residual echo spectrum for the dominant nearend detection.
	echo := residualEchoSpectrum
	if suppressor.dominantNearendDetection.useUnboundedEchoSpectrum {
		echo = residualEchoSpectrumUnbounded
	}

	// Update the nearend state selection.
	s.dominantNearendDetector.update(nearendSpectrum, echo, comfortNoiseSpectrum, s.initialState)

	// Compute the gain for the lower band.
	lowNoiseRender := s.lowRenderDetector.detect(render)
	s.lowerBandGain(suppressor, lowNoiseRender, state.saturatedEcho(), nearendSpectrum, residualEchoSpectrum,
		comfortNoiseSpectrum, clockDrift, lowBandGain)

	// Compute the gain for the upper bands.
	narrowPeakBand, hasNarrowPeakBand := analyzer.getNarrowPeakBand()

	return s.upperBandsGain(suppressor, echoSpectrum, comfortNoiseSpectrum, narrowPeakBand, hasNarrowPeakBand,
		state.saturatedEcho(), render, lowBandGain)
}

// isDominantNearend mirrors IsDominantNearend.
func (s *suppressionGain) isDominantNearend() bool { return s.dominantNearendDetector.isNearendState() }

// setInitialState mirrors SetInitialState: it toggles the usage of the
// initial state.
func (s *suppressionGain) setInitialState(state bool) { s.initialState = state }

// updateStateDependingOnConfig mirrors UpdateStateDependingOnConfig: it
// updates the internal state, such as the sizes and the parameters, when
// the config changes.
func (s *suppressionGain) updateStateDependingOnConfig(suppressor suppressorConfig) {
	for ch := range s.nearendSmoothers {
		s.nearendSmoothers[ch].updateMemoryLength(suppressor.nearendAverageBlocks)
	}
	s.dominantNearendDetector.setConfig(suppressor)
	s.nearendParams.setConfig(suppressor.lastLfBand, suppressor.firstHfBand, suppressor.nearendTuning)
	s.normalParams.setConfig(suppressor.lastLfBand, suppressor.firstHfBand, suppressor.normalTuning)
}
