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
//
// The "slow" variant has impulse responses that decay slowly, the "fast"
// variant has ones that decay fast, down to a lower noise floor.
type reverbScenario struct {
	config     echoCanceller3Config
	variant    string
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

	reverbVariantSlow = "slow"
	reverbVariantFast = "fast"
)

func newReverbScenario(lengthBlocks int, defaultLen float32, variant string) *reverbScenario {
	config := defaultEchoCanceller3Config()
	config.filter.refined.lengthBlocks = lengthBlocks
	config.epStrength.defaultLen = defaultLen
	scenario := &reverbScenario{
		config:    config,
		variant:   variant,
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
// files: "name filter_length_blocks default_len", plus the variant when
// there is one.
func reverbScenarioSuffix(lengthBlocks int, defaultLen float32, variant string) string {
	suffix := fmt.Sprintf(" %d %g", lengthBlocks, defaultLen)
	if variant != "" {
		suffix += " " + variant
	}

	return suffix
}

// sampleDecay returns the decay per sample of the impulse responses.
func (s *reverbScenario) sampleDecay(index int) float32 {
	decays := map[string][2]float32{
		"": {0.9946, 0.9973}, reverbVariantSlow: {0.9997, 0.9999}, reverbVariantFast: {0.96, 0.97},
	}[s.variant]
	if index < 1500 {
		return decays[0]
	}

	return decays[1]
}

func (s *reverbScenario) drawFilter(ch int, sampleDecay, blockDecay float32) {
	delay := 2 + ch
	s.drawImpulseResponse(s.h[ch], delay*kBlockSize+5, sampleDecay)

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

func (s *reverbScenario) drawImpulseResponse(response []float32, peak int, sampleDecay float32) {
	noiseFloor := float32(1e-3)
	if s.variant == reverbVariantFast {
		noiseFloor = 1e-9
	}
	envelope := float32(0.3)
	for k := range response {
		noise := 2*s.random.randFloat() - 1
		switch {
		case k < peak:
			response[k] = 0
		case k == peak:
			response[k] = 0.8
		default:
			if k >= peak+4*kBlockSize {
				envelope *= sampleDecay
			}
			response[k] = float32(envelope*noise) + float32(noiseFloor*noise)
		}
	}
}

func (s *reverbScenario) step(index int) {
	if index%500 == 0 {
		blockDecay := float32(0.5)
		if index >= 1500 {
			blockDecay = 0.7
		}
		for ch := range reverbScenarioChannels {
			s.drawFilter(ch, s.sampleDecay(index), blockDecay)
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
