// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "math"

type speechProbabilityEstimator struct {
	modelEstimator  *signalModelEstimator
	priorSpeechProb float32
	speechProb      [fftSizeBy2Plus1]float32
}

func newSpeechProbabilityEstimator() *speechProbabilityEstimator {
	return &speechProbabilityEstimator{
		modelEstimator:  newSignalModelEstimator(),
		priorSpeechProb: 0.5,
	}
}

func (s *speechProbabilityEstimator) update(
	numAnalyzedFrames int32,
	priorSnr, postSnr, conservativeNoiseSpectrum, signalSpectrum *[fftSizeBy2Plus1]float32,
	signalSpectralSum, signalEnergy float32,
) {
	if numAnalyzedFrames < longStartupPhaseBlocks {
		s.modelEstimator.adjustNormalization(numAnalyzedFrames, signalEnergy)
	}
	s.modelEstimator.update(priorSnr, postSnr, conservativeNoiseSpectrum, signalSpectrum, signalSpectralSum, signalEnergy)

	model := &s.modelEstimator.features
	prior := &s.modelEstimator.priorEstimator.priorModel

	const kWidthPrior0 = float32(4.0)
	const kWidthPrior1 = 2.0 * kWidthPrior0

	widthPrior := kWidthPrior0
	if model.lrt < prior.lrt {
		widthPrior = kWidthPrior1
	}
	ind0 := 0.5 * (tanh32(widthPrior*(model.lrt-prior.lrt)) + 1)

	widthPrior = kWidthPrior0
	if model.spectralFlatness > prior.flatnessThreshold {
		widthPrior = kWidthPrior1
	}
	ind1 := 0.5 * (tanh32(widthPrior*(prior.flatnessThreshold-model.spectralFlatness)) + 1)

	widthPrior = kWidthPrior0
	if model.spectralDiff < prior.templateDiffThreshold {
		widthPrior = kWidthPrior1
	}
	ind2 := 0.5 * (tanh32(widthPrior*(model.spectralDiff-prior.templateDiffThreshold)) + 1)

	indPrior := prior.lrtWeighting*ind0 + prior.flatnessWeighting*ind1 + prior.differenceWeighting*ind2

	s.priorSpeechProb += 0.1 * (indPrior - s.priorSpeechProb)
	if s.priorSpeechProb > 1 {
		s.priorSpeechProb = 1
	}
	if s.priorSpeechProb < 0.01 {
		s.priorSpeechProb = 0.01
	}

	gainPrior := (1.0 - s.priorSpeechProb) / (s.priorSpeechProb + 0.0001)

	var invLrt [fftSizeBy2Plus1]float32
	expApproxSignFlipSpan(model.avgLogLrt[:], invLrt[:])
	for i := range fftSizeBy2Plus1 {
		s.speechProb[i] = 1.0 / (1.0 + gainPrior*invLrt[i])
	}
}

func tanh32(x float32) float32 { return float32(math.Tanh(float64(x))) }
