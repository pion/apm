// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// kSubbands mirrors SignalDependentErleEstimator::kSubbands.
const kSubbands = 6

// erleBandBoundaries mirrors kBandBoundaries.
func erleBandBoundaries() [kSubbands + 1]int {
	return [kSubbands + 1]int{1, 8, 16, 24, 32, 48, kFftLengthBy2Plus1}
}

// formSubbandMap matches FormSubbandMap.
func formSubbandMap() [kFftLengthBy2Plus1]int {
	boundaries := erleBandBoundaries()
	var mapBandToSubband [kFftLengthBy2Plus1]int
	subband := 1
	for k := range mapBandToSubband {
		if k >= boundaries[subband] {
			subband++
		}
		mapBandToSubband[k] = subband - 1 //nolint:gosec // G602 false positive on a range loop.
	}

	return mapBandToSubband
}

// defineFilterSectionSizes matches DefineFilterSectionSizes: the size in
// blocks of the sections that divide the linear filter. The lower
// sections, which typically represent the direct path, have a larger
// resolution than the higher ones, which represent more reverberant
// acoustic paths.
func defineFilterSectionSizes(delayHeadroomBlocks, numBlocks, numSections int) []int {
	filterLengthBlocks := numBlocks - delayHeadroomBlocks
	sectionSizes := make([]int, numSections)
	remainingBlocks := filterLengthBlocks
	remainingSections := numSections
	estimatorSize := 2
	idx := 0
	for remainingSections > 1 && remainingBlocks > estimatorSize*remainingSections {
		sectionSizes[idx] = estimatorSize
		remainingBlocks -= estimatorSize
		remainingSections--
		estimatorSize *= 2
		idx++
	}

	lastGroupsSize := remainingBlocks / remainingSections
	for ; idx < numSections; idx++ {
		sectionSizes[idx] = lastGroupsSize
	}
	sectionSizes[numSections-1] += remainingBlocks - lastGroupsSize*remainingSections

	return sectionSizes
}

// setSectionsBoundaries matches SetSectionsBoundaries: the limits in
// blocks of each filter section.
func setSectionsBoundaries(delayHeadroomBlocks, numBlocks, numSections int) []int {
	estimatorBoundariesBlocks := make([]int, numSections+1)
	if len(estimatorBoundariesBlocks) == 2 {
		estimatorBoundariesBlocks[1] = numBlocks

		return estimatorBoundariesBlocks
	}
	sectionSizes := defineFilterSectionSizes(delayHeadroomBlocks, numBlocks, numSections)

	idx := 0
	currentSizeBlock := 0
	estimatorBoundariesBlocks[0] = delayHeadroomBlocks
	for k := delayHeadroomBlocks; k < numBlocks; k++ {
		currentSizeBlock++
		if currentSizeBlock >= sectionSizes[idx] {
			idx++
			if idx == len(sectionSizes) {
				break
			}
			estimatorBoundariesBlocks[idx] = k + 1
			currentSizeBlock = 0
		}
	}
	estimatorBoundariesBlocks[len(sectionSizes)] = numBlocks

	return estimatorBoundariesBlocks
}

// setMaxErleSubbands matches SetMaxErleSubbands.
func setMaxErleSubbands(maxErleL, maxErleH float32, limitSubbandL int) [kSubbands]float32 {
	var maxErle [kSubbands]float32
	fill(maxErle[:limitSubbandL], maxErleL)
	fill(maxErle[limitSubbandL:], maxErleH)

	return maxErle
}

// subbandPowers sums a power spectrum over each subband.
func subbandPowers(powerSpectrum *[kFftLengthBy2Plus1]float32) [kSubbands]float32 {
	boundaries := erleBandBoundaries()
	var powers [kSubbands]float32
	for subband := range powers {
		powers[subband] = sum(powerSpectrum[boundaries[subband]:boundaries[subband+1]]) //nolint:gosec // G602 false positive on a range loop.
	}

	return powers
}

// signalDependentErleEstimator mirrors SignalDependentErleEstimator: it
// estimates the dependency of the ERLE on the input signal. By looking at
// the input signal, it estimates whether the current echo estimate is due
// to the direct path or to a more reverberant one, and refines the
// average ERLE that it receives accordingly.
type signalDependentErleEstimator struct {
	minErle                 float32
	numSections             int
	bandToSubband           [kFftLengthBy2Plus1]int
	maxErle                 [kSubbands]float32
	sectionBoundariesBlocks []int
	useOnsetDetection       bool
	erle                    [][kFftLengthBy2Plus1]float32
	erleOnsetCompensated    [][kFftLengthBy2Plus1]float32
	s2SectionAccum          [][][kFftLengthBy2Plus1]float32
	erleEstimators          [][][kSubbands]float32
	erleRef                 [][kSubbands]float32
	correctionFactors       [][][kSubbands]float32
	numUpdates              [][kSubbands]int
	nActiveSections         [][kFftLengthBy2Plus1]int
}

func newSignalDependentErleEstimator(
	config echoCanceller3Config, numCaptureChannels int,
) *signalDependentErleEstimator {
	numSections := config.erle.numSections
	bandToSubband := formSubbandMap()
	estimator := &signalDependentErleEstimator{
		minErle:       config.erle.min,
		numSections:   numSections,
		bandToSubband: bandToSubband,
		maxErle:       setMaxErleSubbands(config.erle.maxL, config.erle.maxH, bandToSubband[kFftLengthBy2/2]),
		sectionBoundariesBlocks: setSectionsBoundaries(
			config.delay.delayHeadroomSamples/kBlockSize, config.filter.refined.lengthBlocks, numSections),
		useOnsetDetection:    config.erle.onsetDetection,
		erle:                 make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		erleOnsetCompensated: make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		s2SectionAccum:       make([][][kFftLengthBy2Plus1]float32, numCaptureChannels),
		erleEstimators:       make([][][kSubbands]float32, numCaptureChannels),
		erleRef:              make([][kSubbands]float32, numCaptureChannels),
		correctionFactors:    make([][][kSubbands]float32, numCaptureChannels),
		numUpdates:           make([][kSubbands]int, numCaptureChannels),
		nActiveSections:      make([][kFftLengthBy2Plus1]int, numCaptureChannels),
	}
	for ch := range numCaptureChannels {
		estimator.s2SectionAccum[ch] = make([][kFftLengthBy2Plus1]float32, numSections)
		estimator.erleEstimators[ch] = make([][kSubbands]float32, numSections)
		estimator.correctionFactors[ch] = make([][kSubbands]float32, numSections)
	}
	estimator.reset()

	return estimator
}

// reset mirrors Reset.
func (s *signalDependentErleEstimator) reset() {
	for ch := range s.erle {
		fill(s.erle[ch][:], s.minErle)
		fill(s.erleOnsetCompensated[ch][:], s.minErle)
		for section := range s.erleEstimators[ch] {
			fill(s.erleEstimators[ch][section][:], s.minErle)
			fill(s.correctionFactors[ch][section][:], 1)
		}
		fill(s.erleRef[ch][:], s.minErle)
		s.numUpdates[ch] = [kSubbands]int{}
		s.nActiveSections[ch] = [kFftLengthBy2Plus1]int{}
	}
}

// getErle mirrors Erle: the ERLE per frequency band.
func (s *signalDependentErleEstimator) getErle(onsetCompensated bool) [][kFftLengthBy2Plus1]float32 {
	if onsetCompensated && s.useOnsetDetection {
		return s.erleOnsetCompensated
	}

	return s.erle
}

// update mirrors Update. The ERLE passed as an input must be an estimate
// of the average ERLE achieved by the linear filter.
//
// It estimates the number of sections of the linear filter that hold
// most of the energy of the echo estimate. Based on that number, it
// corrects the input ERLE.
func (s *signalDependentErleEstimator) update(
	render *renderBuffer,
	filterFrequencyResponses [][][kFftLengthBy2Plus1]float32,
	x2 *[kFftLengthBy2Plus1]float32,
	y2, e2, averageErle, averageErleOnsetCompensated [][kFftLengthBy2Plus1]float32,
	convergedFilters []bool,
) {
	// The number of filter sections that are needed for achieving 90% of
	// the power spectrum energy of the echo estimate.
	s.computeEchoEstimatePerFilterSection(render, filterFrequencyResponses)
	s.computeActiveFilterSections()

	// The correction factors adapt the ERLE to the characteristics of the
	// input signal.
	s.updateCorrectionFactors(x2, y2, e2, convergedFilters)

	for ch := range s.erle {
		for k := range kFftLengthBy2 {
			subband := s.bandToSubband[k]
			correctionFactor := s.correctionFactors[ch][s.nActiveSections[ch][k]][subband]
			s.erle[ch][k] = clamp32(averageErle[ch][k]*correctionFactor, s.minErle, s.maxErle[subband])
			if s.useOnsetDetection {
				s.erleOnsetCompensated[ch][k] = clamp32(
					averageErleOnsetCompensated[ch][k]*correctionFactor, s.minErle, s.maxErle[subband])
			}
		}
	}
}

func (s *signalDependentErleEstimator) updateCorrectionFactors(
	x2 *[kFftLengthBy2Plus1]float32, y2, e2 [][kFftLengthBy2Plus1]float32, convergedFilters []bool,
) {
	for ch, converged := range convergedFilters {
		if converged {
			s.updateChannelCorrectionFactors(ch, subbandPowers(x2), subbandPowers(&y2[ch]), subbandPowers(&e2[ch]))
		}
	}
}

// smoothErle moves an ERLE estimate towards a new value, when there is
// one.
func (s *signalDependentErleEstimator) smoothErle(erle *float32, newErle float32, isUpdated bool, subband int) {
	const (
		smthConstantDecreases = 0.1
		smthConstantIncreases = smthConstantDecreases / 2.0
	)
	alpha := float32(smthConstantDecreases)
	if newErle > *erle {
		alpha = smthConstantIncreases
	}
	if !isUpdated {
		alpha = 0
	}
	*erle += alpha * (newErle - *erle)
	*erle = clamp32(*erle, s.minErle, s.maxErle[subband])
}

func (s *signalDependentErleEstimator) updateChannelCorrectionFactors(
	ch int, x2Subbands, y2Subbands, e2Subbands [kSubbands]float32,
) {
	const numUpdateThr = 50
	boundaries := erleBandBoundaries()

	for subband := range kSubbands {
		// The number of active sections of a subband is the minimum over
		// its bands: if the direct path is the main contributor to the
		// echo estimate of one band, it is taken as the main contributor
		// for the subband. That number identifies the ERLE estimator to
		// update.
		idx := s.nActiveSections[ch][boundaries[subband]]
		for _, active := range s.nActiveSections[ch][boundaries[subband]:boundaries[subband+1]] {
			idx = min(idx, active)
		}

		var newErle float32
		isErleUpdated := x2Subbands[subband] > erleX2BandEnergyThreshold && e2Subbands[subband] > 0
		if isErleUpdated {
			newErle = y2Subbands[subband] / e2Subbands[subband]
			s.numUpdates[ch][subband]++
		}

		s.smoothErle(&s.erleEstimators[ch][idx][subband], newErle, isErleUpdated, subband)
		s.smoothErle(&s.erleRef[ch][subband], newErle, isErleUpdated, subband)

		if isErleUpdated && s.numUpdates[ch][subband] > numUpdateThr {
			// The ratio between the ERLE that is updated using all the
			// points and the ERLE that is updated only on signals that
			// share the same number of active filter sections.
			newCorrectionFactor := s.erleEstimators[ch][idx][subband] / s.erleRef[ch][subband]
			s.correctionFactors[ch][idx][subband] += 0.1 * (newCorrectionFactor - s.correctionFactors[ch][idx][subband])
		}
	}
}

// computeEchoEstimatePerFilterSection approximates the power spectrum of
// the echo estimate if the filter were limited to a number of sections.
func (s *signalDependentErleEstimator) computeEchoEstimatePerFilterSection(
	render *renderBuffer, filterFrequencyResponses [][][kFftLengthBy2Plus1]float32,
) {
	spectrumRenderBuffer := render.getSpectrumBuffer()
	numRenderChannels := len(spectrumRenderBuffer.buffer[0])
	oneByNumRenderChannels := 1 / float32(numRenderChannels)

	for captureCh := range s.s2SectionAccum {
		idxRender := spectrumRenderBuffer.offsetIndex(render.position(), s.sectionBoundariesBlocks[0])

		for section := range s.numSections {
			var x2Section, h2Section [kFftLengthBy2Plus1]float32
			blockLimit := min(s.sectionBoundariesBlocks[section+1], len(filterFrequencyResponses[captureCh]))
			for blk := s.sectionBoundariesBlocks[section]; blk < blockLimit; blk++ {
				for _, spectrum := range spectrumRenderBuffer.buffer[idxRender] {
					for k := range x2Section {
						x2Section[k] += spectrum[k] * oneByNumRenderChannels //nolint:gosec // G602 false positive on a range loop.
					}
				}
				for k := range h2Section {
					h2Section[k] += filterFrequencyResponses[captureCh][blk][k] //nolint:gosec // G602 false positive on a range loop.
				}
				idxRender = spectrumRenderBuffer.incIndex(idxRender)
			}

			for k := range x2Section {
				s.s2SectionAccum[captureCh][section][k] = x2Section[k] * h2Section[k] //nolint:gosec // G602 false positive on a range loop.
			}
		}

		for section := 1; section < s.numSections; section++ {
			for k := range s.s2SectionAccum[captureCh][section] {
				s.s2SectionAccum[captureCh][section][k] += s.s2SectionAccum[captureCh][section-1][k]
			}
		}
	}
}

// computeActiveFilterSections computes, for each band, the smallest
// number of sections that hold 90% of the energy of the echo estimate.
func (s *signalDependentErleEstimator) computeActiveFilterSections() {
	for ch := range s.nActiveSections {
		s.nActiveSections[ch] = [kFftLengthBy2Plus1]int{}
		for k := range kFftLengthBy2Plus1 {
			section := s.numSections
			target := 0.9 * s.s2SectionAccum[ch][s.numSections-1][k]
			for section > 0 && s.s2SectionAccum[ch][section-1][k] >= target {
				section--
				s.nActiveSections[ch][k] = section
			}
		}
	}
}
