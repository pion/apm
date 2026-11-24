// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const erleBlocksForOnsetDetection = erleBlocksToHold + 150

// setMaxErleBands matches SetMaxErleBands.
func setMaxErleBands(maxErleL, maxErleH float32) [kFftLengthBy2Plus1]float32 {
	var maxErle [kFftLengthBy2Plus1]float32
	fill(maxErle[:kFftLengthBy2/2], maxErleL)
	fill(maxErle[kFftLengthBy2/2:], maxErleH)

	return maxErle
}

// clamp32 limits x to [low, high].
func clamp32(x, low, high float32) float32 {
	return min(max(x, low), high)
}

// updateErleBand moves the ERLE of a band towards a new value.
func updateErleBand(erle *float32, newErle float32, lowRenderEnergy bool, minErle, maxErle float32) {
	alpha := float32(0.05)
	if newErle < *erle {
		alpha = 0.1
		if lowRenderEnergy {
			alpha = 0
		}
	}
	*erle = clamp32(*erle+alpha*(newErle-*erle), minErle, maxErle)
}

// accumulatedSpectra mirrors SubbandErleEstimator::AccumulatedSpectra.
type accumulatedSpectra struct {
	y2              [][kFftLengthBy2Plus1]float32
	e2              [][kFftLengthBy2Plus1]float32
	lowRenderEnergy [][kFftLengthBy2Plus1]bool
	numPoints       []int
}

// subbandErleEstimator mirrors SubbandErleEstimator: it estimates the
// echo return loss enhancement for each frequency subband.
type subbandErleEstimator struct {
	useOnsetDetection bool
	minErle           float32
	maxErle           [kFftLengthBy2Plus1]float32
	// useMinErleDuringOnsets is false upstream when the field trial
	// WebRTC-Aec3MinErleDuringOnsetsKillSwitch is enabled.
	useMinErleDuringOnsets bool
	accumSpectra           accumulatedSpectra
	// erle is the ERLE without special handling of render onsets.
	erle [][kFftLengthBy2Plus1]float32
	// erleOnsetCompensated is the ERLE lowered during render onsets.
	erleOnsetCompensated [][kFftLengthBy2Plus1]float32
	erleUnbounded        [][kFftLengthBy2Plus1]float32
	// erleDuringOnsets is the estimation of the ERLE during render onsets.
	erleDuringOnsets [][kFftLengthBy2Plus1]float32
	comingOnset      [][kFftLengthBy2Plus1]bool
	holdCounters     [][kFftLengthBy2Plus1]int
}

func newSubbandErleEstimator(config echoCanceller3Config, numCaptureChannels int) *subbandErleEstimator {
	estimator := &subbandErleEstimator{
		useOnsetDetection: config.erle.onsetDetection,
		minErle:           config.erle.min,
		maxErle:           setMaxErleBands(config.erle.maxL, config.erle.maxH),

		useMinErleDuringOnsets: true,
		accumSpectra: accumulatedSpectra{
			y2:              make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
			e2:              make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
			lowRenderEnergy: make([][kFftLengthBy2Plus1]bool, numCaptureChannels),
			numPoints:       make([]int, numCaptureChannels),
		},
		erle:                 make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		erleOnsetCompensated: make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		erleUnbounded:        make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		erleDuringOnsets:     make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		comingOnset:          make([][kFftLengthBy2Plus1]bool, numCaptureChannels),
		holdCounters:         make([][kFftLengthBy2Plus1]int, numCaptureChannels),
	}
	estimator.reset()

	return estimator
}

// reset mirrors Reset.
func (s *subbandErleEstimator) reset() {
	for ch := range s.erle {
		fill(s.erle[ch][:], s.minErle)
		fill(s.erleOnsetCompensated[ch][:], s.minErle)
		fill(s.erleUnbounded[ch][:], s.minErle)
		fill(s.erleDuringOnsets[ch][:], s.minErle)
		for k := range s.comingOnset[ch] {
			s.comingOnset[ch][k] = true
		}
		s.holdCounters[ch] = [kFftLengthBy2Plus1]int{}
	}
	s.resetAccumulatedSpectra()
}

// update mirrors Update.
func (s *subbandErleEstimator) update(
	x2 *[kFftLengthBy2Plus1]float32, y2, e2 [][kFftLengthBy2Plus1]float32, convergedFilters []bool,
) {
	s.updateAccumulatedSpectra(x2, y2, e2, convergedFilters)
	s.updateBands(convergedFilters)

	if s.useOnsetDetection {
		s.decreaseErlePerBandForLowRenderSignals()
	}

	for ch := range s.erle {
		for _, erle := range []*[kFftLengthBy2Plus1]float32{
			&s.erle[ch], &s.erleOnsetCompensated[ch], &s.erleUnbounded[ch],
		} {
			erle[0] = erle[1]
			erle[kFftLengthBy2] = erle[kFftLengthBy2-1]
		}
	}
}

// getErle mirrors Erle: the ERLE estimate.
func (s *subbandErleEstimator) getErle(onsetCompensated bool) [][kFftLengthBy2Plus1]float32 {
	if onsetCompensated && s.useOnsetDetection {
		return s.erleOnsetCompensated
	}

	return s.erle
}

// getErleUnbounded mirrors ErleUnbounded: the ERLE estimate that is not
// capped.
func (s *subbandErleEstimator) getErleUnbounded() [][kFftLengthBy2Plus1]float32 {
	return s.erleUnbounded
}

// getErleDuringOnsets mirrors ErleDuringOnsets: the ERLE estimate at
// onsets.
func (s *subbandErleEstimator) getErleDuringOnsets() [][kFftLengthBy2Plus1]float32 {
	return s.erleDuringOnsets
}

func (s *subbandErleEstimator) updateBands(convergedFilters []bool) {
	for ch := range s.accumSpectra.y2 {
		// The converged filter flag already imposes a minimum on the ERLE
		// that can be estimated, as it is false when the filter performs
		// poorly.
		if !convergedFilters[ch] || s.accumSpectra.numPoints[ch] != erlePointsToAccumulate {
			continue
		}
		s.updateChannelBands(ch)
	}
}

func (s *subbandErleEstimator) updateChannelBands(ch int) {
	for band := 1; band < kFftLengthBy2; band++ {
		if s.accumSpectra.e2[ch][band] > 0 {
			s.updateBand(ch, band, s.accumSpectra.y2[ch][band]/s.accumSpectra.e2[ch][band])
		}
	}
}

func (s *subbandErleEstimator) updateBand(ch, band int, newErle float32) {
	// The virtually unbounded ERLE.
	const unboundedErleMax = 100000.0
	lowRenderEnergy := s.accumSpectra.lowRenderEnergy[ch][band]

	if s.useOnsetDetection && !lowRenderEnergy {
		s.updateOnset(ch, band, newErle)
	}

	updateErleBand(&s.erle[ch][band], newErle, lowRenderEnergy, s.minErle, s.maxErle[band])
	if s.useOnsetDetection {
		updateErleBand(&s.erleOnsetCompensated[ch][band], newErle, lowRenderEnergy, s.minErle, s.maxErle[band])
	}
	updateErleBand(&s.erleUnbounded[ch][band], newErle, lowRenderEnergy, s.minErle, unboundedErleMax)
}

// updateOnset tracks the ERLE at the first update of a band after an
// onset of the render signal.
func (s *subbandErleEstimator) updateOnset(ch, k int, newErle float32) {
	if s.comingOnset[ch][k] {
		s.comingOnset[ch][k] = false
		if !s.useMinErleDuringOnsets {
			duringOnsets := &s.erleDuringOnsets[ch][k]
			alpha := float32(0.15)
			if newErle < *duringOnsets {
				alpha = 0.3
			}
			*duringOnsets = clamp32(*duringOnsets+alpha*(newErle-*duringOnsets), s.minErle, s.maxErle[k])
		}
	}
	s.holdCounters[ch][k] = erleBlocksForOnsetDetection
}

func (s *subbandErleEstimator) decreaseErlePerBandForLowRenderSignals() {
	for ch := range s.accumSpectra.y2 {
		for k := 1; k < kFftLengthBy2; k++ {
			s.holdCounters[ch][k]--
			if s.holdCounters[ch][k] > erleBlocksForOnsetDetection-erleBlocksToHold {
				continue
			}
			if s.erleOnsetCompensated[ch][k] > s.erleDuringOnsets[ch][k] {
				s.erleOnsetCompensated[ch][k] = max(s.erleDuringOnsets[ch][k], 0.97*s.erleOnsetCompensated[ch][k])
			}
			if s.holdCounters[ch][k] <= 0 {
				s.comingOnset[ch][k] = true
				s.holdCounters[ch][k] = 0
			}
		}
	}
}

func (s *subbandErleEstimator) resetAccumulatedSpectra() {
	for ch := range s.erleDuringOnsets {
		s.resetChannelAccumulatedSpectra(ch)
	}
}

func (s *subbandErleEstimator) resetChannelAccumulatedSpectra(ch int) {
	s.accumSpectra.y2[ch] = [kFftLengthBy2Plus1]float32{}
	s.accumSpectra.e2[ch] = [kFftLengthBy2Plus1]float32{}
	s.accumSpectra.numPoints[ch] = 0
	s.accumSpectra.lowRenderEnergy[ch] = [kFftLengthBy2Plus1]bool{}
}

func (s *subbandErleEstimator) updateAccumulatedSpectra(
	x2 *[kFftLengthBy2Plus1]float32, y2, e2 [][kFftLengthBy2Plus1]float32, convergedFilters []bool,
) {
	st := &s.accumSpectra
	for ch := range y2 {
		// See updateBands for the use of the converged filter flag.
		if !convergedFilters[ch] {
			continue
		}

		if st.numPoints[ch] == erlePointsToAccumulate {
			s.resetChannelAccumulatedSpectra(ch)
		}

		for k := range x2 {
			st.y2[ch][k] += y2[ch][k]
			st.e2[ch][k] += e2[ch][k]
			st.lowRenderEnergy[ch][k] = st.lowRenderEnergy[ch][k] || x2[k] < erleX2BandEnergyThreshold
		}

		st.numPoints[ch]++
	}
}
