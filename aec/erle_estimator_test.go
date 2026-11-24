// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	erleTestLowFrequencyLimit = kFftLengthBy2 / 2
	erleTestTrueErle          = 10.0
	erleTestTrueErleOnsets    = 1.0
	erleTestEchoPathGain      = 3.0
)

func verifyErleBands(t *testing.T, erle [][kFftLengthBy2Plus1]float32, referenceLf, referenceHf float32) {
	t.Helper()
	for ch := range erle {
		for _, value := range erle[ch][:erleTestLowFrequencyLimit] {
			assert.InDelta(t, referenceLf, value, 0.001)
		}
		for _, value := range erle[ch][erleTestLowFrequencyLimit:] {
			assert.InDelta(t, referenceHf, value, 0.001)
		}
	}
}

func verifyErle(t *testing.T, estimator *erleEstimator, referenceLf, referenceHf float32) {
	t.Helper()
	verifyErleBands(t, estimator.getErle(true), referenceLf, referenceHf)
	assert.InDelta(t, erleTestTrueErle, math.Pow(2, float64(estimator.fullbandErleLog2())), 0.5)
}

func verifyErleGreaterOrEqual(t *testing.T, erle1, erle2 [][kFftLengthBy2Plus1]float32) {
	t.Helper()
	for ch := range erle1 {
		for i := range erle1[ch] {
			assert.GreaterOrEqual(t, erle1[ch][i], erle2[ch][i])
		}
	}
}

// erleEstimatorTest holds the state of the upstream ErleEstimator tests.
type erleEstimatorTest struct {
	config                  echoCanceller3Config
	x2                      [kFftLengthBy2Plus1]float32
	e2, y2                  [][kFftLengthBy2Plus1]float32
	convergedFilters        []bool
	render                  *block
	filterFrequencyResponse [][][kFftLengthBy2Plus1]float32
	renderDelayBuffer       *renderDelayBufferImpl
	estimator               *erleEstimator
}

func newErleEstimatorTest(numRenderChannels, numCaptureChannels int) *erleEstimatorTest {
	const sampleRateHz = 48000
	config := defaultEchoCanceller3Config()
	config.erle.onsetDetection = true
	test := &erleEstimatorTest{
		config:           config,
		e2:               make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		y2:               make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		convergedFilters: make([]bool, numCaptureChannels),
		render:           newBlock(numBandsForRate(sampleRateHz), numRenderChannels),
		// Upstream indexes the response by block then by channel here.
		filterFrequencyResponse: make([][][kFftLengthBy2Plus1]float32, config.filter.refined.lengthBlocks),
		renderDelayBuffer:       newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels),
		estimator:               newErleEstimator(0, config, numCaptureChannels),
	}
	for ch := range test.convergedFilters {
		test.convergedFilters[ch] = true
	}

	// GetFilterFreq.
	for blockIndex := range test.filterFrequencyResponse {
		test.filterFrequencyResponse[blockIndex] = make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	}
	delayHeadroomBlocks := config.delay.delayHeadroomSamples / kBlockSize
	for ch := range numCaptureChannels {
		fill(test.filterFrequencyResponse[delayHeadroomBlocks][ch][:], erleTestEchoPathGain)
	}

	formFarendTimeFrame(test.render)
	test.insertRender()

	return test
}

func (e *erleEstimatorTest) insertRender() {
	e.renderDelayBuffer.insert(e.render)
	e.renderDelayBuffer.prepareCaptureProcessing()
}

// formFarendFrame ports FormFarendFrame.
func (e *erleEstimatorTest) formFarendFrame(erle float32) {
	spectrumBuffer := e.renderDelayBuffer.getRenderBuffer().getSpectrumBuffer()
	numRenderChannels := len(spectrumBuffer.buffer[0])

	e.x2 = [kFftLengthBy2Plus1]float32{}
	for ch := range numRenderChannels {
		for k := range e.x2 {
			e.x2[k] += spectrumBuffer.buffer[spectrumBuffer.write][ch][k] / float32(numRenderChannels)
		}
	}

	for ch := range e.y2 {
		for k := range e.x2 {
			e.y2[ch][k] = e.x2[k] * erleTestEchoPathGain * erleTestEchoPathGain
			e.e2[ch][k] = e.y2[ch][k] / erle
		}
	}
}

// formNearendFrame ports FormNearendFrame.
func (e *erleEstimatorTest) formNearendFrame() {
	for band := range e.render.numBands {
		for ch := range e.render.numChannels {
			fill(e.render.view(band, ch), 0)
		}
	}

	e.x2 = [kFftLengthBy2Plus1]float32{}
	for ch := range e.y2 {
		fill(e.y2[ch][:], 500*1000*1000)
		fill(e.e2[ch][:], e.y2[ch][0])
	}
}

func (e *erleEstimatorTest) update() {
	e.estimator.update(e.renderDelayBuffer.getRenderBuffer(), e.filterFrequencyResponse, &e.x2, e.y2, e.e2,
		e.convergedFilters)
}

func (e *erleEstimatorTest) insertAndUpdate(times int) {
	for range times {
		e.insertRender()
		e.update()
	}
}

func forEachErleEstimatorChannelCount(t *testing.T, test func(t *testing.T, numRenderChannels, numCaptureChannels int)) {
	t.Helper()
	for _, numRenderChannels := range []int{1, 2, 4, 8} {
		for _, numCaptureChannels := range []int{1, 2, 8} {
			t.Run(fmt.Sprintf("%d_%d", numRenderChannels, numCaptureChannels), func(t *testing.T) {
				t.Parallel()
				test(t, numRenderChannels, numCaptureChannels)
			})
		}
	}
}

// Port of ErleEstimatorMultiChannel.VerifyErleIncreaseAndHold.
func TestErleEstimator_VerifyErleIncreaseAndHold(t *testing.T) {
	forEachErleEstimatorChannelCount(t, func(t *testing.T, numRenderChannels, numCaptureChannels int) {
		t.Helper()
		test := newErleEstimatorTest(numRenderChannels, numCaptureChannels)
		estimator := test.estimator
		verify := func() {
			verifyErle(t, estimator, test.config.erle.maxL, test.config.erle.maxH)
			verifyErleGreaterOrEqual(t, estimator.getErle(false), estimator.getErle(true))
			verifyErleGreaterOrEqual(t, estimator.getErleUnbounded(), estimator.getErle(false))
		}

		// Verifies that the ERLE estimate is properly increased to higher
		// values.
		test.formFarendFrame(erleTestTrueErle)
		test.insertAndUpdate(1000)
		verify()

		// Verifies that the ERLE is not immediately decreased during
		// nearend activity.
		test.formNearendFrame()
		test.insertAndUpdate(50)
		verify()
	})
}

// Port of ErleEstimatorMultiChannel.VerifyErleTrackingOnOnsets.
func TestErleEstimator_VerifyErleTrackingOnOnsets(t *testing.T) {
	forEachErleEstimatorChannelCount(t, func(t *testing.T, numRenderChannels, numCaptureChannels int) {
		t.Helper()
		test := newErleEstimatorTest(numRenderChannels, numCaptureChannels)
		minErle := test.config.erle.min

		for range 20 {
			test.formFarendFrame(erleTestTrueErleOnsets)
			test.insertAndUpdate(10)
			test.formFarendFrame(erleTestTrueErle)
			test.insertAndUpdate(1000)
			test.formNearendFrame()
			test.insertAndUpdate(300)
		}
		verifyErleBands(t, test.estimator.getErleDuringOnsets(), minErle, minErle)
		test.formNearendFrame()
		for range 1000 {
			test.update()
		}
		// Verifies that during nearend activity, the ERLE converges to the
		// ERLE for onsets.
		verifyErle(t, test.estimator, minErle, minErle)
	})
}

// TestErleEstimator_MatchesUpstream compares the ERLE estimates with the
// upstream ErleEstimator at the pinned commit, for an estimator with a
// startup phase of 100 blocks and one with 4 filter sections. The first
// is reset at block 850 without a delay change, the second with one.
func TestErleEstimator_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, tc := range erleScenarioCases() {
		suffix := erleScenarioSuffix(tc.numRender, tc.numCapture)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/erle_estimator_upstream.txt", suffix)
			scenario := newErleScenario(tc.numRender, tc.numCapture)
			config := defaultEchoCanceller3Config()
			estimator := newErleEstimator(100, config, tc.numCapture)
			config.erle.numSections = 4
			sections := newErleEstimator(0, config, tc.numCapture)

			for index := range erleScenarioBlocks {
				scenario.step(index)
				for _, erle := range []*erleEstimator{estimator, sections} {
					erle.update(scenario.renderBuffer(), scenario.h2, &scenario.x2, scenario.y2, scenario.e2,
						scenario.converged)
				}

				// The block index, the fullband ERLE of both estimators,
				// then the quality of every filter.
				got := []float32{float32(index), estimator.fullbandErleLog2(), sections.fullbandErleLog2()}
				for _, quality := range estimator.getInstLinearQualityEstimates() {
					got = append(got, optionalValue(quality))
				}
				require.InDeltaSlice(t, reference.next("erle_fullband"), got, erleReferenceDelta, "block %d", index)

				if erleScenarioDumpsSpectra(index) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					assertErleSpectra(t, reference, "erle", estimator.getErle(false), index)
					assertErleSpectra(t, reference, "erle_onset_compensated", estimator.getErle(true), index)
					assertErleSpectra(t, reference, "erle_unbounded", estimator.getErleUnbounded(), index)
					assertErleSpectra(t, reference, "erle_during_onsets", estimator.getErleDuringOnsets(), index)
					assertErleSpectra(t, reference, "erle_sections", sections.getErle(false), index)
					assertErleSpectra(t, reference, "erle_sections_onset_compensated", sections.getErle(true), index)
					assertErleSpectra(t, reference, "erle_sections_unbounded", sections.getErleUnbounded(), index)
				}
				if index == erleScenarioResetBlock {
					estimator.reset(false)
					sections.reset(true)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
