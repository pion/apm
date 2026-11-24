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

// runCoarseFilterUpdateTest mirrors RunFilterUpdateTest in
// coarse_filter_update_gain_unittest.cc. Like upstream, filterLengthBlocks
// sets the length of the refined filter, which the test does not use: the
// coarse filter keeps its default length.
func runCoarseFilterUpdateTest(
	numBlocksToProcess, delaySamples, numRenderChannels, filterLengthBlocks int,
	blocksWithSaturation []int,
) (e, y [kBlockSize]float32, gain fftData) {
	const sampleRateHz = 48000
	const scale = 1.0 / float32(kFftLengthBy2)
	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = filterLengthBlocks
	coarseFilter := newAdaptiveFirFilter(config.filter.coarse.lengthBlocks, config.filter.coarse.lengthBlocks,
		config.filter.configChangeDurationBlocks, numRenderChannels)
	fft := newAec3Fft()
	config.delay.defaultDelay = 1
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels)
	coarseGain := newCoarseFilterUpdateGain(config.filter.coarse, config.filter.configChangeDurationBlocks)
	random := newTestRandom(42)
	render := newBlock(numBandsForRate(sampleRateHz), numRenderChannels)
	analyzer := newRenderSignalAnalyzer(config)
	var scratch [kFftLength]float32
	var echo, errCoarse fftData
	var renderPower [kFftLengthBy2Plus1]float32
	delayBuffer := newTestDelayBuffer(delaySamples)

	for k := range numBlocksToProcess {
		saturation := slices.Contains(blocksWithSaturation, k)

		for band := range numBandsForRate(sampleRateHz) {
			for ch := range numRenderChannels {
				randomizeSampleVector(random, render.view(band, ch))
			}
		}
		delayBuffer.delay(render.view(0, 0), y[:])

		renderDelayBuffer.insert(render)
		if k == 0 {
			renderDelayBuffer.reset()
		}
		renderDelayBuffer.prepareCaptureProcessing()
		renderBuffer := renderDelayBuffer.getRenderBuffer()

		analyzer.update(renderBuffer, delaySamples/kBlockSize, true)

		coarseFilter.filter(renderBuffer, &echo)
		fft.inv(&echo, &scratch)
		for i := range e {
			e[i] = min(max(y[i]-scratch[i+kFftLengthBy2]*scale, -32768), 32767)
		}
		fft.zeroPaddedFft(&e, windowRectangular, &errCoarse)

		renderBuffer.spectralSum(coarseFilter.sizePartitions(), &renderPower)
		coarseGain.compute(&renderPower, analyzer, &errCoarse, coarseFilter.sizePartitions(), saturation, &gain)
		coarseFilter.adapt(renderBuffer, &gain)
	}

	return e, y, gain
}

// powerSum mirrors the accumulation of the gain spectrum into a double.
func powerSum(gain *fftData) float64 {
	var power [kFftLengthBy2Plus1]float32
	gain.spectrum(&power)
	var sum float64
	for _, p := range power {
		sum += float64(p)
	}

	return sum
}

// Port of GainCausesFilterToConverge.
func TestCoarseFilterUpdateGain_GainCausesFilterToConverge(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 8} {
		for _, filterLengthBlocks := range []int{12, 20, 30} {
			for _, delaySamples := range []int{0, 64, 150, 200, 301} {
				name := fmt.Sprintf("render %d length %d delay %d", numRenderChannels, filterLengthBlocks, delaySamples)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					e, y, _ := runCoarseFilterUpdateTest(5000, delaySamples, numRenderChannels, filterLengthBlocks, nil)

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
}

// Port of SaturationBehavior.
func TestCoarseFilterUpdateGain_SaturationBehavior(t *testing.T) {
	var blocksWithSaturation []int
	for k := 99; k < 200; k++ {
		blocksWithSaturation = append(blocksWithSaturation, k)
	}
	for _, numRenderChannels := range []int{1, 2, 8} {
		for _, filterLengthBlocks := range []int{12, 20, 30} {
			_, _, gain := runCoarseFilterUpdateTest(100, 65, numRenderChannels, filterLengthBlocks, blocksWithSaturation)
			assert.Equal(t, fftData{}, gain, "render %d length %d", numRenderChannels, filterLengthBlocks)
		}
	}
}

// Port of DecreasingGain.
func TestCoarseFilterUpdateGain_DecreasingGain(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 4} {
		for _, filterLengthBlocks := range []int{12, 20, 30} {
			_, _, gainA := runCoarseFilterUpdateTest(100, 65, numRenderChannels, filterLengthBlocks, nil)
			_, _, gainB := runCoarseFilterUpdateTest(200, 65, numRenderChannels, filterLengthBlocks, nil)
			_, _, gainC := runCoarseFilterUpdateTest(300, 65, numRenderChannels, filterLengthBlocks, nil)

			name := fmt.Sprintf("render %d length %d", numRenderChannels, filterLengthBlocks)
			assert.Greater(t, powerSum(&gainA), powerSum(&gainB), name)
			assert.Greater(t, powerSum(&gainB), powerSum(&gainC), name)
		}
	}
}
