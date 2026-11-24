// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aecStateNormalUsageTest holds the state of RunNormalUsageTest.
type aecStateNormalUsageTest struct {
	state                            *aecState
	renderDelayBuffer                *renderDelayBufferImpl
	e2Refined, y2                    [][kFftLengthBy2Plus1]float32
	render                           *block
	capture                          [][kBlockSize]float32
	outputs                          []subtractorOutput
	convergedFilterFrequencyResponse [][][kFftLengthBy2Plus1]float32
	impulseResponse                  [][]float32
}

func newAecStateNormalUsageTest(numRenderChannels, numCaptureChannels int) *aecStateNormalUsageTest {
	const sampleRateHz = 48000
	config := defaultEchoCanceller3Config()
	test := &aecStateNormalUsageTest{
		state:             newAecState(config, numCaptureChannels),
		renderDelayBuffer: newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels),
		e2Refined:         make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		y2:                make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		render:            newBlock(numBandsForRate(sampleRateHz), numRenderChannels),
		capture:           make([][kBlockSize]float32, numCaptureChannels),
		outputs:           make([]subtractorOutput, numCaptureChannels),

		convergedFilterFrequencyResponse: make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		impulseResponse:                  make([][]float32, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		fill(test.outputs[ch].sRefined[:], 100)
		fill(test.outputs[ch].eRefined[:], 100)
		fill(test.capture[ch][:], 1000)
		test.convergedFilterFrequencyResponse[ch] = make([][kFftLengthBy2Plus1]float32, 10)
		for partition := range test.convergedFilterFrequencyResponse[ch] {
			fill(test.convergedFilterFrequencyResponse[ch][partition][:], 0.01)
		}
		test.impulseResponse[ch] = make([]float32, getTimeDomainLength(config.filter.refined.lengthBlocks))
	}
	fill(test.convergedFilterFrequencyResponse[0][2][:], 100)
	test.convergedFilterFrequencyResponse[0][2][0] = 1

	return test
}

func (a *aecStateNormalUsageTest) fillRender(numBands int, value float32) {
	for band := range numBands {
		for ch := range a.render.numChannels {
			fill(a.render.view(band, ch), value)
		}
	}
}

// update computes the subtractor metrics and updates the state.
func (a *aecStateNormalUsageTest) update() {
	for ch := range a.outputs {
		a.outputs[ch].computeMetrics(a.capture[ch][:])
	}
	a.state.update(delayEstimate{quality: delayQualityRefined, delay: 10}, true,
		a.convergedFilterFrequencyResponse, a.impulseResponse, a.renderDelayBuffer.getRenderBuffer(),
		a.e2Refined, a.y2, a.outputs)
}

func (a *aecStateNormalUsageTest) insertAndUpdate(times int) {
	for range times {
		a.renderDelayBuffer.insert(a.render)
		a.update()
	}
}

func (a *aecStateNormalUsageTest) fillSpectra(spectra [][kFftLengthBy2Plus1]float32, value float32) {
	for ch := range spectra {
		fill(spectra[ch][:], value)
	}
}

// verifyUsability ports the first part of RunNormalUsageTest: the
// usability of the linear filter and the active render detection.
func (a *aecStateNormalUsageTest) verifyUsability(t *testing.T) {
	t.Helper()
	state := a.state

	// Verify that linear AEC usability is true when the filter is
	// converged.
	a.fillRender(a.render.numBands, 101)
	a.insertAndUpdate(3000)
	assert.True(t, state.usableLinearEstimate())

	// Verify that linear AEC usability becomes false after an echo path
	// change is reported.
	state.handleEchoPathChange(echoPathVariability{delayChange: delayAdjustmentNewDetectedDelay})
	a.update()
	assert.False(t, state.usableLinearEstimate())

	// Verify that the active render detection works as intended.
	a.fillRender(1, 101)
	a.renderDelayBuffer.insert(a.render)
	state.handleEchoPathChange(echoPathVariability{gainChange: true, delayChange: delayAdjustmentNewDetectedDelay})
	a.update()
	assert.False(t, state.activeRender())

	a.insertAndUpdate(1000)
	assert.True(t, state.activeRender())
}

// verifyErl ports the part of RunNormalUsageTest that verifies that the
// ERL is properly estimated.
func (a *aecStateNormalUsageTest) verifyErl(t *testing.T) {
	t.Helper()
	a.fillRender(a.render.numBands, 0)
	for ch := range a.render.numChannels {
		a.render.view(0, ch)[0] = 5000
	}
	for k := range a.renderDelayBuffer.getRenderBuffer().getFftBuffer() {
		a.renderDelayBuffer.insert(a.render)
		if k == 0 {
			a.renderDelayBuffer.reset()
		}
		a.renderDelayBuffer.prepareCaptureProcessing()
	}

	a.fillSpectra(a.y2, 10*10000*10000)
	for range 1000 {
		a.update()
	}

	require.True(t, a.state.usableLinearEstimate())
	erl := a.state.erl()
	assert.Equal(t, erl[0], erl[1]) //nolint:testifylint // Exact upstream check.
	for k := 1; k < len(erl)-1; k++ {
		want := float32(1000)
		if k%2 == 0 {
			want = 10
		}
		assert.InDelta(t, want, erl[k], 0.1)
	}
	assert.Equal(t, erl[len(erl)-2], erl[len(erl)-1]) //nolint:testifylint // Exact upstream check.
}

// verifyErle ports the part of RunNormalUsageTest that verifies that the
// ERLE is properly estimated. The render spectrum only has energy in the
// even bands.
func (a *aecStateNormalUsageTest) verifyErle(t *testing.T) {
	t.Helper()
	const lowFrequencyLimit = 32
	a.fillSpectra(a.e2Refined, 1*10000*10000)
	a.fillSpectra(a.y2, 10*a.e2Refined[0][0])
	for range 1000 {
		a.update()
	}
	require.True(t, a.state.usableLinearEstimate())
	erle := &a.state.erle(true)[0]
	assert.Equal(t, erle[0], erle[1]) //nolint:testifylint // Exact upstream check.
	for k := 2; k < lowFrequencyLimit; k += 2 {
		assert.InDelta(t, 4, erle[k], 0.1)
	}
	for k := lowFrequencyLimit; k < len(erle)-1; k += 2 {
		assert.InDelta(t, 1.5, erle[k], 0.1)
	}
	assert.Equal(t, erle[len(erle)-2], erle[len(erle)-1]) //nolint:testifylint // Exact upstream check.

	a.fillSpectra(a.e2Refined, 1*10000*10000)
	a.fillSpectra(a.y2, 5*a.e2Refined[0][0])
	for range 1000 {
		a.update()
	}
	require.True(t, a.state.usableLinearEstimate())
	erle = &a.state.erle(true)[0]
	assert.Equal(t, erle[0], erle[1]) //nolint:testifylint // Exact upstream check.
	for k := 1; k < len(erle)-1; k++ {
		want := float32(1)
		switch {
		case k%2 == 0 && k < lowFrequencyLimit:
			want = 4
		case k%2 == 0:
			want = 1.5
		}
		assert.InDelta(t, want, erle[k], 0.1)
	}
	assert.Equal(t, erle[len(erle)-2], erle[len(erle)-1]) //nolint:testifylint // Exact upstream check.
}

// Port of AecStateMultiChannel.NormalUsage: the general functionality of
// AecState.
func TestAecState_NormalUsage(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 8} {
		for _, numCaptureChannels := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("%d_%d", numRenderChannels, numCaptureChannels), func(t *testing.T) {
				t.Parallel()
				test := newAecStateNormalUsageTest(numRenderChannels, numCaptureChannels)
				test.verifyUsability(t)
				test.verifyErl(t)
				test.verifyErle(t)
			})
		}
	}
}

// Port of AecState.ConvergedFilterDelay. Like upstream, it only checks
// that the state can be updated with filters of every delay.
func TestAecState_ConvergedFilterDelay(t *testing.T) {
	const (
		filterLengthBlocks = 10
		numCaptureChannels = 1
	)
	config := defaultEchoCanceller3Config()
	state := newAecState(config, numCaptureChannels)
	renderDelayBuffer := newRenderDelayBufferImpl(config, 48000, 1)
	e2Refined := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	y2 := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	outputs := make([]subtractorOutput, numCaptureChannels)
	fill(outputs[0].sRefined[:], 100)
	var capture [kBlockSize]float32

	frequencyResponse := [][][kFftLengthBy2Plus1]float32{make([][kFftLengthBy2Plus1]float32, filterLengthBlocks)}
	for partition := range frequencyResponse[0] {
		fill(frequencyResponse[0][partition][:], 0.01)
	}
	impulseResponse := [][]float32{make([]float32, getTimeDomainLength(config.filter.refined.lengthBlocks))}

	for k := range filterLengthBlocks {
		clear(impulseResponse[0])
		impulseResponse[0][k*kBlockSize+1] = 1

		state.handleEchoPathChange(echoPathVariability{})
		outputs[0].computeMetrics(capture[:])
		assert.NotPanics(t, func() {
			state.update(delayEstimate{}, false, frequencyResponse, impulseResponse,
				renderDelayBuffer.getRenderBuffer(), e2Refined, y2, outputs)
		})
	}
}

// aecStateFlags returns the flags and the delays of the state, in the
// order of the reference, then the number of bands where the residual
// echo is scaled down to zero.
func aecStateFlags(state *aecState) []float32 {
	reportedDelay := float32(-1)
	if delay, found := state.externalDelayBlocks(); found {
		reportedDelay = float32(delay.delay)
	}
	var scaling [kFftLengthBy2Plus1]float32
	state.getResidualEchoScaling(scaling[:])
	zeroScalings := 0
	for _, value := range scaling {
		if value == 0 {
			zeroScalings++
		}
	}

	return []float32{
		boolValue(state.usableLinearEstimate()),
		boolValue(state.useLinearFilterOutput()),
		boolValue(state.activeRender()),
		boolValue(state.saturatedCapture()),
		boolValue(state.saturatedEcho()),
		boolValue(state.transparentModeActive()),
		boolValue(state.transitionTriggered()),
		float32(state.minDirectPathFilterDelay()),
		float32(state.filterLengthBlocks()),
		boolValue(state.useStationarityProperties()),
		reportedDelay,
		float32(zeroScalings),
	}
}

// assertAecStateSpectrum compares a spectrum with the next line of the
// reference that has this name, bit for bit.
func assertAecStateSpectrum(t *testing.T, reference *referenceCursor, name string, got []float32) {
	t.Helper()
	assert.Equal(t, reference.next(name), got, name)
}

// assertAecStateSpectra compares the spectra of a block with the
// reference. With the custom mode, the reference does not hold the ERL
// and the frequency response of the reverb, which then depend on the
// rounding of the FFT.
func assertAecStateSpectra(t *testing.T, reference *referenceCursor, scenario *aecStateScenario, index int) {
	t.Helper()
	state := scenario.state
	last := scenario.numCapture - 1
	require.Equal(t, []float32{float32(index)}, reference.next("step"))
	assertAecStateSpectrum(t, reference, "erle", state.erle(false)[last][:])
	assertAecStateSpectrum(t, reference, "erle_onset_compensated", state.erle(true)[last][:])
	assertAecStateSpectrum(t, reference, "erle_unbounded", state.erleUnbounded()[last][:])
	if !state.useStationarityProperties() {
		assertAecStateSpectrum(t, reference, "erl", state.erl()[:])
		assertAecStateSpectrum(t, reference, "reverb_frequency_response", state.getReverbFrequencyResponse()[:])
	}
	var scaling [kFftLengthBy2Plus1]float32
	state.getResidualEchoScaling(scaling[:])
	require.Equal(t, reference.next("residual_echo_scaling"), scaling[:], "block %d", index)
	assertAecStateSpectrum(t, reference, "render_reverb", state.avgRenderReverb.getReverb()[:])
}

// TestAecState_MatchesUpstream compares the state with the upstream
// AecState at the pinned commit, bit for bit: the blocks where the flags and the
// delays change, the fullband ERLE, the time-domain ERL and the reverb
// decays every 5th block, and the ERLE, the ERL, the frequency response
// of the reverb, the residual echo scaling and the reverb of the render
// signal of some blocks.
func TestAecState_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, tc := range aecStateScenarioCases() {
		t.Run(tc.suffix()[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/aec_state_upstream.txt", tc.suffix())
			flagsReference := reference.filter("flags")
			scenario := newAecStateScenario(tc)
			state := scenario.state
			var flags flagChanges

			for index := range scenario.blocks {
				scenario.step(index)

				flags.add(index, aecStateFlags(state))
				if index%5 == 0 {
					want := reference.next("levels")
					got := []float32{
						state.fullBandErleLog2(), state.erlTimeDomain(), state.reverbDecay(false), state.reverbDecay(true),
					}
					require.Equal(t, float32(index), want[0])
					require.Equal(t, want[1:], got, "block %d", index)
				}
				if scenario.dumpsSpectra(index) {
					assertAecStateSpectra(t, reference, scenario, index)
				}
			}
			flags.assertMatches(t, flagsReference, "flags")
			require.Empty(t, flagsReference.entries)
			require.Empty(t, reference.entries)
		})
	}
}
