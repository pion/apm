// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// LogSpectralDistance returns the mean log-spectral distance (LSD) between
// a reference and test signal, in dB. Lower is better. Computed as:
//
//	LSD = sqrt( (1/K) · Σ_k (10·log10(|R_k|² + ε) - 10·log10(|T_k|² + ε))² )
//
// averaged over short-time frames with the given window size and hop.
//
// frameLen should be a power of two; hop is typically frameLen/2.
func LogSpectralDistance(reference, test []float32, sampleRate, frameLen, hop int) float64 {
	if frameLen <= 0 || hop <= 0 {
		return math.NaN()
	}
	frameLen = nextPow2(frameLen)
	n := min(len(test), len(reference))
	if n < frameLen {
		return math.NaN()
	}
	plan := newRFFTPlan(frameLen)
	w := hann(frameLen)
	bufR := make([]float64, frameLen)
	bufT := make([]float64, frameLen)
	const eps = 1e-10
	var sum float64
	var frames int
	for i := 0; i+frameLen <= n; i += hop {
		for j := 0; j < frameLen; j++ {
			bufR[j] = float64(reference[i+j]) * w[j]
			bufT[j] = float64(test[i+j]) * w[j]
		}
		magR := plan.Magnitude(bufR)
		magT := plan.Magnitude(bufT)
		var fsum float64
		for k := range magR {
			a := 10 * math.Log10(magR[k]*magR[k]+eps)
			b := 10 * math.Log10(magT[k]*magT[k]+eps)
			d := a - b
			fsum += d * d
		}
		sum += math.Sqrt(fsum / float64(len(magR)))
		frames++
	}
	if frames == 0 {
		return math.NaN()
	}

	return sum / float64(frames)
}
