// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// residualEchoEstimatorTest ports ResidualEchoEstimatorTest.
type residualEchoEstimatorTest struct {
	estimator         *residualEchoEstimator
	state             *aecState
	renderDelayBuffer *renderDelayBufferImpl
	e2Refined         [][kFftLengthBy2Plus1]float32
	s2Linear          [][kFftLengthBy2Plus1]float32
	y2                [][kFftLengthBy2Plus1]float32
	r2                [][kFftLengthBy2Plus1]float32
	r2Unbounded       [][kFftLengthBy2Plus1]float32
	render            *block
	h2                [][][kFftLengthBy2Plus1]float32
	impulseResponses  [][]float32
	random            *testRandom
	outputs           []subtractorOutput
	firstFrame        bool
}

func newResidualEchoEstimatorTest(
	numRenderChannels, numCaptureChannels int, config echoCanceller3Config,
) *residualEchoEstimatorTest {
	const (
		sampleRateHz = 48000
		level        = 10
	)
	spectra := func() [][kFftLengthBy2Plus1]float32 {
		return make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	}
	test := &residualEchoEstimatorTest{
		estimator:         newResidualEchoEstimator(config, numRenderChannels),
		state:             newAecState(config, numCaptureChannels),
		renderDelayBuffer: newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels),
		e2Refined:         spectra(),
		s2Linear:          spectra(),
		y2:                spectra(),
		r2:                spectra(),
		r2Unbounded:       spectra(),
		render:            newBlock(numBandsForRate(sampleRateHz), numRenderChannels),
		h2:                make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		impulseResponses:  make([][]float32, numCaptureChannels),
		random:            newTestRandom(42),
		outputs:           make([]subtractorOutput, numCaptureChannels),
		firstFrame:        true,
	}
	for ch := range numCaptureChannels {
		test.h2[ch] = make([][kFftLengthBy2Plus1]float32, 10)
		for partition := range test.h2[ch] {
			fill(test.h2[ch][partition][:], 0.01)
		}
		fill(test.h2[ch][2][:], 10)
		test.h2[ch][2][0] = 0.1
		test.impulseResponses[ch] = make([]float32, getTimeDomainLength(config.filter.refined.lengthBlocks))
		fill(test.outputs[ch].sRefined[:], 100)
		fill(test.e2Refined[ch][:], level)
		fill(test.y2[ch][:], level)
	}
	fill(test.s2Linear[0][:], level)

	return test
}

// runOneFrame ports RunOneFrame.
func (r *residualEchoEstimatorTest) runOneFrame(dominantNearend bool) {
	randomizeSampleVector(r.random, r.render.view(0, 0))
	r.renderDelayBuffer.insert(r.render)
	if r.firstFrame {
		r.renderDelayBuffer.reset()
		r.firstFrame = false
	}
	r.renderDelayBuffer.prepareCaptureProcessing()
	render := r.renderDelayBuffer.getRenderBuffer()

	r.state.update(delayEstimate{}, false, r.h2, r.impulseResponses, render, r.e2Refined, r.y2, r.outputs)
	r.estimator.estimate(r.state, render, r.s2Linear, r.y2, dominantNearend, r.r2, r.r2Unbounded)
}

// Port of ResidualEchoEstimatorMultiChannel.BasicTest. Like upstream, it
// only checks that the estimator runs.
func TestResidualEchoEstimator_BasicTest(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 4} {
		for _, numCaptureChannels := range []int{1, 2, 4} {
			test := newResidualEchoEstimatorTest(numRenderChannels, numCaptureChannels, defaultEchoCanceller3Config())
			assert.NotPanics(t, func() {
				for range 1993 {
					test.runOneFrame(false)
				}
			})
		}
	}
}

// Port of ResidualEchoEstimatorMultiChannel.ReverbTest.
func TestResidualEchoEstimator_ReverbTest(t *testing.T) {
	const (
		numFrames = 100
		epsilon   = 1e-4
	)
	referenceConfig := defaultEchoCanceller3Config()
	referenceConfig.epStrength.defaultLen = 0.95
	referenceConfig.epStrength.nearendLen = 0.95
	configUseNearendLen := referenceConfig
	configUseNearendLen.epStrength.nearendLen = 0.83

	referenceTest := newResidualEchoEstimatorTest(1, 1, referenceConfig)
	useNearendLenTest := newResidualEchoEstimatorTest(1, 1, configUseNearendLen)

	var acumEnergyReferenceR2, acumEnergyR2 float32
	for frame := range numFrames {
		dominantNearend := frame > numFrames/2
		referenceTest.runOneFrame(dominantNearend)
		useNearendLenTest.runOneFrame(dominantNearend)

		energyReferenceR2 := sum(referenceTest.r2[0][:])
		energyR2 := sum(useNearendLenTest.r2[0][:])
		if dominantNearend {
			assert.GreaterOrEqual(t, energyReferenceR2, energyR2)
		} else {
			assert.InDelta(t, energyReferenceR2, energyR2, epsilon)
		}
		acumEnergyReferenceR2 += energyReferenceR2
		acumEnergyR2 += energyR2

		if frame == numFrames/2 || frame == numFrames-1 {
			if dominantNearend {
				assert.Greater(t, acumEnergyReferenceR2, acumEnergyR2)
			} else {
				assert.InDelta(t, acumEnergyReferenceR2, acumEnergyR2, epsilon)
			}
		}
	}
}

const (
	// residualEchoModeCustom uses the stationarity properties of the
	// render signal and a reverb decay of 0.7 for the nearend regions. The
	// render signal is at a lower level outside of the first 15 blocks of
	// every 150 blocks.
	residualEchoModeCustom = "custom"
	// residualEchoModeNoReverb does not model the reverb in the nonlinear
	// mode, compensates the ERLE for onsets when the nearend is dominant,
	// and takes the render power over 2 blocks before and 3 blocks after
	// the delay. The render signal is not silent in blocks 0 to 19.
	residualEchoModeNoReverb = "noreverb"
	// residualEchoModeNoEcho has filters that are zero and an output of
	// the refined filter that is the capture signal from the start. The
	// AecState is not told about a delay change.
	residualEchoModeNoEcho = "noecho"

	residualEchoScenarioBlocks = 2400
)

// residualEchoScenario holds the state of the upstream reference scenario
// of the residual echo estimator: an AecState that is fed with synthetic
// filters and subtractor outputs, like in the upstream unit test of the
// estimator.
//
// The render signal is noise at 16 kHz, silent in blocks 0 to 19 and 400
// to 559 and weak in blocks 820 to 839. The filters have a peak in the block of their delay. In blocks
// 0 to 799 the output of the refined filter is a tenth of the capture
// signal. The AecState is told about a delay change at block 800. From
// then on the filters are zero and the output of the refined filter is
// the capture signal. The capture signal is flagged as saturated in
// blocks 60 to 69, 300 to 309, where the echo estimate is strong, and
// 1000 to 1009. An external delay is reported from block 100 on. The
// nearend is dominant in the first 10 blocks of every 40 blocks. The
// spectra are drawn at random.
type residualEchoScenario struct {
	mode                  string
	numRender, numCapture int
	config                echoCanceller3Config
	state                 *aecState
	renderDelayBuffer     *renderDelayBufferImpl
	random                *testRandom
	render                *block
	outputs               []subtractorOutput
	y2, e2, s2Linear      [][kFftLengthBy2Plus1]float32
	h2                    [][][kFftLengthBy2Plus1]float32
	impulseResponses      [][]float32
}

func newResidualEchoScenario(numRender, numCapture int, mode string) *residualEchoScenario {
	config := defaultEchoCanceller3Config()
	switch mode {
	case residualEchoModeCustom:
		config.echoAudibility.useStationarityProperties = true
		config.epStrength.nearendLen = 0.7
	case residualEchoModeNoReverb:
		config.echoModel.modelReverbInNonlinearMode = false
		config.epStrength.erleOnsetCompensationInDominantNearend = true
		config.echoModel.renderPreWindowSize = 2
		config.echoModel.renderPostWindowSize = 3
	}
	spectra := func() [][kFftLengthBy2Plus1]float32 { return make([][kFftLengthBy2Plus1]float32, numCapture) }
	scenario := &residualEchoScenario{
		mode:              mode,
		numRender:         numRender,
		numCapture:        numCapture,
		config:            config,
		state:             newAecState(config, numCapture),
		renderDelayBuffer: newRenderDelayBufferImpl(config, 16000, numRender),
		random:            newTestRandom(42),
		render:            newBlock(1, numRender),
		outputs:           make([]subtractorOutput, numCapture),
		y2:                spectra(),
		e2:                spectra(),
		s2Linear:          spectra(),
		h2:                make([][][kFftLengthBy2Plus1]float32, numCapture),
		impulseResponses:  make([][]float32, numCapture),
	}
	for ch := range numCapture {
		scenario.h2[ch] = make([][kFftLengthBy2Plus1]float32, config.filter.refined.lengthBlocks)
		scenario.impulseResponses[ch] = make([]float32, getTimeDomainLength(config.filter.refined.lengthBlocks))
	}

	return scenario
}

// suffix is the end of the keys of the scenario in the reference file:
// "name render_channels capture_channels mode".
func (s *residualEchoScenario) suffix() string {
	return fmt.Sprintf(" %d %d %s", s.numRender, s.numCapture, s.mode)
}

func (s *residualEchoScenario) converged(index int) bool {
	return index < 800 && s.mode != residualEchoModeNoEcho
}

// drawFilters draws the filters of every capture channel.
func (s *residualEchoScenario) drawFilters(converged bool) {
	for ch := range s.numCapture {
		delay := 2 + ch
		for partition := range s.h2[ch] {
			for k := range s.h2[ch][partition] {
				draw := s.random.randFloat()
				level := float32(0.01)
				if partition == delay {
					level = 10
				}
				s.h2[ch][partition][k] = 0
				if converged {
					s.h2[ch][partition][k] = level * (0.5 + draw)
				}
			}
		}
		for k := range s.impulseResponses[ch] {
			draw := s.random.randFloat()
			switch {
			case !converged:
				s.impulseResponses[ch][k] = 0
			case k == delay*kBlockSize+1:
				s.impulseResponses[ch][k] = 1
			default:
				s.impulseResponses[ch][k] = 0.002 * (draw - 0.5)
			}
		}
	}
}

func (s *residualEchoScenario) renderGain(index int) float32 {
	switch {
	case inRange(index, 820, 840):
		return 0.001
	case (index < 20 && s.mode != residualEchoModeNoReverb) || inRange(index, 400, 560):
		return 0
	case s.mode == residualEchoModeCustom && index%150 >= 15:
		return 0.03
	default:
		return 1
	}
}

// drawCapture draws the subtractor output and the spectra of a capture
// channel.
func (s *residualEchoScenario) drawCapture(ch, index int) {
	converged := s.converged(index)
	captureAmplitude := float32(2000)
	if inRange(index, 300, 310) {
		captureAmplitude = 30000
	}
	var capture [kBlockSize]float32
	randomizeSampleVectorAmplitude(s.random, capture[:], captureAmplitude)
	output := &s.outputs[ch]
	for k, sample := range capture {
		output.eRefined[k] = sample
		if converged {
			output.eRefined[k] = 0.1 * sample
		}
		output.eCoarse[k] = output.eRefined[k]
		output.sRefined[k] = sample - output.eRefined[k]
		output.sCoarse[k] = sample - output.eRefined[k]
	}
	output.computeMetrics(capture[:])

	for k := range s.y2[ch] {
		drawY := s.random.randFloat()
		drawE := s.random.randFloat()
		drawS := s.random.randFloat()
		s.y2[ch][k] = 1e9 * (0.5 + drawY) / float32(ch+1)
		if converged {
			s.e2[ch][k] = s.y2[ch][k] / (5 + 10*drawE)
		} else {
			s.e2[ch][k] = s.y2[ch][k] / (1 + drawE)
		}
		s.s2Linear[ch][k] = s.y2[ch][k] * (0.5 + 0.5*drawS)
	}
}

// insertRender draws and inserts the render signal of a block.
func (s *residualEchoScenario) insertRender(index int) {
	for ch := range s.numRender {
		channel := s.render.view(0, ch)
		randomizeSampleVector(s.random, channel)
		for k := range channel {
			channel[k] *= s.renderGain(index)
		}
	}
	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
}

// step updates the AecState with the data of a block.
func (s *residualEchoScenario) step(index int) {
	if index == 0 || index == 800 {
		s.drawFilters(s.converged(index))
	}
	s.insertRender(index)

	for ch := range s.numCapture {
		s.drawCapture(ch, index)
	}

	s.state.updateCaptureSaturation(inRange(index, 60, 70) || inRange(index, 300, 310) || inRange(index, 1000, 1010))
	if index == 800 && s.mode != residualEchoModeNoEcho {
		s.state.handleEchoPathChange(echoPathVariability{delayChange: delayAdjustmentNewDetectedDelay})
	}
	s.state.update(delayEstimate{quality: delayQualityRefined, delay: 2}, index >= 100, s.h2, s.impulseResponses,
		s.renderDelayBuffer.getRenderBuffer(), s.e2, s.y2, s.outputs)
}

// residualEchoScenarioDominantNearend tells whether the nearend is
// dominant in a block.
func residualEchoScenarioDominantNearend(index int) bool { return index%40 < 10 }

// residualEchoScenarioDumpsSpectra tells whether the reference holds the
// spectra of a block.
func residualEchoScenarioDumpsSpectra(index int) bool {
	return index%200 == 199 || (index < 140 && index%20 == 5) || index == 305 ||
		(inRange(index, 400, 600) && index%40 == 5) || index == 825 || index == 1005
}

// TestResidualEchoEstimator_MatchesUpstream compares the residual echo
// power with the upstream ResidualEchoEstimator at the pinned commit: its
// sum over the bands every 3rd block, with the state of the AecState, and
// the spectra of some blocks, for the last capture channel.
func TestResidualEchoEstimator_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		numRender, numCapture int
		mode                  string
	}{{1, 1, "default"}, {2, 2, residualEchoModeCustom}, {1, 2, residualEchoModeNoReverb}, {1, 1, residualEchoModeNoEcho}} {
		scenario := newResidualEchoScenario(tc.numRender, tc.numCapture, tc.mode)
		t.Run(scenario.suffix()[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/residual_echo_estimator_upstream.txt", scenario.suffix())
			estimator := newResidualEchoEstimator(scenario.config, tc.numRender)
			r2 := make([][kFftLengthBy2Plus1]float32, tc.numCapture)
			r2Unbounded := make([][kFftLengthBy2Plus1]float32, tc.numCapture)
			last := tc.numCapture - 1
			state := scenario.state

			for index := range residualEchoScenarioBlocks {
				scenario.step(index)
				estimator.estimate(state, scenario.renderDelayBuffer.getRenderBuffer(), scenario.s2Linear, scenario.y2,
					residualEchoScenarioDominantNearend(index), r2, r2Unbounded)

				if index%3 == 0 {
					// The block index, the sums of the residual echo power,
					// then the state: whether the linear estimate is usable,
					// the echo is saturated and the transparent mode is
					// active, the filter delay, and whether the neural
					// estimator is active.
					want := reference.next("levels")
					require.Equal(t, []float32{
						float32(index), boolValue(state.usableLinearEstimate()), boolValue(state.saturatedEcho()),
						boolValue(state.transparentModeActive()), float32(state.minDirectPathFilterDelay()), 0,
					}, append([]float32{want[0]}, want[3:]...), "block %d", index)
					for i, got := range []float32{sum(r2[last][:]), sum(r2Unbounded[last][:])} {
						require.InDelta(t, want[1+i], got, float64(want[1+i])*residualEchoReferenceEpsilon, "block %d", index)
					}
				}
				if residualEchoScenarioDumpsSpectra(index) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					assertResidualEchoSpectrum(t, reference.next("r2"), r2[last][:], index)
					assertResidualEchoSpectrum(t, reference.next("r2_unbounded"), r2Unbounded[last][:], index)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}

// residualEchoReferenceEpsilon bounds the difference to the upstream
// residual echo power, relative to its value. The only difference in the
// inputs is the rounding of the FFT of the render signal.
const residualEchoReferenceEpsilon = 1e-4

// assertResidualEchoSpectrum compares a spectrum with the reference,
// relative to its largest value.
func assertResidualEchoSpectrum(t *testing.T, want, got []float32, index int) {
	t.Helper()
	var largest float32
	for _, value := range want {
		largest = max(largest, abs32(value))
	}
	require.InDeltaSlice(t, want, got, float64(largest)*residualEchoReferenceEpsilon, "block %d", index)
}
