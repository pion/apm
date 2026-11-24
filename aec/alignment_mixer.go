// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// mixingVariant mirrors AlignmentMixer::MixingVariant.
type mixingVariant int

const (
	mixingDownmix mixingVariant = iota
	mixingAdaptive
	mixingFixed
)

// chooseMixingVariant matches ChooseMixingVariant.
func chooseMixingVariant(downmix, adaptiveSelection bool, numChannels int) mixingVariant {
	if numChannels == 1 {
		return mixingFixed
	}
	if downmix {
		return mixingDownmix
	}
	if adaptiveSelection {
		return mixingAdaptive
	}

	return mixingFixed
}

// alignmentMixer mirrors AlignmentMixer: it produces the single channel
// the delay estimator aligns on.
type alignmentMixer struct {
	numChannels               int
	oneByNumChannels          float32
	excitationEnergyThreshold float32
	preferFirstTwoChannels    bool
	selectionVariant          mixingVariant
	strongBlockCounters       [2]int
	cumulativeEnergies        []float32
	selectedChannel           int
	blockCounter              int
}

func newAlignmentMixer(numChannels int, cfg alignmentMixing) *alignmentMixer {
	mixer := &alignmentMixer{
		numChannels:               numChannels,
		oneByNumChannels:          1 / float32(numChannels),
		excitationEnergyThreshold: kBlockSize * cfg.activityPowerThreshold,
		preferFirstTwoChannels:    cfg.preferFirstTwoChannels,
		selectionVariant:          chooseMixingVariant(cfg.downmix, cfg.adaptiveSelection, numChannels),
	}
	if mixer.selectionVariant == mixingAdaptive {
		mixer.cumulativeEnergies = make([]float32, numChannels)
	}

	return mixer
}

// produceOutput mirrors ProduceOutput. out holds kBlockSize samples.
func (m *alignmentMixer) produceOutput(x *block, out []float32) {
	if m.selectionVariant == mixingDownmix {
		m.downmix(x, out)

		return
	}

	channel := 0
	if m.selectionVariant != mixingFixed {
		channel = m.selectChannel(x)
	}
	copy(out, x.view(0, channel))
}

func (m *alignmentMixer) downmix(x *block, out []float32) {
	copy(out, x.view(0, 0))
	for channel := 1; channel < m.numChannels; channel++ {
		for i, sample := range x.view(0, channel) {
			out[i] += sample
		}
	}
	for i := range out[:kBlockSize] {
		out[i] *= m.oneByNumChannels
	}
}

func (m *alignmentMixer) selectChannel(x *block) int {
	const blocksToChooseLeftOrRight = kNumBlocksPerSecond / 2
	goodSignalInLeftOrRight := m.preferFirstTwoChannels &&
		(m.strongBlockCounters[0] > blocksToChooseLeftOrRight ||
			m.strongBlockCounters[1] > blocksToChooseLeftOrRight)

	numChToAnalyze := m.numChannels
	if goodSignalInLeftOrRight {
		numChToAnalyze = 2
	}

	m.blockCounter++
	m.updateEnergies(x, numChToAnalyze)

	strongestCh := 0
	for channel := range numChToAnalyze {
		if m.cumulativeEnergies[channel] > m.cumulativeEnergies[strongestCh] {
			strongestCh = channel
		}
	}

	if (goodSignalInLeftOrRight && m.selectedChannel > 1) ||
		m.cumulativeEnergies[strongestCh] > 2*m.cumulativeEnergies[m.selectedChannel] {
		m.selectedChannel = strongestCh
	}

	return m.selectedChannel
}

// updateEnergies accumulates the energy of the first numChToAnalyze
// channels, then smooths it once enough blocks are seen.
func (m *alignmentMixer) updateEnergies(x *block, numChToAnalyze int) {
	const numBlocksBeforeEnergySmoothing = 60 * kNumBlocksPerSecond
	for channel := range numChToAnalyze {
		var x2Sum float32
		for _, sample := range x.view(0, channel) {
			x2Sum += sample * sample
		}

		if channel < 2 && x2Sum > m.excitationEnergyThreshold {
			m.strongBlockCounters[channel]++
		}

		if m.blockCounter <= numBlocksBeforeEnergySmoothing {
			m.cumulativeEnergies[channel] += x2Sum
		} else {
			const smoothing = 1.0 / (10 * kNumBlocksPerSecond)
			m.cumulativeEnergies[channel] += smoothing * (x2Sum - m.cumulativeEnergies[channel])
		}
	}

	// Normalize the energies to allow the energy computations to from now
	// be based on smoothing.
	if m.blockCounter == numBlocksBeforeEnergySmoothing {
		const oneByNumBlocksBeforeEnergySmoothing = 1.0 / numBlocksBeforeEnergySmoothing
		for channel := range numChToAnalyze {
			m.cumulativeEnergies[channel] *= oneByNumBlocksBeforeEnergySmoothing
		}
	}
}
