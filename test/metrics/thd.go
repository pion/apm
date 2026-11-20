// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// THDPlusN returns the total harmonic distortion plus noise of a signal
// containing a single sinusoid at fundamentalHz, in dB. Negative; closer
// to -∞ means a cleaner tone.
//
//	THD+N (dB) = 10·log10( (P_total - P_fund) / P_fund )
//
// The fundamental's power is estimated by integrating an FFT bin window
// around fundamentalHz; everything outside that window is attributed to
// distortion plus noise.
func THDPlusN(x []float32, sampleRate int, fundamentalHz float64) float64 {
	n := nextPow2(len(x))
	plan := newRFFTPlan(n)
	w := hann(n)
	buf := make([]float64, n)
	for i := 0; i < len(x) && i < n; i++ {
		buf[i] = float64(x[i]) * w[i]
	}
	mag := plan.Magnitude(buf)
	binHz := float64(sampleRate) / float64(n)
	fundBin := int(math.Round(fundamentalHz / binHz))
	const halfWindow = 2 // ±2 bins around fundamental
	var fundPow, totPow float64
	for k := 1; k < len(mag); k++ { // skip DC
		p := mag[k] * mag[k]
		totPow += p
		if k >= fundBin-halfWindow && k <= fundBin+halfWindow {
			fundPow += p
		}
	}
	dist := totPow - fundPow
	if fundPow <= 0 {
		return math.NaN()
	}
	if dist <= 0 {
		return math.Inf(-1)
	}

	return 10 * math.Log10(dist/fundPow)
}
