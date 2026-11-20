// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

const (
	kFftLengthBy2Log2 = 6

	earlyReverbMinSizeBlocks = 3
	reverbBlocksPerSection   = 6
	// The linear regression approach assumes a symmetric index around 0.
	earlyReverbFirstPointAtLinearRegressors = -0.5*reverbBlocksPerSection*kFftLengthBy2 + 0.5
)

// symmetricArithmetricSum matches SymmetricArithmetricSum: the arithmetic
// sum of 2 * sum_{i=0.5}^{(N-1)/2} i^2, calculated directly.
func symmetricArithmetricSum(n int) float32 {
	return float32(n) * (float32(n*n) - 1) * float32(1.0/12.0)
}

// blockEnergyPeak matches BlockEnergyPeak: the peak energy of a block of
// an impulse response.
func blockEnergyPeak(h []float32, peakBlock int) float32 {
	blk := h[peakBlock*kFftLengthBy2 : (peakBlock+1)*kFftLengthBy2]
	peakValue := blk[0]
	for _, value := range blk[1:] {
		if peakValue*peakValue < value*value {
			peakValue = value
		}
	}

	return peakValue * peakValue
}

// blockEnergyAverage matches BlockEnergyAverage: the average energy of a
// block of an impulse response.
func blockEnergyAverage(h []float32, blockIndex int) float32 {
	return energy(h[blockIndex*kFftLengthBy2:(blockIndex+1)*kFftLengthBy2]) * (1.0 / kFftLengthBy2)
}

// lateReverbLinearRegressor mirrors
// ReverbDecayEstimator::LateReverbLinearRegressor: it estimates the decay
// of the late reverb from the linear filter.
type lateReverbLinearRegressor struct {
	nz        float32
	nn        float32
	count     float32
	numPoints int
	n         int
}

// reset mirrors Reset: it resets the estimator to receive a number of
// data points.
func (l *lateReverbLinearRegressor) reset(numDataPoints int) {
	l.nz = 0
	l.nn = symmetricArithmetricSum(numDataPoints)
	// The linear regression approach assumes a symmetric index around 0.
	l.count = 0
	if numDataPoints > 0 {
		l.count = -float32(numDataPoints)*0.5 + 0.5
	}
	l.numPoints = numDataPoints
	l.n = 0
}

// accumulate mirrors Accumulate.
func (l *lateReverbLinearRegressor) accumulate(z float32) {
	l.nz += l.count * z
	l.count++
	l.n++
}

// estimate mirrors Estimate: the decay.
func (l *lateReverbLinearRegressor) estimate() float32 {
	if l.nn == 0 {
		return 0
	}

	return l.nz / l.nn
}

// estimateAvailable mirrors EstimateAvailable.
func (l *lateReverbLinearRegressor) estimateAvailable() bool {
	return l.n == l.numPoints && l.numPoints != 0
}

// earlyReverbLengthEstimator mirrors
// ReverbDecayEstimator::EarlyReverbLengthEstimator: it identifies the
// length of the early reverb from the linear filter. The impulse response
// is divided in sections and the tilt of each section is computed by a
// linear regressor.
type earlyReverbLengthEstimator struct {
	numeratorsSmooth    []float32
	numerators          []float32
	coefficientsCounter int
	blockCounter        int
	numSections         int
}

func newEarlyReverbLengthEstimator(maxBlocks int) earlyReverbLengthEstimator {
	return earlyReverbLengthEstimator{
		numeratorsSmooth: make([]float32, maxBlocks-reverbBlocksPerSection),
		numerators:       make([]float32, maxBlocks-reverbBlocksPerSection),
	}
}

// reset mirrors Reset.
func (e *earlyReverbLengthEstimator) reset() {
	e.coefficientsCounter = 0
	clear(e.numerators)
	e.blockCounter = 0
}

// accumulate mirrors Accumulate.
func (e *earlyReverbLengthEstimator) accumulate(value, smoothing float32) {
	// Each section is composed of reverbBlocksPerSection blocks and
	// overlaps with the next one in reverbBlocksPerSection - 1 blocks: the
	// first section covers the blocks [0:5], the second the blocks [1:6]
	// and so on. For each value, reverbBlocksPerSection sections need to
	// be updated.
	firstSectionIndex := max(e.blockCounter-reverbBlocksPerSection+1, 0)
	lastSectionIndex := min(e.blockCounter, len(e.numerators)-1)
	xValue := float32(e.coefficientsCounter) + earlyReverbFirstPointAtLinearRegressors
	valueToInc := kFftLengthBy2 * value
	valueToAdd := xValue*value + float32(e.blockCounter-lastSectionIndex)*valueToInc
	for section := lastSectionIndex; section >= firstSectionIndex; section-- {
		e.numerators[section] += valueToAdd
		valueToAdd += valueToInc
	}

	// If this was the last coefficient of the current block and the block
	// ends a section, update the numerator of the linear regressor of
	// that section.
	e.coefficientsCounter++
	if e.coefficientsCounter == kFftLengthBy2 {
		if e.blockCounter >= reverbBlocksPerSection-1 {
			section := e.blockCounter - (reverbBlocksPerSection - 1)
			e.numeratorsSmooth[section] += smoothing * (e.numerators[section] - e.numeratorsSmooth[section])
			e.numSections = section + 1
		}
		e.blockCounter++
		e.coefficientsCounter = 0
	}
}

// estimate mirrors Estimate: the size in blocks of the early reverb. The
// tilts of the sections are compared. As all the linear regressors share
// the same denominator, only their numerators are compared.
func (e *earlyReverbLengthEstimator) estimate() int {
	const numSectionsToAnalyze = 9
	nn := symmetricArithmetricSum(reverbBlocksPerSection * kFftLengthBy2)
	// numerator11 is the numerator of the linear regressor for a decay of
	// 1.1, which is not a decay: log2(1.1) * nn / kFftLengthBy2.
	numerator11 := float32(0.13750352374993502) * nn / kFftLengthBy2
	// log2(0.8) * nn / kFftLengthBy2.
	numerator08 := float32(-0.32192809488736229) * nn / kFftLengthBy2

	if e.numSections < numSectionsToAnalyze {
		return 0
	}

	// The portions of the impulse response whose energy is not decreasing
	// are considered to be part of the early reverberations. So are the
	// blocks where the energy is decreasing faster than at the end of the
	// impulse response. The estimation is limited to the first
	// numSectionsToAnalyze sections.
	minNumeratorTail := e.minNumeratorTail(numSectionsToAnalyze)
	earlyReverbSizeMinus1 := 0
	for k, numerator := range e.numeratorsSmooth[:numSectionsToAnalyze] {
		if numerator > numerator11 || (numerator < numerator08 && numerator < 0.9*minNumeratorTail) {
			earlyReverbSizeMinus1 = k
		}
	}

	if earlyReverbSizeMinus1 == 0 {
		return 0
	}

	return earlyReverbSizeMinus1 + 1
}

// minNumeratorTail returns the smallest numerator of the sections from
// first on. When there is no such section, upstream reads the numerator
// at first, which is past the end of the numerators when there are only
// first of them. That value is taken as 0 here.
func (e *earlyReverbLengthEstimator) minNumeratorTail(first int) float32 {
	if first >= len(e.numeratorsSmooth) {
		return 0
	}
	minNumerator := e.numeratorsSmooth[first]
	for _, numerator := range e.numeratorsSmooth[first:max(first, e.numSections)] {
		minNumerator = min(minNumerator, numerator)
	}

	return minNumerator
}

// reverbDecayEstimator mirrors ReverbDecayEstimator: it estimates the
// decay of the late reverb.
type reverbDecayEstimator struct {
	filterLengthBlocks            int
	filterLengthCoefficients      int
	useAdaptiveEchoDecay          bool
	lateReverbDecayEstimator      lateReverbLinearRegressor
	earlyReverbEstimator          earlyReverbLengthEstimator
	lateReverbStart               int
	lateReverbEnd                 int
	blockToAnalyze                int
	estimationRegionCandidateSize int
	estimationRegionIdentified    bool
	previousGains                 []float32
	decay                         float32
	mildDecay                     float32
	tailGain                      float32
	smoothingConstant             float32
}

func newReverbDecayEstimator(config echoCanceller3Config) *reverbDecayEstimator {
	lengthBlocks := config.filter.refined.lengthBlocks

	return &reverbDecayEstimator{
		filterLengthBlocks:       lengthBlocks,
		filterLengthCoefficients: getTimeDomainLength(lengthBlocks),
		useAdaptiveEchoDecay:     config.epStrength.defaultLen < 0,
		earlyReverbEstimator:     newEarlyReverbLengthEstimator(lengthBlocks - earlyReverbMinSizeBlocks),
		lateReverbStart:          earlyReverbMinSizeBlocks,
		lateReverbEnd:            earlyReverbMinSizeBlocks,
		previousGains:            make([]float32, lengthBlocks),
		decay:                    abs32(config.epStrength.defaultLen),
		mildDecay:                abs32(config.epStrength.nearendLen),
	}
}

// update mirrors Update: it updates the decay estimate.
func (r *reverbDecayEstimator) update(
	filter []float32, filterQuality optionalFloat32, filterDelayBlocks int, usableLinearFilter, stationarySignal bool,
) {
	if stationarySignal {
		return
	}

	estimationFeasible := filterDelayBlocks <= r.filterLengthBlocks-earlyReverbMinSizeBlocks-1 &&
		len(filter) == r.filterLengthCoefficients && filterDelayBlocks > 0 && usableLinearFilter
	if !estimationFeasible {
		r.resetDecayEstimation()

		return
	}

	if !r.useAdaptiveEchoDecay {
		return
	}

	var newSmoothing float32
	if filterQuality.valid {
		newSmoothing = filterQuality.value * 0.2
	}
	r.smoothingConstant = max(newSmoothing, r.smoothingConstant)
	if r.smoothingConstant == 0 {
		return
	}

	if r.blockToAnalyze < r.filterLengthBlocks {
		// Analyze the filter and accumulate data for reverb estimation.
		r.analyzeFilter(filter)
		r.blockToAnalyze++
	} else {
		// When the filter is fully analyzed, estimate the reverb decay and
		// reset the blockToAnalyze counter.
		r.estimateDecay(filter, filterDelayBlocks)
	}
}

// getDecay mirrors Decay: the decay for the exponential model, the
// default one or a milder one.
func (r *reverbDecayEstimator) getDecay(mild bool) float32 {
	if !r.useAdaptiveEchoDecay && mild {
		return r.mildDecay
	}

	return r.decay
}

func (r *reverbDecayEstimator) resetDecayEstimation() {
	r.earlyReverbEstimator.reset()
	r.lateReverbDecayEstimator.reset(0)
	r.blockToAnalyze = 0
	r.estimationRegionCandidateSize = 0
	r.estimationRegionIdentified = false
	r.smoothingConstant = 0
	r.lateReverbStart = 0
	r.lateReverbEnd = 0
}

func (r *reverbDecayEstimator) estimateDecay(filter []float32, peakBlock int) {
	const (
		maxDecay = 0.95 // About 1 s min RT60.
		minDecay = 0.02 // About 15 ms max RT60.
	)

	// Reset the block analysis counter.
	r.blockToAnalyze = min(peakBlock+earlyReverbMinSizeBlocks, r.filterLengthBlocks)

	// To estimate the reverb decay, the energy of the first filter section
	// must be substantially larger than the last. Also, the first filter
	// section energy must not deviate too much from the max peak.
	firstReverbGain := blockEnergyAverage(filter, r.blockToAnalyze)
	sizeBlocks := len(filter) >> kFftLengthBy2Log2
	r.tailGain = blockEnergyAverage(filter, sizeBlocks-1)
	peakEnergy := blockEnergyPeak(filter, peakBlock)
	sufficientReverbDecay := firstReverbGain > 4*r.tailGain
	validFilter := firstReverbGain > 2*r.tailGain && peakEnergy < 100

	// Estimate the size of the regions with early and late reflections.
	sizeEarlyReverb := r.earlyReverbEstimator.estimate()
	sizeLateReverb := max(r.estimationRegionCandidateSize-sizeEarlyReverb, 0)

	// Only update the reverb decay estimate if the size of the identified
	// late reverb is sufficiently large.
	if sizeLateReverb >= 5 {
		if validFilter && r.lateReverbDecayEstimator.estimateAvailable() {
			decay := float32(math.Pow(2, float64(r.lateReverbDecayEstimator.estimate()*kFftLengthBy2)))
			decay = max(0.97*r.decay, decay)
			decay = min(decay, maxDecay)
			decay = max(decay, minDecay)
			r.decay += r.smoothingConstant * (decay - r.decay)
		}

		// Update the length of the decay. There must be enough data in
		// order to estimate the decay rate.
		r.lateReverbDecayEstimator.reset(sizeLateReverb * kFftLengthBy2)
		r.lateReverbStart = peakBlock + earlyReverbMinSizeBlocks + sizeEarlyReverb
		r.lateReverbEnd = r.blockToAnalyze + r.estimationRegionCandidateSize - 1
	} else {
		r.lateReverbDecayEstimator.reset(0)
		r.lateReverbStart = 0
		r.lateReverbEnd = 0
	}

	// Reset the variables for the identification of the region for reverb
	// decay estimation.
	r.estimationRegionIdentified = !validFilter || !sufficientReverbDecay
	r.estimationRegionCandidateSize = 0

	// Stop the estimation of the decay until another good filter is
	// received.
	r.smoothingConstant = 0

	// Reset the early reflections detector.
	r.earlyReverbEstimator.reset()
}

func (r *reverbDecayEstimator) analyzeFilter(filter []float32) {
	h := filter[r.blockToAnalyze*kFftLengthBy2 : (r.blockToAnalyze+1)*kFftLengthBy2]

	// The squared filter coefficients of the block to analyze.
	var h2 [kFftLengthBy2]float32
	for k, value := range h {
		h2[k] = value * value
	}

	// Map out the region for estimating the reverb decay.
	gain := max(sum(h2[:])*(1.0/kFftLengthBy2), 1e-32)
	previousGain := &r.previousGains[r.blockToAnalyze]
	adapting := *previousGain > 1.1*gain || *previousGain < 0.9*gain
	aboveNoiseFloor := gain > r.tailGain
	*previousGain = gain

	// Count the consecutive "good" filter sections: those with an energy
	// above the noise floor that has not changed too much since the last
	// check.
	r.estimationRegionIdentified = r.estimationRegionIdentified || adapting || !aboveNoiseFloor
	if !r.estimationRegionIdentified {
		r.estimationRegionCandidateSize++
	}

	// Accumulate data for the reverb decay estimation and for the
	// estimation of the early reflections.
	if r.blockToAnalyze > r.lateReverbEnd {
		return
	}
	lateReverb := r.blockToAnalyze >= r.lateReverbStart
	for _, value := range h2 {
		h2Log2 := fastApproxLog2f(float32(float64(value) + 1e-10))
		if lateReverb {
			r.lateReverbDecayEstimator.accumulate(h2Log2)
		}
		r.earlyReverbEstimator.accumulate(h2Log2, r.smoothingConstant)
	}
}
