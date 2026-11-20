// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"

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
	assertNear(t, reference.next("e_refined"), out.eRefined[:], "e_refined")
	assertNear(t, reference.next("e_coarse"), out.eCoarse[:], "e_coarse")
	assertNear(t, reference.next("s_refined"), out.sRefined[:], "s_refined")
	assertNear(t, reference.next("E_refined_re"), out.ERefined.re[:], "E_refined_re")
	assertNear(t, reference.next("E_refined_im"), out.ERefined.im[:], "E_refined_im")
	assertNear(t, reference.next("E2_refined"), out.E2Refined[:], "E2_refined")
	assertNear(t, reference.next("E2_coarse"), out.E2Coarse[:], "E2_coarse")
	assertNear(t, reference.next("impulse_response"), s.subtractor.filterImpulseResponses()[channel],
		"impulse_response")
	h2 := s.subtractor.filterFrequencyResponses()[channel]
	require.Equal(t, []float32{float32(len(h2))}, reference.next("frequency_response_size"))
	assertNear(t, reference.next("frequency_response_1"), h2[1][:], "frequency_response_1")
}

// TestSubtractor_MatchesUpstream compares the outputs and the filter
// estimates with the upstream Subtractor at the pinned commit, built
// with the portable code paths. Each line of the reference file is keyed
// by "name render_channels capture_channels delay".
func TestSubtractor_MatchesUpstream(t *testing.T) {
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
