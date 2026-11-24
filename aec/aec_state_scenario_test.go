// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import "fmt"

// aecStateScenario holds the state of the upstream reference scenario of
// the AecState: the subtractor scenario, driven by the AecState like the
// echo remover does.
//
// The render signal is silent in blocks 0 to 19 and 700 to 759 and weak
// in blocks 1000 to 1049. The capture signal is flagged as saturated in
// blocks 60 to 69 and 600 to 619. An external delay is reported from
// block 100 on. The AecState is told about a delay change at block 800.
// The echo path gain steps up at block 900, where the AecState is told
// about a gain change.
type aecStateScenario struct {
	*subtractorScenario
	state        *aecState
	fft          *aec3Fft
	captureOld   [][kBlockSize]float32
	y2, e2       [][kFftLengthBy2Plus1]float32
	delaySamples int
	hasEcho      bool
	blocks       int
	// aecDelayChangeBlock is the block where the AecState is told about a
	// delay change.
	aecDelayChangeBlock int
}

const (
	aecStateModeDefault = "default"
	// aecStateModeCustom uses the stationarity properties of the render
	// signal and a reverb decay of 0.7 for the nearend regions. The render
	// signal is at a lower level outside of the first 15 blocks of every
	// 150 blocks.
	aecStateModeCustom = "custom"
	// aecStateModeNoEcho has no echo in the capture signal. The AecState
	// is told about a delay change at block 300 instead, and the run lasts
	// 2000 blocks instead of 1590.
	aecStateModeNoEcho = "noecho"
)

// aecStateScenarioCase is a case of the reference: the channel counts,
// the delay of the echo path and the mode.
type aecStateScenarioCase struct {
	numRender, numCapture, delaySamples int
	mode                                string
}

func aecStateScenarioCases() []aecStateScenarioCase {
	return []aecStateScenarioCase{
		{1, 1, 150, aecStateModeDefault}, {2, 2, 330, aecStateModeCustom}, {1, 1, 150, aecStateModeNoEcho},
	}
}

// suffix is the end of the keys of the case in the reference files:
// "name render_channels capture_channels delay mode".
func (c aecStateScenarioCase) suffix() string {
	return fmt.Sprintf(" %d %d %d %s", c.numRender, c.numCapture, c.delaySamples, c.mode)
}

func (c aecStateScenarioCase) config() echoCanceller3Config {
	config := defaultEchoCanceller3Config()
	if c.mode == aecStateModeCustom {
		config.echoAudibility.useStationarityProperties = true
		config.epStrength.nearendLen = 0.7
	}

	return config
}

func newAecStateScenario(tc aecStateScenarioCase) *aecStateScenario {
	scenario := &aecStateScenario{
		subtractorScenario:  newSubtractorScenario(tc.numRender, tc.numCapture, tc.delaySamples),
		state:               newAecState(tc.config(), tc.numCapture),
		fft:                 newAec3Fft(),
		captureOld:          make([][kBlockSize]float32, tc.numCapture),
		y2:                  make([][kFftLengthBy2Plus1]float32, tc.numCapture),
		e2:                  make([][kFftLengthBy2Plus1]float32, tc.numCapture),
		delaySamples:        tc.delaySamples,
		hasEcho:             tc.mode != aecStateModeNoEcho,
		blocks:              1590,
		aecDelayChangeBlock: 800,
	}
	if !scenario.hasEcho {
		scenario.pathGain = func(int) float32 { return 0 }
		scenario.blocks = 2000
		scenario.aecDelayChangeBlock = 300
	}
	scenario.renderGain = func(index int) float32 {
		switch {
		case index < 20 || inRange(index, 700, 760):
			return 0
		case inRange(index, 1000, 1050):
			return 0.002
		case tc.mode == aecStateModeCustom && index%150 >= 15:
			return 0.03
		default:
			return 1
		}
	}

	return scenario
}

func (s *aecStateScenario) step(index int) {
	s.generate(index)
	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
	render := s.renderDelayBuffer.getRenderBuffer()

	s.state.updateCaptureSaturation(inRange(index, 60, 70) || inRange(index, 600, 620))
	if index == s.aecDelayChangeBlock {
		s.state.handleEchoPathChange(echoPathVariability{delayChange: delayAdjustmentNewDetectedDelay})
	}
	if index == 900 && s.hasEcho {
		s.state.handleEchoPathChange(echoPathVariability{gainChange: true})
	}
	s.analyzer.update(render, s.state.minDirectPathFilterDelay(), true)
	if s.state.transitionTriggered() {
		s.subtractor.exitInitialState()
	}
	s.subtractor.process(render, s.capture, s.analyzer, s.state.saturatedCapture(), s.outputs)

	for ch := range s.numCapture {
		var captureFft fftData
		capture := (*[kBlockSize]float32)(s.capture.view(0, ch))
		s.fft.paddedFft(capture, &s.captureOld[ch], windowSqrtHanning, &captureFft)
		s.captureOld[ch] = *capture
		captureFft.spectrum(&s.y2[ch])
		s.e2[ch] = s.outputs[ch].E2Refined
	}
	externalDelay := delayEstimate{quality: delayQualityRefined, delay: s.delaySamples / kBlockSize}
	s.state.update(externalDelay, index >= 100, s.subtractor.filterFrequencyResponses(),
		s.subtractor.filterImpulseResponses(), render, s.e2, s.y2, s.outputs)
}

// dumpsSpectra tells whether the reference holds the spectra of a block.
func (s *aecStateScenario) dumpsSpectra(index int) bool {
	return s.hasEcho &&
		(index%200 == 199 || (inRange(index, 20, 60) && index%10 == 5) || (inRange(index, 700, 770) && index%10 == 5))
}
