// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	stationarityMinNoisePower           = 10.0
	stationarityHangoverBlocks          = kNumBlocksPerSecond / 20
	stationarityBlocksAverageInitPhase  = 20
	stationarityBlocksInitialPhase      = kNumBlocksPerSecond * 2
	stationarityWindowLength            = 13
	stationarityThreshold               = 10.0
	stationarityBlockStationaryFraction = 0.75
)

// noiseSpectrum mirrors StationarityEstimator::NoiseSpectrum.
type noiseSpectrum struct {
	noiseSpectrum [kFftLengthBy2Plus1]float32
	blockCounter  int
}

// reset mirrors Reset: it resets the noise power spectrum estimate.
func (n *noiseSpectrum) reset() {
	n.blockCounter = 0
	fill(n.noiseSpectrum[:], stationarityMinNoisePower)
}

// update mirrors Update: it updates the noise power spectrum with a new
// frame.
func (n *noiseSpectrum) update(spectrum [][kFftLengthBy2Plus1]float32) {
	avgSpectrum := &spectrum[0]
	if len(spectrum) > 1 {
		// For multiple channels, average the channel spectra. Like
		// upstream, the first bin is the one of the first channel.
		avgSpectrumData := spectrum[0]
		for ch := 1; ch < len(spectrum); ch++ {
			for k := 1; k < kFftLengthBy2Plus1; k++ {
				avgSpectrumData[k] += spectrum[ch][k]
			}
		}
		oneByNumChannels := 1 / float32(len(spectrum))
		for k := 1; k < kFftLengthBy2Plus1; k++ {
			avgSpectrumData[k] *= oneByNumChannels
		}
		avgSpectrum = &avgSpectrumData
	}

	n.blockCounter++
	alpha := n.getAlpha()
	for k := range n.noiseSpectrum {
		if n.blockCounter <= stationarityBlocksAverageInitPhase {
			n.noiseSpectrum[k] += (1.0 / stationarityBlocksAverageInitPhase) * avgSpectrum[k]
		} else {
			n.noiseSpectrum[k] = n.updateBandBySmoothing(avgSpectrum[k], n.noiseSpectrum[k], alpha)
		}
	}
}

// power mirrors Power: the noise power spectrum at a band.
func (n *noiseSpectrum) power(band int) float32 { return n.noiseSpectrum[band] }

// getAlpha mirrors GetAlpha: the update coefficient for the current
// frame.
func (n *noiseSpectrum) getAlpha() float32 {
	// Variables, so that the tilt is computed in single precision.
	alpha, alphaInit := float32(0.004), float32(0.04)
	tiltAlpha := (alphaInit - alpha) / stationarityBlocksInitialPhase
	if n.blockCounter > stationarityBlocksInitialPhase+stationarityBlocksAverageInitPhase {
		return alpha
	}

	return alphaInit - tiltAlpha*float32(n.blockCounter-stationarityBlocksAverageInitPhase)
}

// updateBandBySmoothing mirrors UpdateBandBySmoothing: it updates the
// noise power spectrum at a band with a new frame.
func (n *noiseSpectrum) updateBandBySmoothing(powerBand, powerBandNoise, alpha float32) float32 {
	powerBandNoiseUpdated := powerBandNoise
	if powerBandNoise < powerBand {
		alphaInc := alpha * (powerBandNoise / powerBand)
		if n.blockCounter > stationarityBlocksInitialPhase && 10*powerBandNoise < powerBand {
			alphaInc *= 0.1
		}
		powerBandNoiseUpdated += alphaInc * (powerBand - powerBandNoise)
	} else {
		powerBandNoiseUpdated += alpha * (powerBand - powerBandNoise)
		powerBandNoiseUpdated = max(powerBandNoiseUpdated, stationarityMinNoisePower)
	}

	return powerBandNoiseUpdated
}

// stationarityEstimator mirrors StationarityEstimator: it tells which
// bands of the render signal are stationary.
type stationarityEstimator struct {
	noise             noiseSpectrum
	hangovers         [kFftLengthBy2Plus1]int
	stationarityFlags [kFftLengthBy2Plus1]bool
}

func newStationarityEstimator() stationarityEstimator {
	var estimator stationarityEstimator
	estimator.reset()

	return estimator
}

// reset mirrors Reset.
func (s *stationarityEstimator) reset() {
	s.noise.reset()
	s.hangovers = [kFftLengthBy2Plus1]int{}
	s.stationarityFlags = [kFftLengthBy2Plus1]bool{}
}

// updateNoiseEstimator mirrors UpdateNoiseEstimator: it only updates the
// noise estimator, which is useful until the delay is known.
func (s *stationarityEstimator) updateNoiseEstimator(spectrum [][kFftLengthBy2Plus1]float32) {
	s.noise.update(spectrum)
}

// updateStationarityFlags mirrors UpdateStationarityFlags: it updates the
// flags that tell whether the current frame is stationary. For a more
// robust estimation, it looks at future and past frames.
func (s *stationarityEstimator) updateStationarityFlags(
	spectrumBuf *spectrumBuffer,
	renderReverbContributionSpectrum *[kFftLengthBy2Plus1]float32,
	idxCurrent, numLookahead int,
) {
	numLookaheadBounded := min(numLookahead, stationarityWindowLength-1)
	idx := idxCurrent
	if numLookaheadBounded < stationarityWindowLength-1 {
		numLookback := (stationarityWindowLength - 1) - numLookaheadBounded
		idx = spectrumBuf.offsetIndex(idxCurrent, numLookback)
	}

	// The power of each band is accumulated over several consecutive
	// spectra. Their indexes are computed once for all the bands.
	var indexes [stationarityWindowLength]int
	indexes[0] = idx
	for k := 1; k < len(indexes); k++ {
		indexes[k] = spectrumBuf.decIndex(indexes[k-1])
	}

	for k := range s.stationarityFlags {
		s.stationarityFlags[k] = s.estimateBandStationarity(
			spectrumBuf, renderReverbContributionSpectrum, &indexes, k)
	}
	s.updateHangover()
	s.smoothStationaryPerFreq()
}

// isBandStationary mirrors IsBandStationary.
func (s *stationarityEstimator) isBandStationary(band int) bool {
	return s.stationarityFlags[band] && s.hangovers[band] == 0
}

// isBlockStationary mirrors IsBlockStationary: whether the current block
// is estimated as stationary.
func (s *stationarityEstimator) isBlockStationary() bool {
	var acumStationarity float32
	for band := range s.stationarityFlags {
		if s.isBandStationary(band) {
			acumStationarity++
		}
	}

	return acumStationarity*(1.0/kFftLengthBy2Plus1) > stationarityBlockStationaryFraction
}

// estimateBandStationarity mirrors EstimateBandStationarity: an
// estimation of the stationarity of a band from the past, present and
// future data.
func (s *stationarityEstimator) estimateBandStationarity(
	spectrumBuf *spectrumBuffer,
	averageReverb *[kFftLengthBy2Plus1]float32,
	indexes *[stationarityWindowLength]int,
	band int,
) bool {
	var acumPower float32
	oneByNumChannels := 1 / float32(len(spectrumBuf.buffer[0]))
	for _, idx := range indexes {
		for ch := range spectrumBuf.buffer[idx] {
			acumPower += spectrumBuf.buffer[idx][ch][band] * oneByNumChannels
		}
	}
	acumPower += averageReverb[band]
	noise := stationarityWindowLength * s.noise.power(band)

	return acumPower < stationarityThreshold*noise
}

// areAllBandsStationary mirrors AreAllBandsStationary.
func (s *stationarityEstimator) areAllBandsStationary() bool {
	for _, flag := range s.stationarityFlags {
		if !flag {
			return false
		}
	}

	return true
}

// updateHangover mirrors UpdateHangover: it updates the hangover
// depending on the stationarity of the current frame.
func (s *stationarityEstimator) updateHangover() {
	reduceHangover := s.areAllBandsStationary()
	for k, flag := range s.stationarityFlags {
		if !flag {
			s.hangovers[k] = stationarityHangoverBlocks
		} else if reduceHangover {
			s.hangovers[k] = max(s.hangovers[k]-1, 0)
		}
	}
}

// smoothStationaryPerFreq mirrors SmoothStationaryPerFreq: it smooths the
// stationarity detection by looking at the neighboring bands.
func (s *stationarityEstimator) smoothStationaryPerFreq() {
	var allAheadStationarySmooth [kFftLengthBy2Plus1]bool
	for k := 1; k < kFftLengthBy2Plus1-1; k++ {
		allAheadStationarySmooth[k] = s.stationarityFlags[k-1] && s.stationarityFlags[k] && s.stationarityFlags[k+1]
	}

	allAheadStationarySmooth[0] = allAheadStationarySmooth[1]
	allAheadStationarySmooth[kFftLengthBy2Plus1-1] = allAheadStationarySmooth[kFftLengthBy2Plus1-2]

	s.stationarityFlags = allAheadStationarySmooth
}
