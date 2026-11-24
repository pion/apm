// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// subtractorOutput is the per-block result of running both the refined
// and coarse adaptive filters on a capture channel.
type subtractorOutput struct {
	sRefined       [kBlockSize]float32         // echo estimate (refined)
	sCoarse        [kBlockSize]float32         // echo estimate (coarse)
	eRefined       [kBlockSize]float32         // capture - refined echo estimate
	eCoarse        [kBlockSize]float32         // capture - coarse echo estimate
	ERefined       fftData                     // FFT of eRefined
	E2Refined      [kFftLengthBy2Plus1]float32 // |ERefined|²
	E2Coarse       [kFftLengthBy2Plus1]float32 // |ECoarse|²
	s2Refined      float32
	s2Coarse       float32
	e2Refined      float32
	e2Coarse       float32
	y2             float32
	sRefinedMaxAbs float32
	sCoarseMaxAbs  float32
}

// computeMetrics mirrors SubtractorOutput::ComputeMetrics: derives the
// scalar power summaries and max-abs values from the per-sample arrays.
func (s *subtractorOutput) computeMetrics(y []float32) {
	s.y2 = energy(y)
	s.e2Refined = energy(s.eRefined[:])
	s.e2Coarse = energy(s.eCoarse[:])
	s.s2Refined = energy(s.sRefined[:])
	s.s2Coarse = energy(s.sCoarse[:])
	s.sRefinedMaxAbs = maxAbs(s.sRefined[:])
	s.sCoarseMaxAbs = maxAbs(s.sCoarse[:])
}

func energy(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}

	return sum
}

func maxAbs(v []float32) float32 {
	var peak float32
	for _, x := range v {
		if a := abs32(x); a > peak {
			peak = a
		}
	}

	return peak
}
