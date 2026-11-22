// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of EchoRemoverMultiChannel.BasicApiCalls: the basic API call
// sequence.
func TestEchoRemover_BasicApiCalls(t *testing.T) {
	for _, numRenderChannels := range []int{1, 2, 8} {
		for _, numCaptureChannels := range []int{1, 2, 8} {
			for _, rate := range []int{16000, 32000, 48000} {
				config := defaultEchoCanceller3Config()
				remover := newEchoRemover(config, rate, numRenderChannels, numCaptureChannels)
				renderDelayBuffer := newRenderDelayBufferImpl(config, rate, numRenderChannels)
				render := newBlock(numBandsForRate(rate), numRenderChannels)
				capture := newBlock(numBandsForRate(rate), numCaptureChannels)
				assert.NotPanics(t, func() {
					for k := range 100 {
						variability := echoPathVariability{gainChange: k%3 == 0}
						if k%5 == 0 {
							variability.delayChange = delayAdjustmentNewDetectedDelay
						}
						renderDelayBuffer.insert(render)
						renderDelayBuffer.prepareCaptureProcessing()
						remover.processCapture(variability, k%2 == 0, delayEstimate{}, false,
							renderDelayBuffer.getRenderBuffer(), nil, capture)
					}
				})
			}
		}
	}
}

// runBasicEchoRemoval runs the body of EchoRemover.BasicEchoRemoval for a
// rate, a channel count and a delay.
func runBasicEchoRemoval(t *testing.T, random *testRandom, rate, numChannels, delaySamples int) {
	t.Helper()
	const numBlocksToProcess = 500
	numBands := numBandsForRate(rate)
	render := newBlock(numBands, numChannels)
	capture := newBlock(numBands, numChannels)
	config := defaultEchoCanceller3Config()
	remover := newEchoRemover(config, rate, numChannels, numChannels)
	renderDelayBuffer := newRenderDelayBufferImpl(config, rate, numChannels)
	renderDelayBuffer.alignFromDelay(delaySamples / kBlockSize)
	delayBuffers := make([]*testDelayBuffer, numBands*numChannels)
	for i := range delayBuffers {
		delayBuffers[i] = newTestDelayBuffer(delaySamples)
	}

	var inputEnergy, outputEnergy float32
	for blockIndex := range numBlocksToProcess {
		silence := blockIndex < 100 || blockIndex%100 >= 10
		for band := range numBands {
			for channel := range numChannels {
				if silence {
					fill(render.view(band, channel), 0)
				} else {
					randomizeSampleVector(random, render.view(band, channel))
				}
				delayBuffers[band*numChannels+channel].delay(render.view(band, channel), capture.view(band, channel))
			}
		}
		if blockIndex > numBlocksToProcess/2 {
			inputEnergy += energy(capture.view(0, 0))
		}

		renderDelayBuffer.insert(render)
		renderDelayBuffer.prepareCaptureProcessing()
		remover.processCapture(echoPathVariability{}, false, delayEstimate{}, false,
			renderDelayBuffer.getRenderBuffer(), nil, capture)

		if blockIndex > numBlocksToProcess/2 {
			outputEnergy += energy(capture.view(0, 0))
		}
	}
	assert.Greater(t, inputEnergy, 10*outputEnergy, "rate %d, channels %d, delay %d", rate, numChannels, delaySamples)
}

// Port of EchoRemover.BasicEchoRemoval: a sanity check that the echo
// remover is able to properly remove echoes.
func TestEchoRemover_BasicEchoRemoval(t *testing.T) {
	random := newTestRandom(42)
	for _, numChannels := range []int{1, 2, 4} {
		for _, rate := range []int{16000, 32000, 48000} {
			for _, delaySamples := range []int{0, 64, 150, 200, 301} {
				runBasicEchoRemoval(t, random, rate, numChannels, delaySamples)
			}
		}
	}
}

// echoRemoverScenario holds the state of the upstream reference scenario
// of the echo remover.
//
// The render delay buffer has a default delay of one block and is reset
// after the first block. The render signal is noise, active in the first
// 80 blocks of every 100 blocks from block 100 on. The capture signal is
// the delayed render signal of the same band, downmixed and scaled by
// 0.6, then by 1.2 from block 600 on, where a gain change is signaled. It
// also holds weak noise, and a strong nearend signal in blocks 700 to 759
// and in the last 20 blocks of the hundreds 2, 6, 9, 10 and 13. The
// capture is flagged as saturated in blocks 820 to 829, a clock drift is
// signaled in blocks 900 to 999, and the capture output is not used in
// blocks 1300 to 1319.
//
// With custom, a delay change is signaled at block 1000, the low frequency
// gains are not smoothed and the nearend is not detected in the initial
// state, the upper bands gain is bounded during echo, and the reverb
// decay for the nearend regions is 0.7.
type echoRemoverScenario struct {
	numBands, numRender, numCapture int
	remover                         *echoRemover
	renderDelayBuffer               *renderDelayBufferImpl
	random                          *testRandom
	render, capture, linearOutput   *block
	delayBuffers                    []*testDelayBuffer
	custom                          bool
	// The energies of the last capture channel of the last block: the
	// first band of the input and of the output, the upper bands of the
	// output, and the output of the linear filter.
	inputEnergy, outputEnergy, upperEnergy, linearEnergy float32
}

const echoRemoverScenarioBlocks = 1500

func newEchoRemoverScenario(sampleRateHz, numRender, numCapture, delaySamples int, custom bool) *echoRemoverScenario {
	numBands := numBandsForRate(sampleRateHz)
	config := defaultEchoCanceller3Config()
	if custom {
		config.suppressor.lfSmoothingDuringInitialPhase = false
		config.suppressor.dominantNearendDetection.useDuringInitialPhase = false
		config.suppressor.highBandsSuppression.maxGainDuringEcho = 0.3
		config.suppressor.highBandsSuppression.enrThreshold = 0.5
		config.epStrength.nearendLen = 0.7
	}
	bufferConfig := config
	bufferConfig.delay.defaultDelay = 1
	scenario := &echoRemoverScenario{
		numBands:          numBands,
		numRender:         numRender,
		numCapture:        numCapture,
		remover:           newEchoRemover(config, sampleRateHz, numRender, numCapture),
		renderDelayBuffer: newRenderDelayBufferImpl(bufferConfig, sampleRateHz, numRender),
		random:            newTestRandom(42),
		render:            newBlock(numBands, numRender),
		capture:           newBlock(numBands, numCapture),
		linearOutput:      newBlock(1, numCapture),
		delayBuffers:      make([]*testDelayBuffer, numBands*numRender),
		custom:            custom,
	}
	for i := range scenario.delayBuffers {
		scenario.delayBuffers[i] = newTestDelayBuffer(delaySamples)
	}

	return scenario
}

// echoRemoverScenarioNearendBurst tells whether a block is in the last 20
// blocks of the hundreds 2, 6, 9, 10 and 13.
func echoRemoverScenarioNearendBurst(index int) bool {
	return index%100 >= 80 && slices.Contains([]int{2, 6, 9, 10, 13}, index/100)
}

// echoRemoverScenarioLevels returns the gain of the echo path and the
// amplitude of the nearend signal of a block.
func echoRemoverScenarioLevels(index int) (pathGain, nearendAmplitude float32) {
	pathGain, nearendAmplitude = 0.6, 30
	if index >= 600 {
		pathGain = 1.2
	}
	if inRange(index, 700, 760) || echoRemoverScenarioNearendBurst(index) {
		nearendAmplitude = 3000
	}

	return pathGain, nearendAmplitude
}

// generate draws the render and the capture signals of a block.
func (s *echoRemoverScenario) generate(index int) {
	activeRender := index >= 100 && index%100 < 80
	pathGain, nearendAmplitude := echoRemoverScenarioLevels(index)
	var echo, delayed, nearend [kBlockSize]float32
	for band := range s.numBands {
		echo = [kBlockSize]float32{}
		for ch := range s.numRender {
			samples := s.render.view(band, ch)
			randomizeSampleVectorAmplitude(s.random, samples, 10000)
			if !activeRender {
				clear(samples)
			}
			s.delayBuffers[band*s.numRender+ch].delay(samples, delayed[:])
			for k := range echo {
				echo[k] += delayed[k] / float32(s.numRender)
			}
		}
		for ch := range s.numCapture {
			randomizeSampleVectorAmplitude(s.random, nearend[:], nearendAmplitude)
			samples := s.capture.view(band, ch)
			for k := range echo {
				samples[k] = pathGain*echo[k]/float32(ch+1) + nearend[k]
			}
		}
	}
}

// step processes a block.
func (s *echoRemoverScenario) step(index int) {
	last := s.numCapture - 1
	s.generate(index)
	s.inputEnergy = energy(s.capture.view(0, last))

	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
	variability := echoPathVariability{gainChange: index == 600, clockDrift: inRange(index, 900, 1000)}
	if s.custom && index == 1000 {
		variability.delayChange = delayAdjustmentNewDetectedDelay
	}
	s.remover.setCaptureOutputUsage(!inRange(index, 1300, 1320))
	s.remover.processCapture(variability, inRange(index, 820, 830), delayEstimate{}, false,
		s.renderDelayBuffer.getRenderBuffer(), s.linearOutput, s.capture)

	s.outputEnergy = energy(s.capture.view(0, last))
	s.upperEnergy = 0
	for band := 1; band < s.numBands; band++ {
		s.upperEnergy += energy(s.capture.view(band, last))
	}
	s.linearEnergy = energy(s.linearOutput.view(0, last))
}

// fusesMultiplyAdd tells whether the Go compiler fuses a multiplication
// and an addition into one operation on this architecture, which changes
// the rounding of the floating-point computations.
func fusesMultiplyAdd() bool {
	switch runtime.GOARCH {
	case "arm64", "ppc64", "ppc64le", "riscv64", "s390x", "loong64":
		return true
	default:
		return false
	}
}

// levelDeviations collects the deviations in dB between energies and
// their reference.
type levelDeviations []float64

// add records the deviation of an energy, unless it is negligible.
func (l *levelDeviations) add(want, got float32) {
	if want > 1e-3 && got > 1e-3 {
		*l = append(*l, math.Abs(10*math.Log10(float64(got)/float64(want))))
	}
}

// assertBelow checks that the deviations are below a limit. On the
// architectures that fuse multiply-add, it instead checks the median, the
// 90th and the 99th percentiles against the limits that the upstream
// implementation, built with fused multiply-add, meets with a margin.
func (l levelDeviations) assertBelow(t *testing.T, name string, limit, median, p90, p99 float64) {
	t.Helper()
	slices.Sort(l)
	require.NotEmpty(t, l, name)
	if !fusesMultiplyAdd() {
		assert.LessOrEqual(t, l[len(l)-1], limit, name)

		return
	}
	assert.LessOrEqual(t, l[len(l)/2], median, "%s, median", name)
	assert.LessOrEqual(t, l[len(l)*9/10], p90, "%s, 90th percentile", name)
	assert.LessOrEqual(t, l[len(l)*99/100], p99, "%s, 99th percentile", name)
}

// TestEchoRemover_MatchesUpstream compares the energy of the output of
// every block, in dB, with the upstream EchoRemover at the pinned commit:
// the first band and the upper bands of the output, and the output of the
// linear filter. It also compares the metrics every 50th block.
//
// The output of the echo remover depends on the rounding of the
// floating-point computations, which feeds back through the adaptation of
// the filters. Upstream itself, built with and without fused
// multiply-add, gives energies that differ by 0.002 dB for the median
// block, by up to 0.07 dB for 90% of the blocks and 1.6 dB for 99% of
// them for the output, and by up to 2.3 dB and 4.7 dB for the output of
// the linear filter. The comparison is therefore looser on the
// architectures where Go fuses multiply-add. The input energy is compared
// exactly.
//
// Each line of the reference file is keyed by
// "name sample_rate render_channels capture_channels delay", plus
// "custom" for the custom scenario.
func TestEchoRemover_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		sampleRateHz, numRender, numCapture, delaySamples int
		custom                                            bool
	}{{48000, 1, 1, 150, false}, {16000, 2, 2, 64, false}, {48000, 2, 1, 150, true}} {
		suffix := fmt.Sprintf(" %d %d %d %d", tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples)
		if tc.custom {
			suffix += " custom"
		}
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/echo_remover_upstream.txt", suffix)
			reference.filter("output")
			metricsReference := reference.filter("metrics")
			scenario := newEchoRemoverScenario(tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples, tc.custom)
			var output, upper, linear, erl, erle levelDeviations

			for index := range echoRemoverScenarioBlocks {
				scenario.step(index)

				// The block index, then the energies of the input, the
				// output, its upper bands and the output of the linear
				// filter.
				want := reference.next("energies")
				require.Equal(t, float32(index), want[0])
				require.InEpsilon(t, want[1], scenario.inputEnergy, 1e-5, "block %d", index)
				output.add(want[2], scenario.outputEnergy)
				upper.add(want[3], scenario.upperEnergy)
				linear.add(want[4], scenario.linearEnergy)

				if index%50 == 49 {
					wantMetrics := metricsReference.next("metrics")
					gotErl, gotErle := scenario.remover.getMetrics()
					erl = append(erl, math.Abs(gotErl-float64(wantMetrics[1])))
					erle = append(erle, math.Abs(gotErle-float64(wantMetrics[2])))
				}
			}
			output.assertBelow(t, "output", 0.01, 0.05, 0.5, 5)
			if tc.sampleRateHz > 16000 {
				upper.assertBelow(t, "upper bands", 0.01, 0.05, 1, 6)
			}
			linear.assertBelow(t, "linear output", 0.01, 0.05, 3, 6)
			erl.assertBelow(t, "ERL", 0.001, 0.01, 0.01, 0.01)
			erle.assertBelow(t, "ERLE", 0.001, 0.1, 1, 1)
			require.Empty(t, reference.entries)
			require.Empty(t, metricsReference.entries)
		})
	}
}
