// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// produceSinusoidInNoise ports ProduceSinusoidInNoise.
func produceSinusoidInNoise(
	sampleRateHz, sinusoidChannel int, sinusoidalFrequencyHz float32, random *testRandom, sampleCounter *int, x *block,
) {
	// Fill x with low-amplitude noise.
	randomizeSampleVectorAmplitude(random, x.data, 500)

	// Produce a sinusoid of the specified frequency in the specified
	// channel.
	channel := x.view(0, sinusoidChannel)
	for j := range channel {
		phase := 2 * math.Pi * sinusoidalFrequencyHz * float32(*sampleCounter+j) / float32(sampleRateHz)
		channel[j] += 32000 * float32(math.Sin(float64(phase)))
	}
	*sampleCounter += kBlockSize
}

// Port of RunNarrowBandDetectionTest.
func runNarrowBandDetectionTest(t *testing.T, numChannels int) {
	t.Helper()
	const (
		sampleRateHz      = 48000
		sinusFrequencyBin = 32
	)
	config := defaultEchoCanceller3Config()
	analyzer := newRenderSignalAnalyzer(config)
	random := newTestRandom(42)
	render := newBlock(numBandsForRate(sampleRateHz), numChannels)
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numChannels)

	generateSinusoidTest := func(knownDelay bool) {
		sampleCounter := 0
		for k := range 100 {
			produceSinusoidInNoise(16000, numChannels-1, 16000/2*sinusFrequencyBin/kFftLengthBy2,
				random, &sampleCounter, render)
			renderDelayBuffer.insert(render)
			if k == 0 {
				renderDelayBuffer.reset()
			}
			renderDelayBuffer.prepareCaptureProcessing()
			analyzer.update(renderDelayBuffer.getRenderBuffer(), 0, knownDelay)
		}
	}

	var mask, ones [kFftLengthBy2Plus1]float32
	fill(ones[:], 1)

	generateSinusoidTest(true)
	mask = ones
	analyzer.maskRegionsAroundNarrowBands(&mask)
	for k, value := range mask {
		want := float32(1)
		if k >= sinusFrequencyBin-2 && k <= sinusFrequencyBin+2 {
			want = 0
		}
		assert.InDelta(t, want, value, 0, "bin %d", k)
	}
	assert.True(t, analyzer.poorSignalExcitation())
	band, ok := analyzer.getNarrowPeakBand()
	require.True(t, ok)
	assert.Equal(t, 32, band)

	// Verify that no bands are detected as narrow when the delay is
	// unknown.
	generateSinusoidTest(false)
	mask = ones
	analyzer.maskRegionsAroundNarrowBands(&mask)
	assert.Equal(t, ones, mask)
	assert.False(t, analyzer.poorSignalExcitation())
}

// Port of RenderSignalAnalyzer.NoFalseDetectionOfNarrowBands: no narrow
// bands are detected in a noise signal.
func TestRenderSignalAnalyzer_NoFalseDetectionOfNarrowBands(t *testing.T) {
	for _, numChannels := range []int{1, 2, 8} {
		config := defaultEchoCanceller3Config()
		analyzer := newRenderSignalAnalyzer(config)
		random := newTestRandom(42)
		render := newBlock(3, numChannels)
		renderDelayBuffer := newRenderDelayBufferImpl(config, 48000, numChannels)

		for k := range 100 {
			randomizeSampleVector(random, render.data)
			renderDelayBuffer.insert(render)
			if k == 0 {
				renderDelayBuffer.reset()
			}
			renderDelayBuffer.prepareCaptureProcessing()
			analyzer.update(renderDelayBuffer.getRenderBuffer(), 0, true)
		}

		var mask, ones [kFftLengthBy2Plus1]float32
		fill(ones[:], 1)
		mask = ones
		analyzer.maskRegionsAroundNarrowBands(&mask)
		assert.Equal(t, ones, mask, "channels %d", numChannels)
		assert.False(t, analyzer.poorSignalExcitation())
		_, ok := analyzer.getNarrowPeakBand()
		assert.False(t, ok)
	}
}

// Port of RenderSignalAnalyzer.NarrowBandDetection: a sinusoid signal is
// detected as narrow bands.
func TestRenderSignalAnalyzer_NarrowBandDetection(t *testing.T) {
	for _, numChannels := range []int{1, 2, 8} {
		runNarrowBandDetectionTest(t, numChannels)
	}
}
