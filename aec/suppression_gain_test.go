// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of SuppressionGainTest.BasicGainComputation: a sanity check that
// the gains are correctly computed.
func TestSuppressionGain_BasicGainComputation(t *testing.T) {
	const (
		numRenderChannels  = 1
		numCaptureChannels = 2
		sampleRateHz       = 16000
	)
	config := defaultEchoCanceller3Config()
	gain := newSuppressionGain(config, numCaptureChannels)
	analyzer := newRenderSignalAnalyzer(config)
	spectra := func() [][kFftLengthBy2Plus1]float32 {
		return make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	}
	e2, s2, y2, r2, r2Unbounded, n2 := spectra(), spectra(), spectra(), spectra(), spectra(), spectra()
	var lowBandGain [kFftLengthBy2Plus1]float32
	outputs := make([]subtractorOutput, numCaptureChannels)
	render := newBlock(numBandsForRate(sampleRateHz), numRenderChannels)
	state := newAecState(config, numCaptureChannels)
	sub := newSubtractor(config, numRenderChannels, numCaptureChannels)
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels)
	fillAll := func(spectra [][kFftLengthBy2Plus1]float32, value float32) {
		for ch := range spectra {
			fill(spectra[ch][:], value)
		}
	}
	updateState := func() {
		state.update(delayEstimate{}, false, sub.filterFrequencyResponses(), sub.filterImpulseResponses(),
			renderDelayBuffer.getRenderBuffer(), e2, y2, outputs)
	}
	getGain := func() {
		gain.getGain(config.suppressor, false, e2, s2, r2, r2Unbounded, n2, analyzer, state, render, false,
			&lowBandGain)
	}
	assertGains := func(want float64) {
		for _, value := range lowBandGain {
			assert.InDelta(t, want, value, 0.001)
		}
	}

	// Ensure that a strong noise is detected to mask any echoes.
	fillAll(e2, 10)
	fillAll(y2, 10)
	fillAll(r2, 0.1)
	fillAll(r2Unbounded, 0.1)
	fillAll(n2, 100)

	// Ensure that the gain is no longer forced to zero.
	for range kNumBlocksPerSecond/5 + 2 {
		updateState()
	}
	for range 100 {
		updateState()
		getGain()
	}
	assertGains(1)

	// Ensure that a strong nearend is detected to mask any echoes.
	fillAll(e2, 100)
	fillAll(y2, 100)
	fillAll(r2, 0.1)
	fillAll(r2Unbounded, 0.1)
	fillAll(s2, 0.1)
	fillAll(n2, 0)
	for range 100 {
		updateState()
		getGain()
	}
	assertGains(1)

	// Add a strong echo to one of the channels and ensure that it is
	// suppressed.
	fill(e2[1][:], 1000000000)
	fill(r2[1][:], 10000000000000)
	fill(r2Unbounded[1][:], 10000000000000)
	for range 10 {
		getGain()
	}
	assertGains(0)
}

// Port of SuppressionGainTest.UpdateStateDependingOnConfig.
func TestSuppressionGain_UpdateStateDependingOnConfig(t *testing.T) {
	config := defaultEchoCanceller3Config()
	config.suppressor.nearendTuning.maxIncFactor = 2
	config.suppressor.normalTuning.maxDecFactorLf = 0.2
	gain := newSuppressionGain(config, 1)

	// Initial call to set up the state.
	gain.updateStateDependingOnConfig(config.suppressor)
	assert.Equal(t, float32(2), gain.nearendParams.maxIncFactor)
	assert.Equal(t, float32(0.2), gain.normalParams.maxDecFactorLf)

	// Change the config and verify that the state is updated.
	newConfig := config
	newConfig.suppressor.nearendTuning.maxIncFactor = 3
	newConfig.suppressor.normalTuning.maxDecFactorLf = 0.3
	gain.updateStateDependingOnConfig(newConfig.suppressor)
	assert.Equal(t, float32(3), gain.nearendParams.maxIncFactor)
	assert.Equal(t, float32(0.3), gain.normalParams.maxDecFactorLf)
}

const (
	// suppressionGainModeSubband uses the subband nearend detector.
	suppressionGainModeSubband = "subband"
	// suppressionGainModeConservative suppresses the high frequencies
	// conservatively, detects the nearend with the bounded residual echo,
	// does not smooth the low frequency gains in the initial state, bounds
	// the upper bands gain more, and has audibility thresholds that differ
	// per frequency region.
	suppressionGainModeConservative = "conservative"
)

// suppressionGainScenarioConfig returns the configuration of a mode of
// the reference scenario.
func suppressionGainScenarioConfig(mode string) echoCanceller3Config {
	config := defaultEchoCanceller3Config()
	suppressor := &config.suppressor
	switch mode {
	case suppressionGainModeSubband:
		suppressor.useSubbandNearendDetection = true
		suppressor.subbandNearendDetection = subbandNearendDetectionConfig{
			nearendAverageBlocks: 3,
			subband1:             subbandRegion{low: 2, high: 8},
			subband2:             subbandRegion{low: 10, high: 30},
			nearendThreshold:     1.5,
			snrThreshold:         20,
		}
	case suppressionGainModeConservative:
		suppressor.conservativeHfSuppression = true
		suppressor.dominantNearendDetection.useUnboundedEchoSpectrum = false
		suppressor.lfSmoothingDuringInitialPhase = false
		suppressor.lastPermanentLfSmoothingBand = 2
		suppressor.highBandsSuppression.enrThreshold = 0.5
		suppressor.highBandsSuppression.maxGainDuringEcho = 0.3
		suppressor.highBandsSuppression.antiHowlingGain = 0.5
		suppressor.highFrequencySuppression = highFrequencySuppressionConfig{limitingGainBand: 10, bandsInLimitingGain: 3}
		config.echoAudibility.audibilityThresholdLf = 5
		config.echoAudibility.audibilityThresholdHf = 20
	}

	return config
}

// suppressionGainScenarioNewConfig applies the changes that are made to
// the suppressor configuration at block 1200 of the reference scenario.
func suppressionGainScenarioNewConfig(suppressor *suppressorConfig) {
	suppressor.nearendAverageBlocks = 2
	suppressor.normalTuning = suppressorTuning{
		maskLf:         maskingThresholds{enrTransparent: 0.2, enrSuppress: 0.5, emrTransparent: 0.2},
		maskHf:         maskingThresholds{enrTransparent: 0.05, enrSuppress: 0.2, emrTransparent: 0.4},
		maxIncFactor:   1.5,
		maxDecFactorLf: 0.5,
	}
	suppressor.nearendTuning = suppressorTuning{
		maskLf:         maskingThresholds{enrTransparent: 2, enrSuppress: 3, emrTransparent: 0.5},
		maskHf:         maskingThresholds{enrTransparent: 0.3, enrSuppress: 0.6, emrTransparent: 0.2},
		maxIncFactor:   3,
		maxDecFactorLf: 0.1,
	}
	suppressor.lastLfSmoothingBand = 8
	suppressor.lastLfBand = 10
	suppressor.firstHfBand = 20
	suppressor.floorFirstIncrease = 0.001
	suppressor.dominantNearendDetection.holdDuration = 20
}

// suppressionGainScenario holds the inputs of the suppression gain in the
// upstream reference scenario.
//
// The spectra are drawn at random, with levels that change every 25
// blocks: the nearend is strong in one period out of three, the echo in
// one out of four, shifted per channel, and the noise in one out of
// seven. The band 16 of the echo, which is not used, is much stronger.
// The residual echo has one of six levels, and its unbounded variant is a
// half or a hundredth of it.
//
// The render signal has three bands. Its first band is noise, weak in
// blocks 0 to 149 and 1340 to 1479, an impulse per block in blocks 1480
// to 1519, stronger noise in blocks 1520 to 1579 and 1580 to 1639, a tone in
// band 58 in blocks 500 to 599 and in band 20 in blocks 900 to 999. Its
// upper bands are noise, strong in one period out of two.
type suppressionGainScenario struct {
	numRender, numCapture int
	random                *testRandom
	render                *block
	renderDelayBuffer     *renderDelayBufferImpl
	analyzer              *renderSignalAnalyzer
	nearend, echo         [][kFftLengthBy2Plus1]float32
	residual, unbounded   [][kFftLengthBy2Plus1]float32
	noise                 [][kFftLengthBy2Plus1]float32
}

func newSuppressionGainScenario(numRender, numCapture int, config echoCanceller3Config) *suppressionGainScenario {
	spectra := func() [][kFftLengthBy2Plus1]float32 { return make([][kFftLengthBy2Plus1]float32, numCapture) }

	return &suppressionGainScenario{
		numRender:         numRender,
		numCapture:        numCapture,
		random:            newTestRandom(7),
		render:            newBlock(3, numRender),
		renderDelayBuffer: newRenderDelayBufferImpl(config, 48000, numRender),
		analyzer:          newRenderSignalAnalyzer(config),
		nearend:           spectra(),
		echo:              spectra(),
		residual:          spectra(),
		unbounded:         spectra(),
		noise:             spectra(),
	}
}

// suppressionGainLevel returns the level of a spectrum in a period.
func suppressionGainLevel(period, cycle int, strong, weak float32) float32 {
	if period%cycle == 0 {
		return strong
	}

	return weak
}

func (s *suppressionGainScenario) drawSpectra(index int) {
	period := index / 25
	for ch := range s.numCapture {
		nearendLevel := suppressionGainLevel(period, 3, 1e6, 1e3)
		echoLevel := suppressionGainLevel(period+ch, 4, 1e7, 1e2)
		noiseLevel := suppressionGainLevel(period, 7, 1e4, 1e2)
		residualLevel := [6]float32{1e3, 1e5, 1, 1e3, 1e7, 1e3}[period%6]
		unboundedFactor := suppressionGainLevel(period, 2, 0.5, 0.01)
		for k := range kFftLengthBy2Plus1 {
			drawNearend := s.random.randFloat()
			drawEcho := s.random.randFloat()
			drawNoise := s.random.randFloat()
			s.nearend[ch][k] = nearendLevel * (0.5 + drawNearend)
			s.echo[ch][k] = echoLevel * (0.5 + drawEcho)
			s.residual[ch][k] = residualLevel * (0.5 + drawEcho)
			s.unbounded[ch][k] = unboundedFactor * s.residual[ch][k]
			s.noise[ch][k] = noiseLevel * (0.5 + drawNoise)
		}
		// A band that the upper bands gain must not use.
		s.echo[ch][16] *= 1e6
	}
}

// suppressionGainLowAmplitude returns the amplitude of the first band of
// the render signal of a block.
func suppressionGainLowAmplitude(index int) float32 {
	switch {
	case index < 150 || inRange(index, 1340, 1480):
		return 20
	case inRange(index, 1520, 1580):
		return 80
	case inRange(index, 1580, 1640):
		return 150
	default:
		return 3000
	}
}

// suppressionGainToneBand returns the band of the tone in the render
// signal of a block, or 0 when there is none.
func suppressionGainToneBand(index int) int {
	switch {
	case inRange(index, 500, 600):
		return 58
	case inRange(index, 900, 1000):
		return 20
	default:
		return 0
	}
}

func (s *suppressionGainScenario) drawRender(index int) {
	highAmplitude := suppressionGainLevel(index/25, 2, 6000, 40)
	toneBand := suppressionGainToneBand(index)
	for ch := range s.numRender {
		lowBand := s.render.view(0, ch)
		randomizeSampleVectorAmplitude(s.random, lowBand, suppressionGainLowAmplitude(index))
		randomizeSampleVectorAmplitude(s.random, s.render.view(1, ch), highAmplitude)
		randomizeSampleVectorAmplitude(s.random, s.render.view(2, ch), highAmplitude/float32(ch+2))
		if inRange(index, 1480, 1520) {
			clear(lowBand)
			lowBand[0] = 300
		}
		if toneBand > 0 {
			for k := range lowBand {
				phase := 2.0 * math.Pi * float64(toneBand) * float64(index*kBlockSize+k) / 128.0
				lowBand[k] = float32(10000.0 * math.Sin(phase))
			}
		}
	}
}

// step draws the inputs of a block and updates the render signal
// analyzer.
func (s *suppressionGainScenario) step(index int) {
	s.drawSpectra(index)
	s.drawRender(index)
	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
	s.analyzer.update(s.renderDelayBuffer.getRenderBuffer(), 0, true)
}

// TestSuppressionGain_MatchesUpstream compares the gains with the
// upstream SuppressionGain at the pinned commit: the upper bands gain,
// the sum of the gains of the first band and the nearend state every 3rd
// block, and the gains of the first band every 30th block.
//
// The AecState is the one of the default scenario of the residual echo
// estimator, which tells when the echo is saturated. A clock drift is
// flagged in blocks 700 to 799. The initial state ends at block 200 and
// is set again in blocks 1700 to 1899. At block 1200 the suppressor
// configuration is replaced by one with other tunings and bands. Each
// line of the reference file is keyed by
// "name render_channels capture_channels mode".
func TestSuppressionGain_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		numRender, numCapture int
		mode                  string
	}{{1, 1, aecStateModeDefault}, {2, 2, suppressionGainModeSubband}, {2, 2, suppressionGainModeConservative}} {
		suffix := fmt.Sprintf(" %d %d %s", tc.numRender, tc.numCapture, tc.mode)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/suppression_gain_upstream.txt", suffix)
			stateScenario := newResidualEchoScenario(tc.numRender, tc.numCapture, aecStateModeDefault)
			config := suppressionGainScenarioConfig(tc.mode)
			scenario := newSuppressionGainScenario(tc.numRender, tc.numCapture, config)
			gain := newSuppressionGain(config, tc.numCapture)
			suppressor := config.suppressor
			var lowBandGain [kFftLengthBy2Plus1]float32

			for index := range residualEchoScenarioBlocks {
				stateScenario.step(index)
				scenario.step(index)
				switch index {
				case 200, 1900:
					gain.setInitialState(false)
				case 1700:
					gain.setInitialState(true)
				case 1200:
					suppressionGainScenarioNewConfig(&suppressor)
				}
				highBandsGain := gain.getGain(suppressor, index == 1200, scenario.nearend, scenario.echo,
					scenario.residual, scenario.unbounded, scenario.noise, scenario.analyzer, stateScenario.state,
					scenario.render, inRange(index, 700, 800), &lowBandGain)

				if index%3 == 0 {
					// The block index, the upper bands gain, the sum of the
					// gains of the first band, whether the nearend is
					// dominant and the echo is saturated, and the narrow
					// peak band of the render signal.
					narrowPeakBand := float32(-1)
					if band, found := scenario.analyzer.getNarrowPeakBand(); found {
						narrowPeakBand = float32(band)
					}
					got := []float32{
						float32(index), highBandsGain, sum(lowBandGain[:]), boolValue(gain.isDominantNearend()),
						boolValue(stateScenario.state.saturatedEcho()), narrowPeakBand,
					}
					require.InDeltaSlice(t, reference.next("gains"), got, 1e-4, "block %d", index)
				}
				if index%30 == 7 {
					require.InDeltaSlice(t, reference.next("gain"), lowBandGain[:], 1e-5, "block %d", index)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
