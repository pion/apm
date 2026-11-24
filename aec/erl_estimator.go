// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	minErl = 0.01
	maxErl = 1000
	// erlX2Min corresponds to WGN of power -46 dBFS.
	erlX2Min       = 44015068.0
	erlHoldCounter = 1000
)

// erlEstimator mirrors ErlEstimator: it estimates the echo return loss
// based on the signal spectra.
type erlEstimator struct {
	startupPhaseLengthBlocks int
	erl                      [kFftLengthBy2Plus1]float32
	holdCounters             [kFftLengthBy2 - 1]int
	erlTimeDomain            float32
	holdCounterTimeDomain    int
	blocksSinceReset         int
}

func newErlEstimator(startupPhaseLengthBlocks int) *erlEstimator {
	estimator := &erlEstimator{startupPhaseLengthBlocks: startupPhaseLengthBlocks, erlTimeDomain: maxErl}
	fill(estimator.erl[:], maxErl)

	return estimator
}

// reset mirrors Reset: it resets the ERL estimation.
func (e *erlEstimator) reset() {
	e.blocksSinceReset = 0
}

// maxSpectrum returns the maximum over the spectra for which use is nil
// or true. The first of them must exist.
func maxSpectrum(spectra [][kFftLengthBy2Plus1]float32, use []bool) [kFftLengthBy2Plus1]float32 {
	first := 0
	for use != nil && !use[first] {
		first++
	}
	result := spectra[first]
	for ch := first + 1; ch < len(spectra); ch++ {
		if use != nil && !use[ch] {
			continue
		}
		for k := range result {
			result[k] = max(result[k], spectra[ch][k]) //nolint:gosec // G602 false positive on a range loop.
		}
	}

	return result
}

// update mirrors Update: it updates the ERL estimate.
func (e *erlEstimator) update(convergedFilters []bool, renderSpectra, captureSpectra [][kFftLengthBy2Plus1]float32) {
	anyFilterConverged := false
	for _, converged := range convergedFilters {
		anyFilterConverged = anyFilterConverged || converged
	}

	e.blocksSinceReset++
	if e.blocksSinceReset < e.startupPhaseLengthBlocks || !anyFilterConverged {
		return
	}

	// Use the maximum spectrum across capture, over the channels with a
	// converged filter, and the maximum across render.
	y2 := captureSpectra[0]
	if len(convergedFilters) > 1 {
		y2 = maxSpectrum(captureSpectra, convergedFilters)
	}
	x2 := maxSpectrum(renderSpectra, nil)

	e.updateBins(&x2, &y2)
	e.updateTimeDomain(&x2, &y2)
}

// updateBins updates the estimates in a maximum statistics manner.
func (e *erlEstimator) updateBins(x2, y2 *[kFftLengthBy2Plus1]float32) {
	for k := 1; k < kFftLengthBy2; k++ {
		if x2[k] > erlX2Min {
			newErl := y2[k] / x2[k]
			if newErl < e.erl[k] {
				e.holdCounters[k-1] = erlHoldCounter
				e.erl[k] += 0.1 * (newErl - e.erl[k])
				e.erl[k] = max(e.erl[k], minErl)
			}
		}
	}

	for k := range e.holdCounters {
		e.holdCounters[k]--
		if e.holdCounters[k] <= 0 {
			e.erl[k+1] = min(maxErl, 2*e.erl[k+1])
		}
	}

	e.erl[0] = e.erl[1]
	e.erl[kFftLengthBy2] = e.erl[kFftLengthBy2-1]
}

// updateTimeDomain computes the ERL over all frequency bins.
func (e *erlEstimator) updateTimeDomain(x2, y2 *[kFftLengthBy2Plus1]float32) {
	var x2Sum, y2Sum float32
	for k := range x2 {
		x2Sum += x2[k]
		y2Sum += y2[k]
	}
	if x2Sum > erlX2Min*kFftLengthBy2Plus1 {
		newErl := y2Sum / x2Sum
		if newErl < e.erlTimeDomain {
			e.holdCounterTimeDomain = erlHoldCounter
			e.erlTimeDomain += 0.1 * (newErl - e.erlTimeDomain)
			e.erlTimeDomain = max(e.erlTimeDomain, minErl)
		}
	}

	e.holdCounterTimeDomain--
	if e.holdCounterTimeDomain <= 0 {
		e.erlTimeDomain = min(maxErl, 2*e.erlTimeDomain)
	}
}

// getErl mirrors Erl: the most recent ERL estimate.
func (e *erlEstimator) getErl() *[kFftLengthBy2Plus1]float32 { return &e.erl }

// getErlTimeDomain mirrors ErlTimeDomain.
func (e *erlEstimator) getErlTimeDomain() float32 { return e.erlTimeDomain }
