// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

type signalModel struct {
	lrt              float32
	spectralDiff     float32
	spectralFlatness float32
	avgLogLrt        [fftSizeBy2Plus1]float32
}

func newSignalModel() signalModel {
	const kSfFeatureThr = float32(0.5)
	s := signalModel{
		lrt:              ltrFeatureThr,
		spectralFlatness: kSfFeatureThr,
		spectralDiff:     kSfFeatureThr,
	}
	for i := range s.avgLogLrt {
		s.avgLogLrt[i] = ltrFeatureThr
	}

	return s
}
