// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import "fmt"

// erleScenario holds the state of the upstream reference scenario of the
// ERLE estimators. The render signal is noise at 16 kHz. It is strong in
// blocks 0 to 199, close to the render energy threshold in blocks 200 to
// 399, silent in blocks 400 to 699, strong in blocks 700 to 999, weak in
// blocks 1000 to 1099, then alternates every 50 blocks between strong and
// close to the threshold. The render spectrum given to the estimators is
// drawn at random with the same level, so that the inputs of the
// estimators are the same as upstream to the last bit.
//
// The capture spectrum is this spectrum scaled per channel plus noise. The error spectrum is the capture spectrum divided by an ERLE of
// 10, of 1 in blocks 700 to 709, and of 3 from block 1200 on, times a
// random factor per band. No filter is converged in blocks 300 to 319,
// and each filter is converged 95% of the time otherwise. The frequency
// response of the filter decays with the partition index.
type erleScenario struct {
	numRender, numCapture int
	random                *testRandom
	renderDelayBuffer     *renderDelayBufferImpl
	render                *block
	x2                    [kFftLengthBy2Plus1]float32
	y2, e2                [][kFftLengthBy2Plus1]float32
	h2                    [][][kFftLengthBy2Plus1]float32
	converged             []bool
}

const (
	erleScenarioBlocks     = 1500
	erleScenarioResetBlock = 850
)

func newErleScenario(numRender, numCapture int) *erleScenario {
	return newErleScenarioWithConfig(defaultEchoCanceller3Config(), numRender, numCapture)
}

func newErleScenarioWithConfig(config echoCanceller3Config, numRender, numCapture int) *erleScenario {
	scenario := &erleScenario{
		numRender:         numRender,
		numCapture:        numCapture,
		random:            newTestRandom(42),
		renderDelayBuffer: newRenderDelayBufferImpl(config, 16000, numRender),
		render:            newBlock(1, numRender),
		y2:                make([][kFftLengthBy2Plus1]float32, numCapture),
		e2:                make([][kFftLengthBy2Plus1]float32, numCapture),
		h2:                make([][][kFftLengthBy2Plus1]float32, numCapture),
		converged:         make([]bool, numCapture),
	}
	scenario.renderDelayBuffer.alignFromDelay(4)
	for ch := range scenario.h2 {
		scenario.h2[ch] = make([][kFftLengthBy2Plus1]float32, config.filter.refined.lengthBlocks)
		scale := 1 + float32(ch)
		for partition := range scenario.h2[ch] {
			for k := range scenario.h2[ch][partition] {
				scenario.h2[ch][partition][k] = scale * (0.5 + scenario.random.randFloat())
			}
			scale *= 0.5
		}
	}

	return scenario
}

func erleScenarioRenderGain(index int) float32 {
	switch {
	case index >= 400 && index < 700:
		return 0
	case index >= 1000 && index < 1100:
		return 0.002
	case index >= 200 && index < 400, index >= 1100 && (index/50)%2 == 1:
		return 0.035
	default:
		return 1
	}
}

func erleScenarioTrueErle(index int) float32 {
	switch {
	case index >= 700 && index < 710:
		return 1
	case index >= 1200:
		return 3
	default:
		return 10
	}
}

func (s *erleScenario) step(index int) {
	renderGain := erleScenarioRenderGain(index)
	trueErle := erleScenarioTrueErle(index)

	for ch := range s.numRender {
		channel := s.render.view(0, ch)
		randomizeSampleVector(s.random, channel)
		for k := range channel {
			channel[k] *= renderGain
		}
	}
	s.renderDelayBuffer.insert(s.render)
	s.renderDelayBuffer.prepareCaptureProcessing()

	renderLevel := renderGain * renderGain * 1e11
	for k := range s.x2 {
		u := s.random.randFloat()
		s.x2[k] = renderLevel * u * u
	}
	for ch := range s.numCapture {
		for k := range s.x2 {
			noise := s.random.randFloat()
			factor := s.random.randFloat()
			s.y2[ch][k] = s.x2[k]*9/float32(ch+1) + 1e4*noise
			s.e2[ch][k] = s.y2[ch][k] / (trueErle * (0.5 + factor))
		}
		draw := s.random.randFloat()
		s.converged[ch] = draw > 0.05 && (index < 300 || index >= 320)
	}
}

// erleScenarioCases lists the render and capture channel counts of the
// reference.
func erleScenarioCases() []struct{ numRender, numCapture int } {
	return []struct{ numRender, numCapture int }{{1, 1}, {2, 2}}
}

// erleScenarioSuffix is the end of the keys of a case in the reference
// files: "name render_channels capture_channels".
func erleScenarioSuffix(numRender, numCapture int) string {
	return fmt.Sprintf(" %d %d", numRender, numCapture)
}

// erleScenarioDumpsSpectra tells whether the reference holds the spectra
// of a block.
func erleScenarioDumpsSpectra(index int) bool {
	return index%50 == 49 || (index >= 200 && index < 220 && index%4 == 0) ||
		(index >= 690 && index < 760 && index%5 == 0) || (index >= 1000 && index < 1120 && index%10 == 0)
}

func (s *erleScenario) renderBuffer() *renderBuffer { return s.renderDelayBuffer.getRenderBuffer() }
