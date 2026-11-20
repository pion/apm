// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import "fmt"

// reverbScenario holds the state of the upstream reference scenario of
// the reverb estimators: two capture channels with synthetic filters.
//
// The impulse response of a filter is zero up to a peak in the block of
// its delay, flat for four blocks, then decays down to a noise floor. Its
// frequency response decays with the block index. The filters are drawn
// again every 500 updates, with a slower decay from update 1500 on. The
// filter quality is unknown every 7th update, the block is stationary
// every 11th update, the delay is 0 in updates 1000 to 1009 and the
// linear filter is not usable in updates 2000 to 2004.
type reverbScenario struct {
	config     echoCanceller3Config
	random     *testRandom
	h          [][]float32
	h2         [][][kFftLengthBy2Plus1]float32
	qualities  []optionalFloat32
	delays     []int
	usable     []bool
	stationary bool
}

const (
	reverbScenarioChannels = 2
	reverbScenarioUpdates  = 3000
)

func newReverbScenario(lengthBlocks int, defaultLen float32) *reverbScenario {
	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = lengthBlocks
	config.epStrength.defaultLen = defaultLen
	scenario := &reverbScenario{
		config:    config,
		random:    newTestRandom(42),
		h:         make([][]float32, reverbScenarioChannels),
		h2:        make([][][kFftLengthBy2Plus1]float32, reverbScenarioChannels),
		qualities: make([]optionalFloat32, reverbScenarioChannels),
		delays:    make([]int, reverbScenarioChannels),
		usable:    make([]bool, reverbScenarioChannels),
	}
	for ch := range reverbScenarioChannels {
		scenario.h[ch] = make([]float32, lengthBlocks*kBlockSize)
		scenario.h2[ch] = make([][kFftLengthBy2Plus1]float32, lengthBlocks)
	}

	return scenario
}

// reverbScenarioSuffix is the end of the keys of a run in the reference
// files: "name filter_length_blocks default_len".
func reverbScenarioSuffix(lengthBlocks int, defaultLen float32) string {
	return fmt.Sprintf(" %d %g", lengthBlocks, defaultLen)
}

func (s *reverbScenario) drawFilter(ch int, sampleDecay, blockDecay float32) {
	delay := 2 + ch
	peak := delay*kBlockSize + 5
	envelope := float32(0.3)
	for k := range s.h[ch] {
		noise := 2*s.random.randFloat() - 1
		switch {
		case k < peak:
			s.h[ch][k] = 0
		case k == peak:
			s.h[ch][k] = 0.8
		default:
			if k >= peak+4*kBlockSize {
				envelope *= sampleDecay
			}
			s.h[ch][k] = float32(envelope*noise) + float32(1e-3*noise)
		}
	}

	gain := float32(1)
	for partition := range s.h2[ch] {
		if partition > delay {
			gain *= blockDecay
		}
		for k := range s.h2[ch][partition] {
			factor := 0.5 + s.random.randFloat()
			if partition < delay {
				s.h2[ch][partition][k] = 1e-4 * factor
			} else {
				s.h2[ch][partition][k] = gain * factor
			}
		}
	}
}

func (s *reverbScenario) step(index int) {
	if index%500 == 0 {
		sampleDecay, blockDecay := float32(0.9946), float32(0.5)
		if index >= 1500 {
			sampleDecay, blockDecay = 0.9973, 0.7
		}
		for ch := range reverbScenarioChannels {
			s.drawFilter(ch, sampleDecay, blockDecay)
		}
	}
	for ch := range reverbScenarioChannels {
		quality := s.random.randFloat()
		s.qualities[ch] = optionalFloat32{value: quality, valid: index%7 != 3}
		if !s.qualities[ch].valid {
			s.qualities[ch].value = 0
		}
		s.delays[ch] = 2 + ch
		if index >= 1000 && index < 1010 {
			s.delays[ch] = 0
		}
		s.usable[ch] = index < 2000 || index >= 2005
	}
	s.stationary = index%11 == 5
}

// reverbScenarioDumpsSpectra tells whether the reference holds the
// frequency responses of an update.
func reverbScenarioDumpsSpectra(index int) bool { return index%50 == 49 || index < 20 }
