// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	erleEpsilon               = 1e-3
	erleX2BandEnergyThreshold = 44015068.0
	erleBlocksToHold          = 100
	erlePointsToAccumulate    = 6
)

// optionalFloat32 stands for std::optional<float>.
type optionalFloat32 struct {
	value float32
	valid bool
}

// erleInstantaneous mirrors FullBandErleEstimator::ErleInstantaneous.
type erleInstantaneous struct {
	clampInstQualityToZero bool
	clampInstQualityToOne  bool
	erleLog2               optionalFloat32
	instQualityEstimate    float32
	maxErleLog2            float32
	minErleLog2            float32
	y2Acum                 float32
	e2Acum                 float32
	numPoints              int
}

func newErleInstantaneous(config erleConfig) erleInstantaneous {
	instantaneous := erleInstantaneous{
		clampInstQualityToZero: config.clampQualityEstimateToZero,
		clampInstQualityToOne:  config.clampQualityEstimateToOne,
	}
	instantaneous.reset()

	return instantaneous
}

// update mirrors Update: it adds a point and returns true if the
// instantaneous ERLE was updated.
func (e *erleInstantaneous) update(y2Sum, e2Sum float32) bool {
	updateEstimates := false
	e.e2Acum += e2Sum
	e.y2Acum += y2Sum
	e.numPoints++
	if e.numPoints == erlePointsToAccumulate {
		if e.e2Acum > 0 {
			updateEstimates = true
			e.erleLog2 = optionalFloat32{value: fastApproxLog2f(e.y2Acum/e.e2Acum + erleEpsilon), valid: true}
		}
		e.numPoints = 0
		e.e2Acum = 0
		e.y2Acum = 0
	}

	if updateEstimates {
		e.updateMaxMin()
		e.updateQualityEstimate()
	}

	return updateEstimates
}

// reset mirrors Reset.
func (e *erleInstantaneous) reset() {
	e.resetAccumulators()
	e.maxErleLog2 = -10 // -30 dB.
	e.minErleLog2 = 33  // 100 dB.
	e.instQualityEstimate = 0
}

// resetAccumulators mirrors ResetAccumulators.
func (e *erleInstantaneous) resetAccumulators() {
	e.erleLog2 = optionalFloat32{}
	e.instQualityEstimate = 0
	e.numPoints = 0
	e.e2Acum = 0
	e.y2Acum = 0
}

// getQualityEstimate mirrors GetQualityEstimate: an indication between 0
// and 1 of the performance of the linear filter.
func (e *erleInstantaneous) getQualityEstimate() optionalFloat32 {
	if !e.erleLog2.valid {
		return optionalFloat32{}
	}
	value := e.instQualityEstimate
	if e.clampInstQualityToZero {
		value = max(0, value)
	}
	if e.clampInstQualityToOne {
		value = min(1, value)
	}

	return optionalFloat32{value: value, valid: true}
}

func (e *erleInstantaneous) updateMaxMin() {
	// The forgetting factors are about 1 dB every 3 seconds.
	e.maxErleLog2 -= 0.0004
	e.maxErleLog2 = max(e.maxErleLog2, e.erleLog2.value)
	e.minErleLog2 += 0.0004
	e.minErleLog2 = min(e.minErleLog2, e.erleLog2.value)
}

func (e *erleInstantaneous) updateQualityEstimate() {
	const alpha = 0.07
	var qualityEstimate float32
	if e.maxErleLog2 > e.minErleLog2 {
		qualityEstimate = (e.erleLog2.value - e.minErleLog2) / (e.maxErleLog2 - e.minErleLog2)
	}
	if qualityEstimate > e.instQualityEstimate {
		e.instQualityEstimate = qualityEstimate
	} else {
		e.instQualityEstimate += alpha * (qualityEstimate - e.instQualityEstimate)
	}
}

// fullBandErleEstimator mirrors FullBandErleEstimator: it estimates the
// echo return loss enhancement using the energy of all the frequency
// bands.
type fullBandErleEstimator struct {
	minErleLog2                   float32
	holdCountersInstantaneousErle []int
	erleTimeDomainLog2            []float32
	instantaneousErle             []erleInstantaneous
	linearFiltersQualities        []optionalFloat32
}

func newFullBandErleEstimator(config erleConfig, numCaptureChannels int) *fullBandErleEstimator {
	estimator := &fullBandErleEstimator{
		minErleLog2:                   fastApproxLog2f(config.min + erleEpsilon),
		holdCountersInstantaneousErle: make([]int, numCaptureChannels),
		erleTimeDomainLog2:            make([]float32, numCaptureChannels),
		instantaneousErle:             make([]erleInstantaneous, numCaptureChannels),
		linearFiltersQualities:        make([]optionalFloat32, numCaptureChannels),
	}
	for ch := range estimator.instantaneousErle {
		estimator.instantaneousErle[ch] = newErleInstantaneous(config)
	}
	estimator.reset()

	return estimator
}

// reset mirrors Reset.
func (f *fullBandErleEstimator) reset() {
	for ch := range f.instantaneousErle {
		f.instantaneousErle[ch].reset()
	}
	f.updateQualityEstimates()
	fill(f.erleTimeDomainLog2, f.minErleLog2)
	clear(f.holdCountersInstantaneousErle)
}

// update mirrors Update.
func (f *fullBandErleEstimator) update(
	x2 []float32, y2, e2 [][kFftLengthBy2Plus1]float32, convergedFilters []bool,
) {
	for ch := range y2 {
		if convergedFilters[ch] {
			f.updateChannel(ch, x2, &y2[ch], &e2[ch])
		}
		f.holdCountersInstantaneousErle[ch]--
		if f.holdCountersInstantaneousErle[ch] == 0 {
			f.instantaneousErle[ch].resetAccumulators()
		}
	}

	f.updateQualityEstimates()
}

// updateChannel computes the fullband ERLE of a channel.
func (f *fullBandErleEstimator) updateChannel(ch int, x2 []float32, y2, e2 *[kFftLengthBy2Plus1]float32) {
	if sum(x2) <= erleX2BandEnergyThreshold*float32(len(x2)) {
		return
	}
	if !f.instantaneousErle[ch].update(sum(y2[:]), sum(e2[:])) {
		return
	}
	f.holdCountersInstantaneousErle[ch] = erleBlocksToHold
	f.erleTimeDomainLog2[ch] += 0.05 * (f.instantaneousErle[ch].erleLog2.value - f.erleTimeDomainLog2[ch])
	f.erleTimeDomainLog2[ch] = max(f.erleTimeDomainLog2[ch], f.minErleLog2)
}

// fullbandErleLog2 mirrors FullbandErleLog2: the fullband ERLE estimate in
// log2 units.
func (f *fullBandErleEstimator) fullbandErleLog2() float32 {
	minErle := f.erleTimeDomainLog2[0]
	for _, erle := range f.erleTimeDomainLog2[1:] {
		minErle = min(minErle, erle)
	}

	return minErle
}

// getInstLinearQualityEstimates mirrors GetInstLinearQualityEstimates: an
// estimation of the current linear filter quality, between 0 and 1.
func (f *fullBandErleEstimator) getInstLinearQualityEstimates() []optionalFloat32 {
	return f.linearFiltersQualities
}

func (f *fullBandErleEstimator) updateQualityEstimates() {
	for ch := range f.instantaneousErle {
		f.linearFiltersQualities[ch] = f.instantaneousErle[ch].getQualityEstimate()
	}
}

// sum adds the values in order, like std::accumulate.
func sum(values []float32) float32 {
	var total float32
	for _, value := range values {
		total += value
	}

	return total
}
