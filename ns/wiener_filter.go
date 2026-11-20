// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

type wienerFilter struct {
	sp                      *suppressionParams
	spectrumPrevProcess     [fftSizeBy2Plus1]float32
	initialSpectralEstimate [fftSizeBy2Plus1]float32
	filter                  [fftSizeBy2Plus1]float32
}

func newWienerFilter(sp *suppressionParams) *wienerFilter {
	w := &wienerFilter{sp: sp}
	for i := range w.filter {
		w.filter[i] = 1
	}

	return w
}

func (w *wienerFilter) update(
	numAnalyzedFrames int32,
	noiseSpectrum, prevNoiseSpectrum, parametricNoiseSpectrum, signalSpectrum *[fftSizeBy2Plus1]float32,
) {
	for i := range fftSizeBy2Plus1 {
		prevTsa := w.spectrumPrevProcess[i] / (prevNoiseSpectrum[i] + 0.0001) * w.filter[i]

		var currentTsa float32
		if signalSpectrum[i] > noiseSpectrum[i] {
			currentTsa = signalSpectrum[i]/(noiseSpectrum[i]+0.0001) - 1.0
		}

		snrPrior := 0.98*prevTsa + (1.0-0.98)*currentTsa
		f := snrPrior / (w.sp.overSubtractionFactor + snrPrior)
		if f > 1 {
			f = 1
		}
		if f < w.sp.minimumAttenuatingGain {
			f = w.sp.minimumAttenuatingGain
		}
		w.filter[i] = f
	}

	if numAnalyzedFrames < shortStartupPhaseBlocks {
		for i := range fftSizeBy2Plus1 {
			w.initialSpectralEstimate[i] += signalSpectrum[i]
			filterInitial := w.initialSpectralEstimate[i] -
				w.sp.overSubtractionFactor*parametricNoiseSpectrum[i]
			filterInitial /= w.initialSpectralEstimate[i] + 0.0001
			if filterInitial > 1 {
				filterInitial = 1
			}
			if filterInitial < w.sp.minimumAttenuatingGain {
				filterInitial = w.sp.minimumAttenuatingGain
			}

			const kOneByShort = 1.0 / float32(shortStartupPhaseBlocks)
			filterInitial *= float32(shortStartupPhaseBlocks - int(numAnalyzedFrames))
			w.filter[i] *= float32(numAnalyzedFrames)
			w.filter[i] += filterInitial
			w.filter[i] *= kOneByShort
		}
	}

	w.spectrumPrevProcess = *signalSpectrum
}

func (w *wienerFilter) computeOverallScalingFactor(numAnalyzedFrames int32, priorSpeechProb, energyBefore, energyAfter float32) float32 {
	if !w.sp.useAttenuationAdjustment || numAnalyzedFrames <= longStartupPhaseBlocks {
		return 1.0
	}

	gain := sqrtFastApprox(energyAfter / (energyBefore + 1.0))

	const kBLim = float32(0.5)
	scaleFactor1 := float32(1.0)
	if gain > kBLim {
		scaleFactor1 = 1.0 + 1.3*(gain-kBLim)
		if gain*scaleFactor1 > 1.0 {
			scaleFactor1 = 1.0 / gain
		}
	}

	scaleFactor2 := float32(1.0)
	if gain < kBLim {
		if gain < w.sp.minimumAttenuatingGain {
			gain = w.sp.minimumAttenuatingGain
		}
		scaleFactor2 = 1.0 - 0.3*(kBLim-gain)
	}

	return priorSpeechProb*scaleFactor1 + (1.0-priorSpeechProb)*scaleFactor2
}
