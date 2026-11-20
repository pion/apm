// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// CepstralDistance returns the mean cepstral distance between a reference
// and test signal, in dB. Uses the standard formula:
//
//	CD = (10/ln(10)) · sqrt( 2 · Σ_{k=1..K} (c_ref[k] - c_test[k])² )
//
// averaged over short-time frames. K is the number of cepstral
// coefficients to compare (typically 13-24).
//
// The cepstrum here is the real cepstrum: IFFT(log(|FFT(x)|)).
func CepstralDistance(reference, test []float32, sampleRate, frameLen, hop, numCoeffs int) float64 {
	if frameLen <= 0 || hop <= 0 || numCoeffs <= 0 {
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
	scale := 10 / math.Log(10)
	var sum float64
	var frames int
	for i := 0; i+frameLen <= n; i += hop {
		for j := 0; j < frameLen; j++ {
			bufR[j] = float64(reference[i+j]) * w[j]
			bufT[j] = float64(test[i+j]) * w[j]
		}
		cR := realCepstrum(plan, bufR, numCoeffs)
		cT := realCepstrum(plan, bufT, numCoeffs)
		var fsum float64
		for k := 1; k < len(cR); k++ {
			d := cR[k] - cT[k]
			fsum += d * d
		}
		sum += scale * math.Sqrt(2*fsum)
		frames++
	}
	if frames == 0 {
		return math.NaN()
	}

	return sum / float64(frames)
}

// realCepstrum returns the first numCoeffs+1 real cepstral coefficients
// of x. Coefficient 0 is the log-energy, coefficients 1..numCoeffs are
// used in distance computation.
func realCepstrum(plan *rfftPlan, x []float64, numCoeffs int) []float64 {
	mag := plan.Magnitude(x)
	// log-magnitude spectrum, then inverse DCT-II via DFT trick:
	// real cepstrum c[n] = (1/N) Σ_k log(|X_k|) · cos(2π·n·k/N)
	// We compute it directly because the magnitude spectrum has only N/2+1 bins.
	const eps = 1e-10
	N := len(mag)
	logMag := make([]float64, N)
	for i, m := range mag {
		logMag[i] = math.Log(m + eps)
	}
	out := make([]float64, numCoeffs+1)
	for n := 0; n <= numCoeffs; n++ {
		var s float64
		for k := range N {
			s += logMag[k] * math.Cos(math.Pi*float64(n)*float64(k)/float64(N-1))
		}
		out[n] = s / float64(N)
	}

	return out
}
