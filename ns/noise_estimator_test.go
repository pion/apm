// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ns

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func constantSpectrum(v float32) *[fftSizeBy2Plus1]float32 {
	var s [fftSizeBy2Plus1]float32
	for i := range s {
		s[i] = v //nolint:gosec // G602 false positive on a range loop.
	}

	return &s
}

func TestQuantileNoiseEstimatorConverges(t *testing.T) {
	q := newQuantileNoiseEstimator()
	var noise [fftSizeBy2Plus1]float32
	for range 1000 {
		q.estimate(constantSpectrum(1000), &noise)
	}
	for _, v := range noise {
		assert.InEpsilon(t, 1000, v, 0.01)
	}
}

func TestNoiseEstimatorPostUpdate(t *testing.T) {
	params := newSuppressionParams(DefaultConfig().TargetLevel)
	signal := constantSpectrum(500)
	noProb, fullProb := constantSpectrum(0), constantSpectrum(1)

	est := newNoiseEstimator(&params)
	for range 200 {
		est.prepareAnalysis()
		est.postUpdate(noProb, signal)
	}
	assert.InDeltaSlice(t, signal[:], est.noiseSpec[:], 0.01, "noise should follow the signal when there is no speech")

	before := est.noiseSpec
	est.prepareAnalysis()
	est.postUpdate(fullProb, constantSpectrum(5000))
	assert.InDeltaSlice(t, before[:], est.noiseSpec[:], 1e-3, "noise should hold during speech")
}

func TestNoiseEstimatorStartup(t *testing.T) {
	params := newSuppressionParams(Level6dB)
	est := newNoiseEstimator(&params)
	signal := constantSpectrum(200)
	for frame := range int32(shortStartupPhaseBlocks) {
		est.prepareAnalysis()
		est.preUpdate(frame, signal, 200*fftSizeBy2Plus1)
	}
	for _, v := range est.noiseSpec {
		assert.Positive(t, v)
		assert.Less(t, v, float32(1000))
	}
}
