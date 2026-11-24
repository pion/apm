// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// erleTestFarendFrame is the active render frame of the upstream ERLE
// tests.
func erleTestFarendFrame() [kBlockSize]float32 {
	return [kBlockSize]float32{
		7459.88, 17209.6, 17383, 20768.9, 16816.7, 18386.3, 4492.83, 9675.85,
		6665.52, 14808.6, 9342.3, 7483.28, 19261.7, 4145.98, 1622.18, 13475.2,
		7166.32, 6856.61, 21937, 7263.14, 9569.07, 14919, 8413.32, 7551.89,
		7848.65, 6011.27, 13080.6, 15865.2, 12656, 17459.6, 4263.93, 4503.03,
		9311.79, 21095.8, 12657.9, 13906.6, 19267.2, 11338.1, 16828.9, 11501.6,
		11405, 15031.4, 14541.6, 19765.5, 18346.3, 19350.2, 3157.47, 18095.8,
		1743.68, 21328.2, 19727.5, 7295.16, 10332.4, 11055.5, 20107.4, 14708.4,
		12416.2, 16434, 2454.69, 9840.8, 6867.23, 1615.75, 6059.9, 8394.19,
	}
}

// formFarendTimeFrame ports FormFarendTimeFrame and GetActiveFrame.
func formFarendTimeFrame(x *block) {
	frame := erleTestFarendFrame()
	for band := range x.numBands {
		for channel := range x.numChannels {
			copy(x.view(band, channel), frame[:])
		}
	}
}

// signalDependentErleTestInputs ports TestInputs.
type signalDependentErleTestInputs struct {
	updates           int
	renderDelayBuffer *renderDelayBufferImpl
	x2                [kFftLengthBy2Plus1]float32
	y2, e2            [][kFftLengthBy2Plus1]float32
	h2                [][][kFftLengthBy2Plus1]float32
	render            *block
	convergedFilters  []bool
}

func newSignalDependentErleTestInputs(
	config echoCanceller3Config, numRenderChannels, numCaptureChannels int,
) *signalDependentErleTestInputs {
	inputs := &signalDependentErleTestInputs{
		renderDelayBuffer: newRenderDelayBufferImpl(config, 16000, numRenderChannels),
		y2:                make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		e2:                make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		h2:                make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		render:            newBlock(1, numRenderChannels),
		convergedFilters:  make([]bool, numCaptureChannels),
	}
	inputs.renderDelayBuffer.alignFromDelay(4)
	for ch := range inputs.h2 {
		inputs.h2[ch] = make([][kFftLengthBy2Plus1]float32, config.filter.refined.lengthBlocks)
		inputs.convergedFilters[ch] = true
	}
	for partition := range inputs.h2[0] {
		fill(inputs.h2[0][partition][:], 1)
	}

	return inputs
}

func (i *signalDependentErleTestInputs) update() {
	if i.updates%2 == 0 {
		fill(i.render.view(0, 0), 0)
	} else {
		formFarendTimeFrame(i.render)
	}

	i.renderDelayBuffer.insert(i.render)
	i.renderDelayBuffer.prepareCaptureProcessing()
	i.updateCurrentPowerSpectra()
	i.updates++
}

func (i *signalDependentErleTestInputs) updateCurrentPowerSpectra() {
	render := i.renderDelayBuffer.getRenderBuffer()
	spectrumRenderBuffer := render.getSpectrumBuffer()
	idx := render.position()
	prevIdx := spectrumRenderBuffer.offsetIndex(idx, 1)
	x2 := &spectrumRenderBuffer.buffer[idx][0]
	x2Prev := &spectrumRenderBuffer.buffer[prevIdx][0]
	i.x2 = *x2
	for ch := range i.y2 {
		for k := range x2 {
			i.e2[ch][k] = 0.01 * x2Prev[k]
			i.y2[ch][k] = x2[k] + i.e2[ch][k]
		}
	}
}

func (i *signalDependentErleTestInputs) run(
	estimator *signalDependentErleEstimator, averageErle [][kFftLengthBy2Plus1]float32, updates int,
) {
	for range updates {
		i.update()
		estimator.update(i.renderDelayBuffer.getRenderBuffer(), i.h2, &i.x2, i.y2, i.e2, averageErle, averageErle,
			i.convergedFilters)
	}
}

func signalDependentErleAverageErle(config echoCanceller3Config, numCaptureChannels int) [][kFftLengthBy2Plus1]float32 {
	averageErle := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	for ch := range averageErle {
		fill(averageErle[ch][:], config.erle.maxL)
	}

	return averageErle
}

// Port of SignalDependentErleEstimatorMultiChannel.SweepSettings. The
// settings that upstream skips are those that
// EchoCanceller3Config::Validate rejects: more sections than blocks.
func TestSignalDependentErleEstimator_SweepSettings(t *testing.T) {
	const maxLengthBlocks = 50
	for _, numRenderChannels := range []int{1, 2, 4} {
		for _, numCaptureChannels := range []int{1, 2, 4} {
			config := defaultEchoCanceller3Config()
			for blocks := 1; blocks < maxLengthBlocks; blocks += 10 {
				for delayHeadroom := range 5 {
					for numSections := 2; numSections <= blocks; numSections++ {
						config.filter.refined.lengthBlocks = blocks
						config.filter.refinedInitial.lengthBlocks = min(config.filter.refinedInitial.lengthBlocks, blocks)
						config.delay.delayHeadroomSamples = delayHeadroom * kBlockSize
						config.erle.numSections = numSections
						estimator := newSignalDependentErleEstimator(config, numCaptureChannels)
						averageErle := signalDependentErleAverageErle(config, numCaptureChannels)
						inputs := newSignalDependentErleTestInputs(config, numRenderChannels, numCaptureChannels)
						assert.NotPanics(t, func() { inputs.run(estimator, averageErle, 10) })
					}
				}
			}
		}
	}
}

// Port of SignalDependentErleEstimatorMultiChannel.LongerRun.
func TestSignalDependentErleEstimator_LongerRun(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 4} {
		for _, numCaptureChannels := range []int{1, 2, 4} {
			config := defaultEchoCanceller3Config()
			config.filter.refined.lengthBlocks = 2
			config.filter.refinedInitial.lengthBlocks = 1
			config.delay.delayHeadroomSamples = 0
			config.delay.hysteresisLimitBlocks = 0
			config.erle.numSections = 2
			averageErle := signalDependentErleAverageErle(config, numCaptureChannels)
			estimator := newSignalDependentErleEstimator(config, numCaptureChannels)
			inputs := newSignalDependentErleTestInputs(config, numRenderChannels, numCaptureChannels)
			assert.NotPanics(t, func() { inputs.run(estimator, averageErle, 200) })
		}
	}
}

// TestSignalDependentErleEstimator_MatchesUpstream compares the ERLE
// estimates with the upstream SignalDependentErleEstimator at the pinned
// commit, with 4 filter sections. The average ERLE comes from the subband
// estimator. The estimator is reset at block 850.
func TestSignalDependentErleEstimator_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, tc := range erleScenarioCases() {
		suffix := erleScenarioSuffix(tc.numRender, tc.numCapture)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/signal_dependent_erle_estimator_upstream.txt", suffix)
			scenario := newErleScenario(tc.numRender, tc.numCapture)
			config := defaultEchoCanceller3Config()
			subband := newSubbandErleEstimator(config, tc.numCapture)
			config.erle.numSections = 4
			estimator := newSignalDependentErleEstimator(config, tc.numCapture)

			for index := range erleScenarioBlocks {
				scenario.step(index)
				subband.update(&scenario.x2, scenario.y2, scenario.e2, scenario.converged)
				estimator.update(scenario.renderBuffer(), scenario.h2, &scenario.x2, scenario.y2, scenario.e2,
					subband.getErle(false), subband.getErle(true), scenario.converged)

				if erleScenarioDumpsSpectra(index) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					assertErleSpectra(t, reference, "signal_dependent", estimator.getErle(false), index)
					assertErleSpectra(t, reference, "signal_dependent_onset_compensated", estimator.getErle(true), index)
				}
				if index == erleScenarioResetBlock {
					subband.reset()
					estimator.reset()
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}

// TestSignalDependentErleEstimator_LongFilterMatchesUpstream compares
// the ERLE estimates with the upstream SignalDependentErleEstimator for
// a filter of 40 blocks split in 6 sections, with a delay headroom of one
// block.
func TestSignalDependentErleEstimator_LongFilterMatchesUpstream(t *testing.T) {
	reference := newReferenceCursor(t, "testdata/signal_dependent_erle_estimator_upstream.txt", " 2 2 long")
	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = 40
	config.delay.delayHeadroomSamples = 64
	scenario := newErleScenarioWithConfig(config, 2, 2)
	subband := newSubbandErleEstimator(config, 2)
	config.erle.numSections = 6
	estimator := newSignalDependentErleEstimator(config, 2)

	for index := range erleScenarioBlocks {
		scenario.step(index)
		subband.update(&scenario.x2, scenario.y2, scenario.e2, scenario.converged)
		estimator.update(scenario.renderBuffer(), scenario.h2, &scenario.x2, scenario.y2, scenario.e2,
			subband.getErle(false), subband.getErle(true), scenario.converged)

		if index%50 == 49 {
			require.Equal(t, []float32{float32(index)}, reference.next("step"))
			assertErleSpectra(t, reference, "signal_dependent", estimator.getErle(false), index)
			assertErleSpectra(t, reference, "signal_dependent_onset_compensated", estimator.getErle(true), index)
		}
		if index == erleScenarioResetBlock {
			subband.reset()
			estimator.reset()
		}
	}
	require.Empty(t, reference.entries)
}
