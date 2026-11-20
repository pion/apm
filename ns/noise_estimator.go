// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "math"

// logTable[i] = ln(i) for i in 0..128. Verbatim from upstream
// noise_estimator.cc.
var logTable = [129]float32{ //nolint:gochecknoglobals // Read-only upstream table.
	0, 0, 0, 0, 0, 1.609438, 1.791759,
	1.945910, 2.079442, 2.197225, float32(math.Ln10), 2.397895, 2.484907,
	2.564949,
	2.639057, 2.708050, 2.772589, 2.833213, 2.890372, 2.944439, 2.995732,
	3.044522, 3.091043, 3.135494, 3.178054, 3.218876, 3.258097, 3.295837,
	3.332205, 3.367296, 3.401197, 3.433987, 3.465736, 3.496507, 3.526361,
	3.555348, 3.583519, 3.610918, 3.637586, 3.663562, 3.688879, 3.713572,
	3.737669, 3.761200, 3.784190, 3.806663, 3.828641, 3.850147, 3.871201,
	3.891820, 3.912023, 3.931826, 3.951244, 3.970292, 3.988984, 4.007333,
	4.025352, 4.043051, 4.060443, 4.077538, 4.094345, 4.110874, 4.127134,
	4.143135, 4.158883, 4.174387, 4.189655, 4.204693, 4.219508, 4.234107,
	4.248495, 4.262680, 4.276666, 4.290460, 4.304065, 4.317488, 4.330733,
	4.343805, 4.356709, 4.369448, 4.382027, 4.394449, 4.406719, 4.418841,
	4.430817, 4.442651, 4.454347, 4.465908, 4.477337, 4.488636, 4.499810,
	4.510859, 4.521789, 4.532599, 4.543295, 4.553877, 4.564348, 4.574711,
	4.584968, 4.595119, 4.605170, 4.615121, 4.624973, 4.634729, 4.644391,
	4.653960, 4.663439, 4.672829, 4.682131, 4.691348, 4.700480, 4.709530,
	4.718499, 4.727388, 4.736198, 4.744932, 4.753591, 4.762174, 4.770685,
	4.779124, 4.787492, 4.795791, 4.804021, 4.812184, 4.820282, 4.828314,
	4.836282, 4.844187, 4.852030,
}

type noiseEstimator struct {
	sp               *suppressionParams
	whiteNoiseLevel  float32
	pinkNoiseNum     float32
	pinkNoiseExp     float32
	prevNoiseSpec    [fftSizeBy2Plus1]float32
	conservativeSpec [fftSizeBy2Plus1]float32
	parametricSpec   [fftSizeBy2Plus1]float32
	noiseSpec        [fftSizeBy2Plus1]float32
	quantile         *quantileNoiseEstimator
}

func newNoiseEstimator(sp *suppressionParams) *noiseEstimator {
	return &noiseEstimator{
		sp:       sp,
		quantile: newQuantileNoiseEstimator(),
	}
}

func (n *noiseEstimator) prepareAnalysis() {
	n.prevNoiseSpec = n.noiseSpec
}

//nolint:cyclop // Mirrors upstream NoiseEstimator::PreUpdate.
func (n *noiseEstimator) preUpdate(numAnalyzedFrames int32, signalSpectrum *[fftSizeBy2Plus1]float32, signalSpectralSum float32) {
	n.quantile.estimate(signalSpectrum, &n.noiseSpec)

	if numAnalyzedFrames >= shortStartupPhaseBlocks {
		return
	}
	const kStartBand = 5
	var sumLogILogMagn, sumLogI, sumLogISq, sumLogMagn float32
	for i := kStartBand; i < fftSizeBy2Plus1; i++ {
		logI := logTable[i]
		sumLogI += logI
		sumLogISq += logI * logI
		ls := logApprox(signalSpectrum[i])
		sumLogMagn += ls
		sumLogILogMagn += logI * ls
	}

	const kOneByFftSizeBy2Plus1 = 1.0 / float32(fftSizeBy2Plus1)
	n.whiteNoiseLevel += signalSpectralSum * kOneByFftSizeBy2Plus1 * n.sp.overSubtractionFactor

	denom := sumLogISq*float32(fftSizeBy2Plus1-kStartBand) - sumLogI*sumLogI
	num := sumLogISq*sumLogMagn - sumLogI*sumLogILogMagn
	pinkNoiseAdjustment := num / denom
	if pinkNoiseAdjustment < 0 {
		pinkNoiseAdjustment = 0
	}
	n.pinkNoiseNum += pinkNoiseAdjustment

	num = sumLogI*sumLogMagn - float32(fftSizeBy2Plus1-kStartBand)*sumLogILogMagn
	pinkNoiseAdjustment = num / denom
	if pinkNoiseAdjustment < 0 {
		pinkNoiseAdjustment = 0
	}
	if pinkNoiseAdjustment > 1 {
		pinkNoiseAdjustment = 1
	}
	n.pinkNoiseExp += pinkNoiseAdjustment

	oneByNumAnalyzedFramesPlus1 := 1.0 / (float32(numAnalyzedFrames) + 1.0)

	var parametricExp, parametricNum float32
	if n.pinkNoiseExp > 0 {
		parametricNum = expApprox(n.pinkNoiseNum * oneByNumAnalyzedFramesPlus1)
		parametricNum *= float32(numAnalyzedFrames) + 1.0
		parametricExp = n.pinkNoiseExp * oneByNumAnalyzedFramesPlus1
	}

	const kOneByShortStartupPhaseBlocks = 1.0 / float32(shortStartupPhaseBlocks)
	for i := range fftSizeBy2Plus1 {
		if n.pinkNoiseExp == 0 {
			n.parametricSpec[i] = n.whiteNoiseLevel
		} else {
			useBand := float32(i)
			if i < kStartBand {
				useBand = float32(kStartBand)
			}
			parametricDenom := powApprox(useBand, parametricExp)
			n.parametricSpec[i] = parametricNum / parametricDenom
		}
	}

	for i := range fftSizeBy2Plus1 {
		n.noiseSpec[i] *= float32(numAnalyzedFrames)
		tmp := n.parametricSpec[i] * float32(shortStartupPhaseBlocks-int(numAnalyzedFrames))
		n.noiseSpec[i] += tmp * oneByNumAnalyzedFramesPlus1
		n.noiseSpec[i] *= kOneByShortStartupPhaseBlocks
	}
}

func (n *noiseEstimator) postUpdate(speechProb, signalSpectrum *[fftSizeBy2Plus1]float32) {
	const kNoiseUpdate = float32(0.9)
	gamma := kNoiseUpdate
	for i := range fftSizeBy2Plus1 {
		probSpeech := speechProb[i]
		probNonSpeech := 1.0 - probSpeech

		noiseUpdateTmp := gamma*n.prevNoiseSpec[i] +
			(1.0-gamma)*(probNonSpeech*signalSpectrum[i]+probSpeech*n.prevNoiseSpec[i])

		gammaOld := gamma
		const kProbRange = float32(0.2)
		if probSpeech > kProbRange {
			gamma = 0.99
		} else {
			gamma = kNoiseUpdate
		}

		if probSpeech < kProbRange {
			n.conservativeSpec[i] += 0.05 * (signalSpectrum[i] - n.conservativeSpec[i])
		}

		if gamma == gammaOld {
			n.noiseSpec[i] = noiseUpdateTmp
		} else {
			n.noiseSpec[i] = gamma*n.prevNoiseSpec[i] +
				(1.0-gamma)*(probNonSpeech*signalSpectrum[i]+probSpeech*n.prevNoiseSpec[i])
			if noiseUpdateTmp < n.noiseSpec[i] {
				n.noiseSpec[i] = noiseUpdateTmp
			}
		}
	}
}
