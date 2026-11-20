// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const narrowBandCounterThreshold = 5

// identifySmallNarrowBandRegions matches IdentifySmallNarrowBandRegions:
// it identifies local bands with narrow characteristics.
func identifySmallNarrowBandRegions(
	render *renderBuffer, delayPartitions int, hasDelayPartitions bool, narrowBandCounters *[kFftLengthBy2 - 1]int,
) {
	if !hasDelayPartitions {
		*narrowBandCounters = [kFftLengthBy2 - 1]int{}

		return
	}

	var channelCounters [kFftLengthBy2 - 1]int
	for _, x2 := range render.spectrum(delayPartitions) {
		for k := 1; k < kFftLengthBy2; k++ {
			if x2[k] > 3*max(x2[k-1], x2[k+1]) {
				channelCounters[k-1]++
			}
		}
	}
	for k := range narrowBandCounters {
		if channelCounters[k] > 0 {
			narrowBandCounters[k]++
		} else {
			narrowBandCounters[k] = 0
		}
	}
}

// renderSignalAnalyzer mirrors RenderSignalAnalyzer: it analyzes the
// properties of the render signal for the filter adaptation.
type renderSignalAnalyzer struct {
	strongPeakFreezeDuration int
	narrowBandCounters       [kFftLengthBy2 - 1]int
	narrowPeakBand           int
	hasNarrowPeakBand        bool
	narrowPeakCounter        int
}

func newRenderSignalAnalyzer(config echoCanceller3Config) *renderSignalAnalyzer {
	return &renderSignalAnalyzer{strongPeakFreezeDuration: config.filter.refined.lengthBlocks}
}

// update mirrors Update: it updates the render signal analysis with the
// most recent render signal.
//
//nolint:unparam // The delay comes from AecState, which is not ported yet.
func (r *renderSignalAnalyzer) update(render *renderBuffer, delayPartitions int, hasDelayPartitions bool) {
	// Identify bands of narrow nature.
	identifySmallNarrowBandRegions(render, delayPartitions, hasDelayPartitions, &r.narrowBandCounters)

	// Identify the presence of a strong narrow band.
	r.identifyStrongNarrowBandComponent(render)
}

// identifyStrongNarrowBandComponent matches
// IdentifyStrongNarrowBandComponent: it identifies whether the signal has
// a single strong narrow-band component.
func (r *renderSignalAnalyzer) identifyStrongNarrowBandComponent(render *renderBuffer) {
	if r.hasNarrowPeakBand {
		r.narrowPeakCounter++
		if r.narrowPeakCounter > r.strongPeakFreezeDuration {
			r.hasNarrowPeakBand = false
		}
	}

	latest := render.getBlock(0)
	var maxPeakLevel float32
	for channel := range latest.numChannels {
		x2Latest := &render.spectrum(0)[channel]
		peakBin, nonPeakPower := spectralPeak(x2Latest)

		// Assess the render signal strength.
		strength := maxAbs(latest.view(0, channel))
		if latest.numBands > 1 {
			strength = max(strength, maxAbs(latest.view(1, channel)))
		}

		// Detect whether the spectral peak has a strong narrowband nature,
		// and store the strongest peak across channels.
		peakLevel := x2Latest[peakBin]
		if peakBin > 0 && strength > 100 && peakLevel > 100*nonPeakPower && peakLevel > maxPeakLevel {
			maxPeakLevel = peakLevel
			r.narrowPeakBand, r.hasNarrowPeakBand = peakBin, true
			r.narrowPeakCounter = 0
		}
	}
}

// spectralPeak identifies the spectral peak and computes the level around
// it.
func spectralPeak(x2 *[kFftLengthBy2Plus1]float32) (peakBin int, nonPeakPower float32) {
	for k, value := range x2 {
		if value > x2[peakBin] {
			peakBin = k
		}
	}
	for k := max(0, peakBin-14); k < peakBin-4; k++ {
		nonPeakPower = max(x2[k], nonPeakPower)
	}
	for k := peakBin + 5; k < min(peakBin+15, kFftLengthBy2Plus1); k++ {
		nonPeakPower = max(x2[k], nonPeakPower)
	}

	return peakBin, nonPeakPower
}

// poorSignalExcitation mirrors PoorSignalExcitation: true if the render
// signal is poorly exciting.
func (r *renderSignalAnalyzer) poorSignalExcitation() bool {
	for _, counter := range r.narrowBandCounters {
		if counter > 10 {
			return true
		}
	}

	return false
}

// maskRegionsAroundNarrowBands mirrors MaskRegionsAroundNarrowBands: it
// zeroes the array around regions with narrow band signal
// characteristics.
func (r *renderSignalAnalyzer) maskRegionsAroundNarrowBands(v *[kFftLengthBy2Plus1]float32) {
	if r.narrowBandCounters[0] > narrowBandCounterThreshold {
		v[1], v[0] = 0, 0
	}
	for k := 2; k < kFftLengthBy2-1; k++ {
		if r.narrowBandCounters[k-1] > narrowBandCounterThreshold {
			v[k-2], v[k-1], v[k], v[k+1], v[k+2] = 0, 0, 0, 0, 0
		}
	}
	if r.narrowBandCounters[kFftLengthBy2-2] > narrowBandCounterThreshold {
		v[kFftLengthBy2], v[kFftLengthBy2-1] = 0, 0
	}
}

// getNarrowPeakBand mirrors NarrowPeakBand.
func (r *renderSignalAnalyzer) getNarrowPeakBand() (int, bool) {
	return r.narrowPeakBand, r.hasNarrowPeakBand
}
