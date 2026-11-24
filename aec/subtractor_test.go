// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/dsp/biquad"
	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subtractorScenario holds the state of the upstream reference scenario.
// Every capture channel holds the delayed render, downmixed and scaled
// per channel, plus noise. The initial state is left at block 300, the
// capture is saturated for a while, the echo path gain steps up at block
// 900 and a delay change is signaled at block 1200.
//
// The scenario stops three blocks later. The coarse filter is then reset
// from the refined one, the two errors become equal, and which filter
// counts as better depends on the last bit of the FFT.
type subtractorScenario struct {
	numRender, numCapture int
	subtractor            *subtractor
	renderDelayBuffer     *renderDelayBufferImpl
	analyzer              *renderSignalAnalyzer
	random                *testRandom
	render, capture       *block
	outputs               []subtractorOutput
	delayBuffers          []*testDelayBuffer
	// delayChangeBlock is the block where a delay change is signaled.
	delayChangeBlock int
	// renderGain scales the render signal of a block, when set.
	renderGain func(index int) float32
	// pathGain replaces the gain of the echo path of a block, when set.
	pathGain func(index int) float32
}

const subtractorScenarioBlocks = 1203

func newSubtractorScenario(numRender, numCapture, delaySamples int) *subtractorScenario {
	const sampleRateHz = 48000
	config := defaultEchoCanceller3Config()
	sub := newSubtractor(config, numRender, numCapture)
	config.delay.defaultDelay = 1
	scenario := &subtractorScenario{
		numRender:         numRender,
		numCapture:        numCapture,
		subtractor:        sub,
		renderDelayBuffer: newRenderDelayBufferImpl(config, sampleRateHz, numRender),
		analyzer:          newRenderSignalAnalyzer(config),
		random:            newTestRandom(42),
		render:            newBlock(numBandsForRate(sampleRateHz), numRender),
		capture:           newBlock(1, numCapture),
		outputs:           make([]subtractorOutput, numCapture),
		delayBuffers:      make([]*testDelayBuffer, numRender),
		delayChangeBlock:  1200,
	}
	for ch := range scenario.delayBuffers {
		scenario.delayBuffers[ch] = newTestDelayBuffer(delaySamples)
	}

	return scenario
}

func (s *subtractorScenario) generate(index int) {
	echo := make([]float32, kBlockSize)
	delayed := make([]float32, kBlockSize)
	noise := make([]float32, kBlockSize)
	for ch := range s.numRender {
		randomizeSampleVector(s.random, s.render.view(0, ch))
		if s.renderGain != nil {
			gain := s.renderGain(index)
			for k, sample := range s.render.view(0, ch) {
				s.render.view(0, ch)[k] = sample * gain
			}
		}
		s.delayBuffers[ch].delay(s.render.view(0, ch), delayed)
		for k := range echo {
			echo[k] += delayed[k] / float32(s.numRender)
		}
	}

	// A gain step in the echo path from block 900 on.
	pathGain := float32(0.5)
	if index >= 900 {
		pathGain = 1.9
	}
	if s.pathGain != nil {
		pathGain = s.pathGain(index)
	}
	for ch := range s.numCapture {
		randomizeSampleVector(s.random, noise)
		channel := s.capture.view(0, ch)
		for k := range echo {
			channel[k] = pathGain*echo[k]/float32(ch+1) + noise[k]/100
		}
	}
}

func (s *subtractorScenario) step(index int) {
	s.generate(index)
	saturatedCapture := index >= 600 && index < 620
	switch index {
	case 300:
		s.subtractor.exitInitialState()
	case s.delayChangeBlock:
		s.subtractor.handleEchoPathChange(echoPathVariability{delayChange: delayAdjustmentNewDetectedDelay})
	}

	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
	renderBuffer := s.renderDelayBuffer.getRenderBuffer()
	s.analyzer.update(renderBuffer, 0, true)
	s.subtractor.process(renderBuffer, s.capture, s.analyzer, saturatedCapture, s.outputs)
}

// assertMetrics compares the scalar metrics of a block for every capture
// channel: the block index and the channel, then s2 and e2 for both
// filters, y2 and the largest magnitudes of the echo estimates.
func (s *subtractorScenario) assertMetrics(t *testing.T, reference *referenceCursor, index int) {
	t.Helper()
	for ch := range s.outputs {
		out := &s.outputs[ch]
		want := reference.next("metrics")
		require.Equal(t, []float32{float32(index), float32(ch)}, want[:2])
		got := []float32{
			out.s2Refined, out.s2Coarse, out.e2Refined, out.e2Coarse, out.y2,
			out.sRefinedMaxAbs, out.sCoarseMaxAbs,
		}
		assert.InEpsilonSlice(t, want[2:], got, 1e-3, "metrics, block %d, channel %d", index, ch)
	}
}

// assertOutput compares the output and the filter estimates of the last
// capture channel.
func (s *subtractorScenario) assertOutput(t *testing.T, reference *referenceCursor, index int) {
	t.Helper()
	channel := s.numCapture - 1
	out := &s.outputs[channel]
	require.Equal(t, []float32{float32(index)}, reference.next("step"))
	assertMatchesUpstream(t, reference.next("e_refined"), out.eRefined[:], "e_refined")
	assertMatchesUpstream(t, reference.next("e_coarse"), out.eCoarse[:], "e_coarse")
	assertMatchesUpstream(t, reference.next("s_refined"), out.sRefined[:], "s_refined")
	assertMatchesUpstream(t, reference.next("E_refined_re"), out.ERefined.re[:], "E_refined_re")
	assertMatchesUpstream(t, reference.next("E_refined_im"), out.ERefined.im[:], "E_refined_im")
	assertMatchesUpstream(t, reference.next("E2_refined"), out.E2Refined[:], "E2_refined")
	assertMatchesUpstream(t, reference.next("E2_coarse"), out.E2Coarse[:], "E2_coarse")
	assertMatchesUpstream(t, reference.next("impulse_response"), s.subtractor.filterImpulseResponses()[channel],
		"impulse_response")
	h2 := s.subtractor.filterFrequencyResponses()[channel]
	require.Equal(t, []float32{float32(len(h2))}, reference.next("frequency_response_size"))
	assertMatchesUpstream(t, reference.next("frequency_response_1"), h2[1][:], "frequency_response_1")
}

// TestSubtractor_MatchesUpstream compares the outputs and the filter
// estimates with the upstream Subtractor at the pinned commit, built
// with the portable code paths. Each line of the reference file is keyed
// by "name render_channels capture_channels delay".
func TestSubtractor_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	for _, tc := range []struct{ numRender, numCapture, delaySamples int }{{1, 1, 150}, {2, 2, 64}} {
		suffix := fmt.Sprintf(" %d %d %d", tc.numRender, tc.numCapture, tc.delaySamples)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/subtractor_upstream.txt", suffix)
			scenario := newSubtractorScenario(tc.numRender, tc.numCapture, tc.delaySamples)

			for index := range subtractorScenarioBlocks {
				scenario.step(index)
				if index%25 == 24 || (index >= 895 && index < 925) || index >= 1195 {
					scenario.assertMetrics(t, reference, index)
				}
				if index == 40 || index == 910 || index == subtractorScenarioBlocks-1 {
					scenario.assertOutput(t, reference, index)
				}
			}
		})
	}
}

// newSubtractorTestHighPass returns the high-pass filters of
// RunSubtractorTest, one per channel.
func newSubtractorTestHighPass(numChannels int) []*biquad.Cascade {
	// [B,A] = butter(2,100/8000,'high')
	coefficients := []biquad.Coefficients{{B: [3]float32{0.97261, -1.94523, 0.97261}, A: [2]float32{-1.94448, 0.94598}}}
	filters := make([]*biquad.Cascade, numChannels)
	for ch := range filters {
		filters[ch] = biquad.NewCascade(coefficients)
	}

	return filters
}

func newTestDelayBuffers(numChannels, delaySamples int) []*testDelayBuffer {
	buffers := make([]*testDelayBuffer, numChannels)
	for ch := range buffers {
		buffers[ch] = newTestDelayBuffer(delaySamples)
	}

	return buffers
}

// addSubtractorTestEcho adds the delayed render, downmixed, to every
// capture channel. Like upstream, the echo is added to the previous
// capture block, so the capture grows far beyond the range the refined
// error is clamped to, and the correlated tests pass even with a filter
// that does not adapt.
func addSubtractorTestEcho(render, capture *block, delayBuffers [][]*testDelayBuffer) {
	var channelCapture [kBlockSize]float32
	for captureCh, buffers := range delayBuffers {
		view := capture.view(0, captureCh)
		for renderCh, buffer := range buffers {
			buffer.delay(render.view(0, renderCh), channelCapture[:])
			for k := range view {
				view[k] += channelCapture[k] / float32(len(buffers))
			}
		}
	}
}

// runSubtractorTest mirrors RunSubtractorTest in subtractor_unittest.cc,
// without the echo path changes that no test uses. It returns the ratio
// of the refined filter error power to the capture power for each
// capture channel.
func runSubtractorTest(
	t *testing.T,
	numRenderChannels, numCaptureChannels, numBlocksToProcess, delaySamples int,
	refinedFilterLengthBlocks, coarseFilterLengthBlocks int,
	uncorrelatedInputs bool,
) []float32 {
	t.Helper()
	const sampleRateHz = 48000
	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = refinedFilterLengthBlocks
	config.filter.coarse.lengthBlocks = coarseFilterLengthBlocks

	sub := newSubtractor(config, numRenderChannels, numCaptureChannels)
	render := newBlock(numBandsForRate(sampleRateHz), numRenderChannels)
	capture := newBlock(1, numCaptureChannels)
	output := make([]subtractorOutput, numCaptureChannels)
	config.delay.defaultDelay = 1
	renderDelayBuffer := newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels)
	analyzer := newRenderSignalAnalyzer(config)
	random := newTestRandom(42)
	y2 := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	e2Refined := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	state := newAecState(config, numCaptureChannels)

	delayBuffers := make([][]*testDelayBuffer, numCaptureChannels)
	for captureCh := range delayBuffers {
		delayBuffers[captureCh] = newTestDelayBuffers(numRenderChannels, delaySamples)
	}

	renderHighPass := newSubtractorTestHighPass(numRenderChannels)
	captureHighPass := newSubtractorTestHighPass(numCaptureChannels)

	for blockNum := range numBlocksToProcess {
		for ch := range numRenderChannels {
			randomizeSampleVector(random, render.view(0, ch))
		}
		if uncorrelatedInputs {
			for ch := range numCaptureChannels {
				randomizeSampleVector(random, capture.view(0, ch))
			}
		} else {
			addSubtractorTestEcho(render, capture, delayBuffers)
		}
		for ch, filter := range renderHighPass {
			filter.Process(render.view(0, ch))
		}
		for ch, filter := range captureHighPass {
			filter.Process(capture.view(0, ch))
		}

		renderDelayBuffer.insert(render)
		if blockNum == 0 {
			renderDelayBuffer.reset()
		}
		renderDelayBuffer.prepareCaptureProcessing()
		renderBuffer := renderDelayBuffer.getRenderBuffer()
		analyzer.update(renderBuffer, state.minDirectPathFilterDelay(), true)

		sub.process(renderBuffer, capture, analyzer, state.saturatedCapture(), output)

		state.handleEchoPathChange(echoPathVariability{})
		state.update(delayEstimate{}, false, sub.filterFrequencyResponses(), sub.filterImpulseResponses(),
			renderBuffer, e2Refined, y2, output)
	}

	results := make([]float32, numCaptureChannels)
	for ch := range results {
		capturePower := energy(capture.view(0, ch))
		assert.NotZero(t, capturePower)
		results[ch] = energy(output[ch].eRefined[:]) / capturePower
	}

	return results
}

// Port of Convergence.
func TestSubtractor_Convergence(t *testing.T) {
	for _, filterLengthBlocks := range []int{12, 20, 30} {
		for _, delaySamples := range []int{0, 64, 150, 200, 301} {
			t.Run(fmt.Sprintf("length %d delay %d", filterLengthBlocks, delaySamples), func(t *testing.T) {
				t.Parallel()
				for _, power := range runSubtractorTest(t, 1, 1, 2500, delaySamples,
					filterLengthBlocks, filterLengthBlocks, false) {
					assert.Less(t, power, float32(0.1))
				}
			})
		}
	}
}

// Port of RefinedFilterLongerThanCoarseFilter.
func TestSubtractor_RefinedFilterLongerThanCoarseFilter(t *testing.T) {
	for _, power := range runSubtractorTest(t, 1, 1, 400, 64, 20, 15, false) {
		assert.Less(t, power, float32(0.5))
	}
}

// Port of CoarseFilterLongerThanRefinedFilter.
func TestSubtractor_CoarseFilterLongerThanRefinedFilter(t *testing.T) {
	for _, power := range runSubtractorTest(t, 1, 1, 400, 64, 15, 20, false) {
		assert.Less(t, power, float32(0.5))
	}
}

// Port of NonConvergenceOnUncorrelatedSignals.
func TestSubtractor_NonConvergenceOnUncorrelatedSignals(t *testing.T) {
	for _, filterLengthBlocks := range []int{12, 20, 30} {
		for _, delaySamples := range []int{0, 64, 150, 200, 301} {
			t.Run(fmt.Sprintf("length %d delay %d", filterLengthBlocks, delaySamples), func(t *testing.T) {
				t.Parallel()
				for _, power := range runSubtractorTest(t, 1, 1, 3000, delaySamples,
					filterLengthBlocks, filterLengthBlocks, true) {
					assert.InDelta(t, 1, power, 0.1)
				}
			})
		}
	}
}

// subtractorChannelCounts mirrors the NonDebugMultiChannel and, in race
// builds, the DebugMultiChannel instantiations.
func subtractorChannelCounts(render, capture []int) (renderCounts, captureCounts []int) {
	if raceEnabled {
		return []int{1, 2}, []int{1, 2}
	}

	return render, capture
}

// Port of SubtractorMultiChannelUpToEightRender.Convergence.
func TestSubtractor_MultiChannelConvergence(t *testing.T) {
	renderCounts, captureCounts := subtractorChannelCounts([]int{1, 2, 8}, []int{1, 2, 4})
	for _, numRenderChannels := range renderCounts {
		for _, numCaptureChannels := range captureCounts {
			t.Run(fmt.Sprintf("render %d capture %d", numRenderChannels, numCaptureChannels), func(t *testing.T) {
				t.Parallel()
				for _, power := range runSubtractorTest(t, numRenderChannels, numCaptureChannels,
					2500*numRenderChannels, 64, 20, 20, false) {
					assert.Less(t, power, float32(0.1))
				}
			})
		}
	}
}

// Port of SubtractorMultiChannelUpToFourRender.NonConvergenceOnUncorrelatedSignals.
func TestSubtractor_MultiChannelNonConvergenceOnUncorrelatedSignals(t *testing.T) {
	renderCounts, captureCounts := subtractorChannelCounts([]int{1, 2, 4}, []int{1, 2, 4})
	for _, numRenderChannels := range renderCounts {
		for _, numCaptureChannels := range captureCounts {
			t.Run(fmt.Sprintf("render %d capture %d", numRenderChannels, numCaptureChannels), func(t *testing.T) {
				t.Parallel()
				for _, power := range runSubtractorTest(t, numRenderChannels, numCaptureChannels,
					5000*numRenderChannels, 64, 20, 20, true) {
					assert.Greater(t, power, float32(0.8))
					assert.InDelta(t, 1, power, 0.25)
				}
			})
		}
	}
}
