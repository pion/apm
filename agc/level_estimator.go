// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

// fixedDigitalLevelEstimator: smooth envelope over a 10 ms frame split into
// 20 sub-frames, instant attack, exponential decay (kDecayMs = 20).
// Mirrors fixed_digital_level_estimator.{h,cc}.
type fixedDigitalLevelEstimator struct {
	filterStateLevel  float32
	samplesInFrame    int
	samplesInSubFrame int
}

const (
	attackFilterConstant float32 = 0.0
	decayFilterConstant  float32 = 0.9971259
)

func newLevelEstimator(samplesPerChannel int) *fixedDigitalLevelEstimator {
	e := &fixedDigitalLevelEstimator{}
	e.setSamplesPerChannel(samplesPerChannel)

	return e
}

func (e *fixedDigitalLevelEstimator) setSamplesPerChannel(s int) {
	e.samplesInFrame = s
	e.samplesInSubFrame = s / kSubFramesInFrame
}

func (e *fixedDigitalLevelEstimator) reset() { e.filterStateLevel = 0 }

// computeLevel returns one envelope value per sub-frame (20 values).
func (e *fixedDigitalLevelEstimator) computeLevel(channels [][]float32, envelope *[kSubFramesInFrame]float32) {
	*envelope = [kSubFramesInFrame]float32{}
	for _, ch := range channels {
		for sf := range kSubFramesInFrame {
			base := sf * e.samplesInSubFrame
			for i := 0; i < e.samplesInSubFrame; i++ {
				v := ch[base+i]
				if v < 0 {
					v = -v
				}
				if v > envelope[sf] {
					envelope[sf] = v
				}
			}
		}
	}

	for sf := range kSubFramesInFrame - 1 {
		if envelope[sf] < envelope[sf+1] {
			envelope[sf] = envelope[sf+1]
		}
	}

	for sf := range kSubFramesInFrame {
		v := envelope[sf]
		if v > e.filterStateLevel {
			envelope[sf] = v*(1-attackFilterConstant) + e.filterStateLevel*attackFilterConstant
		} else {
			envelope[sf] = v*(1-decayFilterConstant) + e.filterStateLevel*decayFilterConstant
		}
		e.filterStateLevel = envelope[sf]
	}
}
