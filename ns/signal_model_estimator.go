// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

type signalModelEstimator struct {
	diffNormalization        float32
	signalEnergySum          float32
	histograms               histograms
	histogramAnalysisCounter int
	priorEstimator           priorSignalModelEstimator
	features                 signalModel
}

func newSignalModelEstimator() *signalModelEstimator {
	return &signalModelEstimator{
		histogramAnalysisCounter: 500,
		priorEstimator:           newPriorSignalModelEstimator(ltrFeatureThr),
		features:                 newSignalModel(),
	}
}

func (s *signalModelEstimator) adjustNormalization(numAnalyzedFrames int32, signalEnergy float32) {
	s.diffNormalization *= float32(numAnalyzedFrames)
	s.diffNormalization += signalEnergy
	s.diffNormalization /= float32(numAnalyzedFrames + 1)
}

func (s *signalModelEstimator) update(
	priorSnr, postSnr, conservativeNoiseSpectrum, signalSpectrum *[fftSizeBy2Plus1]float32,
	signalSpectralSum, signalEnergy float32,
) {
	updateSpectralFlatness(signalSpectrum, signalSpectralSum, &s.features.spectralFlatness)

	specDiff := computeSpectralDiff(conservativeNoiseSpectrum, signalSpectrum, signalSpectralSum, s.diffNormalization)
	s.features.spectralDiff += 0.3 * (specDiff - s.features.spectralDiff)

	s.signalEnergySum += signalEnergy

	s.histogramAnalysisCounter--
	if s.histogramAnalysisCounter > 0 {
		s.histograms.update(&s.features)
	} else {
		s.priorEstimator.update(&s.histograms)
		s.histograms.clear()
		s.histogramAnalysisCounter = featureUpdateWindowSize
		s.signalEnergySum /= float32(featureUpdateWindowSize)
		s.diffNormalization = 0.5 * (s.signalEnergySum + s.diffNormalization)
		s.signalEnergySum = 0
	}

	updateSpectralLrt(priorSnr, postSnr, &s.features.avgLogLrt, &s.features.lrt)
}

const oneByFftSizeBy2Plus1 = 1.0 / float32(fftSizeBy2Plus1)

func computeSpectralDiff(
	conservativeNoiseSpectrum, signalSpectrum *[fftSizeBy2Plus1]float32,
	signalSpectralSum, diffNormalization float32,
) float32 {
	var noiseAverage float32
	for i := range fftSizeBy2Plus1 {
		noiseAverage += conservativeNoiseSpectrum[i]
	}
	noiseAverage *= oneByFftSizeBy2Plus1
	signalAverage := signalSpectralSum * oneByFftSizeBy2Plus1

	var covariance, noiseVariance, signalVariance float32
	for i := range fftSizeBy2Plus1 {
		signalDiff := signalSpectrum[i] - signalAverage
		noiseDiff := conservativeNoiseSpectrum[i] - noiseAverage
		covariance += signalDiff * noiseDiff
		noiseVariance += noiseDiff * noiseDiff
		signalVariance += signalDiff * signalDiff
	}
	covariance *= oneByFftSizeBy2Plus1
	noiseVariance *= oneByFftSizeBy2Plus1
	signalVariance *= oneByFftSizeBy2Plus1

	specDiff := signalVariance - (covariance*covariance)/(noiseVariance+0.0001)

	return specDiff / (diffNormalization + 0.0001)
}

func updateSpectralFlatness(signalSpectrum *[fftSizeBy2Plus1]float32, signalSpectralSum float32, spectralFlatness *float32) {
	const kAveraging = float32(0.3)
	var avgSpectFlatnessNum float32

	for i := 1; i < fftSizeBy2Plus1; i++ {
		if signalSpectrum[i] == 0 {
			*spectralFlatness -= kAveraging * (*spectralFlatness)

			return
		}
	}

	for i := 1; i < fftSizeBy2Plus1; i++ {
		avgSpectFlatnessNum += logApprox(signalSpectrum[i])
	}

	avgSpectFlatnessDenom := signalSpectralSum - signalSpectrum[0]
	avgSpectFlatnessDenom *= oneByFftSizeBy2Plus1
	avgSpectFlatnessNum *= oneByFftSizeBy2Plus1

	specTmp := expApprox(avgSpectFlatnessNum) / avgSpectFlatnessDenom

	*spectralFlatness += kAveraging * (specTmp - *spectralFlatness)
}

func updateSpectralLrt(priorSnr, postSnr *[fftSizeBy2Plus1]float32, avgLogLrt *[fftSizeBy2Plus1]float32, lrt *float32) {
	for i := range fftSizeBy2Plus1 {
		tmp1 := 1.0 + 2.0*priorSnr[i]
		tmp2 := 2.0 * priorSnr[i] / (tmp1 + 0.0001)
		besselTmp := (postSnr[i] + 1.0) * tmp2
		avgLogLrt[i] += 0.5 * (besselTmp - logApprox(tmp1) - avgLogLrt[i])
	}

	var sum float32
	for i := range fftSizeBy2Plus1 {
		sum += avgLogLrt[i]
	}
	*lrt = sum * oneByFftSizeBy2Plus1
}
