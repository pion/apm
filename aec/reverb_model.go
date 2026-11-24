// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// reverbModel mirrors ReverbModel: it estimates the reverberant power of
// a signal, as a power spectrum.
type reverbModel struct {
	reverb [kFftLengthBy2Plus1]float32
}

// reset mirrors Reset: it resets the state.
func (m *reverbModel) reset() {
	m.reverb = [kFftLengthBy2Plus1]float32{}
}

// getReverb mirrors reverb: the reverb power spectrum.
func (m *reverbModel) getReverb() *[kFftLengthBy2Plus1]float32 { return &m.reverb }

// updateReverbNoFreqShaping mirrors UpdateReverbNoFreqShaping: it scales
// the input power spectrum with a constant value and updates the reverb.
func (m *reverbModel) updateReverbNoFreqShaping(powerSpectrum []float32, powerSpectrumScaling, reverbDecay float32) {
	if reverbDecay <= 0 {
		return
	}
	for k, power := range powerSpectrum {
		m.reverb[k] = (m.reverb[k] + power*powerSpectrumScaling) * reverbDecay
	}
}

// updateReverb mirrors UpdateReverb: it scales the input power spectrum
// with a value per bin and updates the reverb.
func (m *reverbModel) updateReverb(powerSpectrum, powerSpectrumScaling []float32, reverbDecay float32) {
	if reverbDecay <= 0 {
		return
	}
	for k, power := range powerSpectrum {
		m.reverb[k] = (m.reverb[k] + power*powerSpectrumScaling[k]) * reverbDecay
	}
}
