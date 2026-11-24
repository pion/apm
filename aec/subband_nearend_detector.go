// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// subbandNearendDetector mirrors SubbandNearendDetector: it flags the
// nearend state from the nearend power in two subbands.
type subbandNearendDetector struct {
	config                subbandNearendDetectionConfig
	nearendSmoothers      []movingAverageSpectrum
	oneOverSubbandLength1 float32
	oneOverSubbandLength2 float32
	nearendState          bool
}

func newSubbandNearendDetector(config suppressorConfig, numCaptureChannels int) *subbandNearendDetector {
	detector := &subbandNearendDetector{nearendSmoothers: make([]movingAverageSpectrum, numCaptureChannels)}
	for ch := range detector.nearendSmoothers {
		detector.nearendSmoothers[ch] = newMovingAverageSpectrum(
			kFftLengthBy2Plus1, config.subbandNearendDetection.nearendAverageBlocks)
	}
	detector.setConfig(config)

	return detector
}

func (s *subbandNearendDetector) isNearendState() bool { return s.nearendState }

func (s *subbandNearendDetector) update(
	nearendSpectrum, _, comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32, _ bool,
) {
	s.nearendState = false
	subband1 := s.config.subband1
	subband2 := s.config.subband2
	for ch := range s.nearendSmoothers {
		noise := &comfortNoiseSpectrum[ch]
		var nearend [kFftLengthBy2Plus1]float32
		s.nearendSmoothers[ch].average(nearendSpectrum[ch][:], nearend[:])

		// The noise power and the nearend power of the first region, then
		// the nearend power of the second region.
		noisePower := sum(noise[subband1.low:subband1.high+1]) * s.oneOverSubbandLength1
		nearendPowerSubband1 := sum(nearend[subband1.low:subband1.high+1]) * s.oneOverSubbandLength1
		nearendPowerSubband2 := sum(nearend[subband2.low:subband2.high+1]) * s.oneOverSubbandLength2

		// One channel is sufficient to trigger the nearend state.
		s.nearendState = s.nearendState ||
			(nearendPowerSubband1 < s.config.nearendThreshold*nearendPowerSubband2 &&
				nearendPowerSubband1 > s.config.snrThreshold*noisePower)
	}
}

func (s *subbandNearendDetector) setConfig(config suppressorConfig) {
	s.config = config.subbandNearendDetection
	s.oneOverSubbandLength1 = 1 / float32(s.config.subband1.high-s.config.subband1.low+1)
	s.oneOverSubbandLength2 = 1 / float32(s.config.subband2.high-s.config.subband2.low+1)
	for ch := range s.nearendSmoothers {
		s.nearendSmoothers[ch].updateMemoryLength(s.config.nearendAverageBlocks)
	}
}
