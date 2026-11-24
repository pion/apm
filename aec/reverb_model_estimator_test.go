// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	reverbModelTestFilterDelayBlocks = 2
	reverbModelTestTruePowerDecay    = 0.5
)

// reverbModelEstimatorTest ports ReverbModelEstimatorTest.
type reverbModelEstimatorTest struct {
	config             echoCanceller3Config
	estimatedDecay     float32
	mildEstimatedDecay float32
	estimatedPowerTail float32
	truePowerTail      float32
	h                  [][]float32
	h2                 [][][kFftLengthBy2Plus1]float32
	qualityLinear      []optionalFloat32
}

func newReverbModelEstimatorTest(defaultDecay float32, numCaptureChannels int) *reverbModelEstimatorTest {
	config := defaultEchoCanceller3Config()
	config.epStrength.defaultLen = defaultDecay
	config.filter.refined.lengthBlocks = 40
	test := &reverbModelEstimatorTest{
		config:         config,
		estimatedDecay: defaultDecay,
		h:              make([][]float32, numCaptureChannels),
		h2:             make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		qualityLinear:  make([]optionalFloat32, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		test.h[ch] = make([]float32, config.filter.refined.lengthBlocks*kBlockSize)
		test.h2[ch] = make([][kFftLengthBy2Plus1]float32, config.filter.refined.lengthBlocks)
		test.qualityLinear[ch] = optionalFloat32{value: 1, valid: true}
	}
	test.createImpulseResponseWithDecay()

	return test
}

func (r *reverbModelEstimatorTest) createImpulseResponseWithDecay() {
	fft := newAec3Fft()
	decaySample := float32(math.Sqrt(math.Pow(reverbModelTestTruePowerDecay, 1.0/kBlockSize)))
	const filterDelayCoefficients = reverbModelTestFilterDelayBlocks * kBlockSize
	for _, response := range r.h {
		response[filterDelayCoefficients] = 1
		for k := filterDelayCoefficients + 1; k < len(response); k++ {
			response[k] = response[k-1] * decaySample
		}
	}

	for ch := range r.h2 {
		for partition := range r.h2[ch] {
			var fftInput [kFftLength]float32
			copy(fftInput[:], r.h[ch][partition*kBlockSize:(partition+1)*kBlockSize])
			var spectrum fftData
			fft.fwd(&fftInput, &spectrum)
			spectrum.spectrum(&r.h2[ch][partition])
		}
	}
	r.truePowerTail = sum(r.h2[0][len(r.h2[0])-1][:])
}

func (r *reverbModelEstimatorTest) runEstimator() {
	numCaptureChannels := len(r.h2)
	estimator := newReverbModelEstimator(r.config, numCaptureChannels)
	usableLinearEstimates := make([]bool, numCaptureChannels)
	filterDelayBlocks := make([]int, numCaptureChannels)
	for ch := range numCaptureChannels {
		usableLinearEstimates[ch] = true
		filterDelayBlocks[ch] = reverbModelTestFilterDelayBlocks
	}
	for range 3000 {
		estimator.update(r.h, r.h2, r.qualityLinear, filterDelayBlocks, usableLinearEstimates, false)
	}
	r.estimatedDecay = estimator.reverbDecay(false)
	r.mildEstimatedDecay = estimator.reverbDecay(true)
	r.estimatedPowerTail = sum(estimator.getReverbFrequencyResponse()[:])
}

func (r *reverbModelEstimatorTest) powerTailDB() float64 {
	return 10 * math.Log10(float64(r.estimatedPowerTail))
}

func (r *reverbModelEstimatorTest) truePowerTailDB() float64 {
	return 10 * math.Log10(float64(r.truePowerTail))
}

// Port of ReverbModelEstimatorTests.NotChangingDecay.
func TestReverbModelEstimator_NotChangingDecay(t *testing.T) {
	const defaultDecay = 0.9
	for _, numCaptureChannels := range []int{1, 2, 4, 8} {
		test := newReverbModelEstimatorTest(defaultDecay, numCaptureChannels)
		test.runEstimator()
		assert.Equal(t, float32(defaultDecay), test.estimatedDecay)
		assert.Equal(t, defaultEpStrengthConfig().nearendLen, test.mildEstimatedDecay)
		assert.InDelta(t, test.truePowerTailDB(), test.powerTailDB(), 5)
	}
}

// Port of ReverbModelEstimatorTests.ChangingDecay.
func TestReverbModelEstimator_ChangingDecay(t *testing.T) {
	const defaultDecay = -0.9
	for _, numCaptureChannels := range []int{1, 2, 4, 8} {
		test := newReverbModelEstimatorTest(defaultDecay, numCaptureChannels)
		test.runEstimator()
		assert.InDelta(t, reverbModelTestTruePowerDecay, test.estimatedDecay, 0.1)
		assert.InDelta(t, reverbModelTestTruePowerDecay, test.mildEstimatedDecay, 0.1)
		assert.InDelta(t, test.truePowerTailDB(), test.powerTailDB(), 5)
	}
}

// TestReverbModelEstimator_MatchesUpstream compares the decay of every
// 4th update and the frequency response of the reverb with the upstream
// ReverbModelEstimator at the pinned commit.
func TestReverbModelEstimator_MatchesUpstream(t *testing.T) {
	for _, run := range []reverbScenarioRun{{13, 0.83, ""}, {40, -0.83, ""}} {
		suffix := reverbScenarioSuffix(run.lengthBlocks, run.defaultLen, run.variant)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/reverb_model_estimator_upstream.txt", suffix)
			scenario := newReverbScenario(run.lengthBlocks, run.defaultLen, run.variant)
			estimator := newReverbModelEstimator(scenario.config, reverbScenarioChannels)

			for index := range reverbScenarioUpdates {
				scenario.step(index)
				estimator.update(scenario.h, scenario.h2, scenario.qualities, scenario.delays, scenario.usable,
					scenario.stationary)

				if index%4 == 0 {
					got := []float32{float32(index), estimator.reverbDecay(false), estimator.reverbDecay(true)}
					require.InDeltaSlice(t, reference.next("model_decay"), got, reverbReferenceDelta, "update %d", index)
				}
				// The reference only holds the frequency responses of the
				// long filter.
				if run.lengthBlocks == 40 && reverbScenarioDumpsSpectra(index) {
					require.Equal(t, []float32{float32(index)}, reference.next("step"))
					require.InDeltaSlice(t, reference.next("model_frequency_response"),
						estimator.getReverbFrequencyResponse()[:], reverbReferenceDelta, "update %d", index)
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
