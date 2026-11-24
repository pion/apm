// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// AttackTime returns the time (in seconds) for a level-tracking signal
// (e.g., AGC output) to reach withinDB of its final value after a step
// change at stepSample.
//
// Algorithm:
//  1. Compute short-time RMS level (in dB) with an integration window of
//     analysisFrame samples and hop equal to that frame.
//  2. Define final level as the median over the last 25 % of frames.
//  3. Find the first post-step frame whose level is within `withinDB` of
//     final and stays there for at least `stableFrames` frames.
//
// Returns NaN if the signal never converges.
func AttackTime(out []float32, sampleRate, stepSample, analysisFrame, stableFrames int, withinDB float64) float64 {
	if analysisFrame <= 0 || sampleRate <= 0 {
		return math.NaN()
	}
	levels := frameLevelsDB(out, analysisFrame)
	stepFrame := stepSample / analysisFrame
	if stepFrame >= len(levels) {
		return math.NaN()
	}
	finalStart := len(levels) - len(levels)/4
	if finalStart <= stepFrame {
		finalStart = stepFrame + 1
	}
	final := medianSlice(levels[finalStart:])
	stable := 0
	for i := stepFrame; i < len(levels); i++ {
		if math.Abs(levels[i]-final) <= withinDB {
			stable++
			if stable >= stableFrames {
				convergeFrame := i - stable + 1

				return float64((convergeFrame-stepFrame)*analysisFrame) / float64(sampleRate)
			}
		} else {
			stable = 0
		}
	}

	return math.NaN()
}

// PeakLevelDBFS returns the maximum absolute sample of x in dBFS.
func PeakLevelDBFS(x []float32) float64 {
	var peak float64
	for _, v := range x {
		if a := math.Abs(float64(v)); a > peak {
			peak = a
		}
	}
	if peak == 0 {
		return math.Inf(-1)
	}

	return 20 * math.Log10(peak)
}

func frameLevelsDB(x []float32, frame int) []float64 {
	if frame <= 0 {
		return nil
	}
	n := len(x) / frame
	out := make([]float64, n)
	for i := range n {
		var s float64
		for j := range frame {
			v := float64(x[i*frame+j])
			s += v * v
		}
		rms := math.Sqrt(s / float64(frame))
		if rms <= 0 {
			out[i] = -120
		} else {
			out[i] = 20 * math.Log10(rms)
		}
	}

	return out
}

func medianSlice(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	cp := make([]float64, len(x))
	copy(cp, x)
	insertionSort(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 0 {
		return (cp[mid-1] + cp[mid]) / 2
	}

	return cp[mid]
}
