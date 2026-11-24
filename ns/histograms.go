// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

const kHistogramSize = 1000

type histograms struct {
	lrt              [kHistogramSize]int
	spectralFlatness [kHistogramSize]int
	spectralDiff     [kHistogramSize]int
}

func (h *histograms) clear() {
	h.lrt = [kHistogramSize]int{}
	h.spectralFlatness = [kHistogramSize]int{}
	h.spectralDiff = [kHistogramSize]int{}
}

func (h *histograms) update(model *signalModel) {
	const oneByBinSizeLrt = 1.0 / binSizeLrt
	if model.lrt < kHistogramSize*binSizeLrt && model.lrt >= 0 {
		h.lrt[int(oneByBinSizeLrt*model.lrt)]++
	}

	const oneByBinSizeSpecFlat = 1.0 / binSizeSpecFlat
	if model.spectralFlatness < kHistogramSize*binSizeSpecFlat && model.spectralFlatness >= 0 {
		h.spectralFlatness[int(model.spectralFlatness*oneByBinSizeSpecFlat)]++
	}

	const oneByBinSizeSpecDiff = 1.0 / binSizeSpecDiff
	if model.spectralDiff < kHistogramSize*binSizeSpecDiff && model.spectralDiff >= 0 {
		h.spectralDiff[int(model.spectralDiff*oneByBinSizeSpecDiff)]++
	}
}
