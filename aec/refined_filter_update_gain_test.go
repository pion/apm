// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// runRefinedFilterUpdateTest mirrors RunFilterUpdateTest in
// refined_filter_update_gain_unittest.cc, without the silent render
// option that no test uses.
func runRefinedFilterUpdateTest(
	numBlocksToProcess, delaySamples, filterLengthBlocks int,
	blocksWithSaturation []int,
) (e, y [kBlockSize]float32, gain fftData) {
	const numRenderChannels = 1
	const numCaptureChannels = 1
	const sampleRateHz = 48000
	const scale = 1.0 / float32(kFftLengthBy2)

	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = filterLengthBlocks
	config.filter.coarse.lengthBlocks = filterLengthBlocks
	refinedFilter := newAdaptiveFirFilter(config.filter.refined.lengthBlocks, config.filter.refined.lengthBlocks,
		config.filter.configChangeDurationBlocks, numRenderChannels)
	coarseFilter := newAdaptiveFirFilter(config.filter.coarse.lengthBlocks, config.filter.coarse.lengthBlocks,
		config.filter.configChangeDurationBlocks, numRenderChannels)
	h2 := [][][kFftLengthBy2Plus1]float32{
		make([][kFftLengthBy2Plus1]float32, refinedFilter.maxFilterSizePartitions()),
	}
	impulseResponses := [][]float32{make([]float32, getTimeDomainLength(refinedFilter.maxFilterSizePartitions()))}

	fft := newAec3Fft()
	coarseGain := newCoarseFilterUpdateGain(config.filter.coarse, config.filter.configChangeDurationBlocks)
	refinedGain := newRefinedFilterUpdateGain(config.filter.refined, config.filter.configChangeDurationBlocks)
	random := newTestRandom(42)
	render := newBlock(numBandsForRate(sampleRateHz), numRenderChannels)
	config.delay.defaultDelay = 1
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels)
	state := newAecState(config, numCaptureChannels)
	analyzer := newRenderSignalAnalyzer(config)
	var scratch [kFftLength]float32
	var echo, errCoarse fftData
	output := make([]subtractorOutput, numCaptureChannels)
	y2 := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	e2Refined := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	var renderPower, erl [kFftLengthBy2Plus1]float32

	delayBuffer := newTestDelayBuffer(delaySamples)
	for blockIndex := range numBlocksToProcess {
		saturation := slices.Contains(blocksWithSaturation, blockIndex)

		// Like upstream, the bands loop is bounded by the number of channels:
		// only the first band holds noise.
		for band := range numRenderChannels {
			for ch := range numRenderChannels {
				randomizeSampleVector(random, render.view(band, ch))
			}
		}
		delayBuffer.delay(render.view(0, 0), y[:])

		renderDelayBuffer.insert(render)
		if blockIndex == 0 {
			renderDelayBuffer.reset()
		}
		renderDelayBuffer.prepareCaptureProcessing()
		renderBuffer := renderDelayBuffer.getRenderBuffer()

		analyzer.update(renderBuffer, state.minDirectPathFilterDelay(), true)

		// Apply the refined filter.
		out := &output[0]
		refinedFilter.filter(renderBuffer, &echo)
		fft.inv(&echo, &scratch)
		for i := range out.eRefined {
			out.eRefined[i] = min(max(y[i]-scratch[i+kFftLengthBy2]*scale, -32768), 32767)
		}
		fft.zeroPaddedFft(&out.eRefined, windowRectangular, &out.ERefined)

		// Apply the coarse filter.
		coarseFilter.filter(renderBuffer, &echo)
		fft.inv(&echo, &scratch)
		for i := range out.eCoarse {
			out.eCoarse[i] = min(max(y[i]-scratch[i+kFftLengthBy2]*scale, -32768), 32767)
		}
		fft.zeroPaddedFft(&out.eCoarse, windowRectangular, &errCoarse)

		out.ERefined.spectrum(&out.E2Refined)
		errCoarse.spectrum(&out.E2Coarse)

		// Adapt the coarse filter.
		renderBuffer.spectralSum(coarseFilter.sizePartitions(), &renderPower)
		coarseGain.compute(&renderPower, analyzer, &errCoarse, coarseFilter.sizePartitions(), saturation, &gain)
		coarseFilter.adapt(renderBuffer, &gain)

		// Adapt the refined filter.
		renderBuffer.spectralSum(refinedFilter.sizePartitions(), &renderPower)
		computeErl(h2[0], &erl)
		refinedGain.compute(&renderPower, analyzer, out, &erl, refinedFilter.sizePartitions(), saturation, false, &gain)
		refinedFilter.adaptUpdateImpulse(renderBuffer, &gain, &impulseResponses[0])

		// Update the delay.
		state.handleEchoPathChange(echoPathVariability{})
		h2[0] = refinedFilter.computeFrequencyResponse(h2[0])
		e2Refined[0] = out.E2Refined
		state.update(delayEstimate{}, false, h2, impulseResponses, renderBuffer, e2Refined, y2, output)
	}

	return output[0].eRefined, y, gain
}

// Port of GainCausesFilterToConverge.
func TestRefinedFilterUpdateGain_GainCausesFilterToConverge(t *testing.T) {
	for _, filterLengthBlocks := range []int{12, 20, 30} {
		for _, delaySamples := range []int{0, 64, 150, 200, 301} {
			t.Run(fmt.Sprintf("length %d delay %d", filterLengthBlocks, delaySamples), func(t *testing.T) {
				t.Parallel()
				e, y, _ := runRefinedFilterUpdateTest(600, delaySamples, filterLengthBlocks, nil)

				// Like upstream, use different criteria to take overmodelling
				// into account.
				if filterLengthBlocks == 12 {
					assert.Less(t, 1000*energy(e[:]), energy(y[:]))
				} else {
					assert.Less(t, energy(e[:]), energy(y[:]))
				}
			})
		}
	}
}

// Port of DecreasingGain.
func TestRefinedFilterUpdateGain_DecreasingGain(t *testing.T) {
	_, _, gainA := runRefinedFilterUpdateTest(250, 65, 12, nil)
	_, _, gainB := runRefinedFilterUpdateTest(500, 65, 12, nil)
	_, _, gainC := runRefinedFilterUpdateTest(750, 65, 12, nil)

	assert.Greater(t, powerSum(&gainA), powerSum(&gainB))
	assert.Greater(t, powerSum(&gainB), powerSum(&gainC))
}

// Port of SaturationBehavior.
func TestRefinedFilterUpdateGain_SaturationBehavior(t *testing.T) {
	var blocksWithSaturation []int
	for k := 99; k < 200; k++ {
		blocksWithSaturation = append(blocksWithSaturation, k)
	}
	for _, filterLengthBlocks := range []int{12, 20, 30} {
		name := fmt.Sprintf("length %d", filterLengthBlocks)
		_, _, gainA := runRefinedFilterUpdateTest(100, 65, filterLengthBlocks, blocksWithSaturation)
		assert.Equal(t, fftData{}, gainA, name)

		_, _, gainA = runRefinedFilterUpdateTest(99, 65, filterLengthBlocks, blocksWithSaturation)
		_, _, gainB := runRefinedFilterUpdateTest(201, 65, filterLengthBlocks, blocksWithSaturation)
		assert.Less(t, powerSum(&gainA), powerSum(&gainB), name)
	}
}
