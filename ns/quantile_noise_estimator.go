// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "math"

const kSimult = 3

type quantileNoiseEstimator struct {
	density     [kSimult * fftSizeBy2Plus1]float32
	logQuantile [kSimult * fftSizeBy2Plus1]float32
	quantile    [fftSizeBy2Plus1]float32
	counter     [kSimult]int
	numUpdates  int
}

func newQuantileNoiseEstimator() *quantileNoiseEstimator {
	q := &quantileNoiseEstimator{numUpdates: 1}
	for i := range q.density {
		q.density[i] = 0.3
	}
	for i := range q.logQuantile {
		q.logQuantile[i] = 8.0
	}
	const oneBySimult = 1 / float32(kSimult)
	for i := range kSimult {
		q.counter[i] = int(math.Floor(float64(longStartupPhaseBlocks * (float32(i) + 1) * oneBySimult)))
	}

	return q
}

//nolint:cyclop // Mirrors upstream QuantileNoiseEstimator::Estimate.
func (q *quantileNoiseEstimator) estimate(signalSpectrum *[fftSizeBy2Plus1]float32, noiseSpectrum *[fftSizeBy2Plus1]float32) {
	var logSpectrum [fftSizeBy2Plus1]float32
	logApproxSpan(signalSpectrum[:], logSpectrum[:])

	quantileIndexToReturn := -1
	for sim, offset := 0, 0; sim < kSimult; sim, offset = sim+1, offset+fftSizeBy2Plus1 {
		oneByCounterPlus1 := 1.0 / float32(q.counter[sim]+1)
		for i, idx := 0, offset; i < fftSizeBy2Plus1; i, idx = i+1, idx+1 {
			var delta float32 = 40.0
			if q.density[idx] > 1.0 {
				delta = 40.0 / q.density[idx]
			}
			multiplier := delta * oneByCounterPlus1
			if logSpectrum[i] > q.logQuantile[idx] {
				q.logQuantile[idx] += 0.25 * multiplier
			} else {
				q.logQuantile[idx] -= 0.75 * multiplier
			}

			const kWidth = float32(0.01)
			const kOneByWidthPlus2 = 1.0 / (2.0 * kWidth)
			d := logSpectrum[i] - q.logQuantile[idx]
			if d < 0 {
				d = -d
			}
			if d < kWidth {
				q.density[idx] = (float32(q.counter[sim])*q.density[idx] + kOneByWidthPlus2) * oneByCounterPlus1
			}
		}

		if q.counter[sim] >= longStartupPhaseBlocks {
			q.counter[sim] = 0
			if q.numUpdates >= longStartupPhaseBlocks {
				quantileIndexToReturn = offset
			}
		}
		q.counter[sim]++
	}

	if q.numUpdates < longStartupPhaseBlocks {
		quantileIndexToReturn = fftSizeBy2Plus1 * (kSimult - 1)
		q.numUpdates++
	}

	if quantileIndexToReturn >= 0 {
		expApproxSpan(q.logQuantile[quantileIndexToReturn:quantileIndexToReturn+fftSizeBy2Plus1], q.quantile[:])
	}

	*noiseSpectrum = q.quantile
}
