// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

type priorSignalModel struct {
	lrt                   float32
	flatnessThreshold     float32
	templateDiffThreshold float32
	lrtWeighting          float32
	flatnessWeighting     float32
	differenceWeighting   float32
}

func newPriorSignalModel(lrtInitial float32) priorSignalModel {
	return priorSignalModel{
		lrt:                   lrtInitial,
		flatnessThreshold:     0.5,
		templateDiffThreshold: 0.5,
		lrtWeighting:          1.0,
		flatnessWeighting:     0.0,
		differenceWeighting:   0.0,
	}
}

type priorSignalModelEstimator struct {
	priorModel priorSignalModel
}

func newPriorSignalModelEstimator(lrtInitial float32) priorSignalModelEstimator {
	return priorSignalModelEstimator{priorModel: newPriorSignalModel(lrtInitial)}
}

//nolint:cyclop // Mirrors upstream PriorSignalModelEstimator::Update.
func (p *priorSignalModelEstimator) update(h *histograms) {
	var lowLrtFluctuations bool
	updateLrt(&h.lrt, &p.priorModel.lrt, &lowLrtFluctuations)

	var flatPeakPos float32
	var flatPeakW int
	findFirstOfTwoLargestPeaks(binSizeSpecFlat, &h.spectralFlatness, &flatPeakPos, &flatPeakW)

	var diffPeakPos float32
	var diffPeakW int
	findFirstOfTwoLargestPeaks(binSizeSpecDiff, &h.spectralDiff, &diffPeakPos, &diffPeakW)

	useSpecFlat := 1
	if float32(flatPeakW) < 0.3*500 || flatPeakPos < 0.6 {
		useSpecFlat = 0
	}
	useSpecDiff := 1
	if float32(diffPeakW) < 0.3*500 || lowLrtFluctuations {
		useSpecDiff = 0
	}

	p.priorModel.templateDiffThreshold = 1.2 * diffPeakPos
	if p.priorModel.templateDiffThreshold > 1 {
		p.priorModel.templateDiffThreshold = 1
	}
	if p.priorModel.templateDiffThreshold < 0.16 {
		p.priorModel.templateDiffThreshold = 0.16
	}

	oneByFeatureSum := 1.0 / float32(1+useSpecFlat+useSpecDiff)
	p.priorModel.lrtWeighting = oneByFeatureSum

	if useSpecFlat == 1 {
		p.priorModel.flatnessThreshold = 0.9 * flatPeakPos
		if p.priorModel.flatnessThreshold > 0.95 {
			p.priorModel.flatnessThreshold = 0.95
		}
		if p.priorModel.flatnessThreshold < 0.1 {
			p.priorModel.flatnessThreshold = 0.1
		}
		p.priorModel.flatnessWeighting = oneByFeatureSum
	} else {
		p.priorModel.flatnessWeighting = 0
	}

	if useSpecDiff == 1 {
		p.priorModel.differenceWeighting = oneByFeatureSum
	} else {
		p.priorModel.differenceWeighting = 0
	}
}

func findFirstOfTwoLargestPeaks(binSize float32, spectralFlatness *[kHistogramSize]int, peakPos *float32, peakWeight *int) {
	var peakValue, secondaryValue, secondaryWeight int
	var secondaryPos float32
	*peakPos = 0
	*peakWeight = 0

	for i := range kHistogramSize {
		binMid := (float32(i) + 0.5) * binSize
		if spectralFlatness[i] > peakValue {
			secondaryValue = peakValue
			secondaryWeight = *peakWeight
			secondaryPos = *peakPos
			peakValue = spectralFlatness[i]
			*peakWeight = spectralFlatness[i]
			*peakPos = binMid
		} else if spectralFlatness[i] > secondaryValue {
			secondaryValue = spectralFlatness[i]
			secondaryWeight = spectralFlatness[i]
			secondaryPos = binMid
		}
	}

	delta := secondaryPos - *peakPos
	if delta < 0 {
		delta = -delta
	}
	if delta < 2*binSize && float32(secondaryWeight) > 0.5*float32(*peakWeight) {
		*peakWeight += secondaryWeight
		*peakPos = 0.5 * (*peakPos + secondaryPos)
	}
}

func updateLrt(lrtHistogram *[kHistogramSize]int, priorModelLrt *float32, lowLrtFluctuations *bool) {
	var average, averageCompl, averageSquared float32
	count := 0

	for i := range 10 {
		binMid := (float32(i) + 0.5) * binSizeLrt
		average += float32(lrtHistogram[i]) * binMid
		count += lrtHistogram[i]
	}
	if count > 0 {
		average /= float32(count)
	}

	for i := range kHistogramSize {
		binMid := (float32(i) + 0.5) * binSizeLrt
		averageSquared += float32(lrtHistogram[i]) * binMid * binMid
		averageCompl += float32(lrtHistogram[i]) * binMid
	}
	const kOneFeatureUpdateWindowSize = 1.0 / float32(featureUpdateWindowSize)
	averageSquared *= kOneFeatureUpdateWindowSize
	averageCompl *= kOneFeatureUpdateWindowSize

	*lowLrtFluctuations = averageSquared-average*averageCompl < 0.05

	const kMaxLrt = float32(1.0)
	const kMinLrt = float32(0.2)
	if *lowLrtFluctuations {
		*priorModelLrt = kMaxLrt
	} else {
		v := 1.2 * average
		if v > kMaxLrt {
			v = kMaxLrt
		}
		if v < kMinLrt {
			v = kMinLrt
		}
		*priorModelLrt = v
	}
}
