// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// SegmentalSNR returns the segmental signal-to-noise ratio between a
// reference signal and a test signal, in dB. The signals are split into
// non-overlapping frames of length frameLen; per-frame SNR is computed,
// clipped to [minDB, maxDB] (ITU recommendation: -10, 35), and averaged.
//
// Frames with reference RMS below silenceThreshDBFS (e.g., -50) are
// skipped: this prevents silent regions from dominating the average.
func SegmentalSNR(reference, test []float32, frameLen int, silenceThreshDBFS float64) float64 {
	const (
		minDB = -10
		maxDB = 35
	)
	n := min(len(test), len(reference))
	if frameLen <= 0 || n < frameLen {
		return math.NaN()
	}
	silenceRefPow := math.Pow(10, silenceThreshDBFS/10)
	var sum float64
	var count int
	for i := 0; i+frameLen <= n; i += frameLen {
		var sigPow, errPow float64
		for j := range frameLen {
			s := float64(reference[i+j])
			d := s - float64(test[i+j])
			sigPow += s * s
			errPow += d * d
		}
		sigPow /= float64(frameLen)
		errPow /= float64(frameLen)
		if sigPow < silenceRefPow {
			continue
		}
		if errPow == 0 {
			sum += maxDB
		} else {
			db := 10 * math.Log10(sigPow/errPow)
			if db < minDB {
				db = minDB
			}
			if db > maxDB {
				db = maxDB
			}
			sum += db
		}
		count++
	}
	if count == 0 {
		return math.NaN()
	}

	return sum / float64(count)
}

// SNR returns the global (non-segmental) SNR in dB. Equivalent to
// SegmentalSNR with frameLen = len(reference), but easier to read at call
// sites that just want a single number.
func SNR(reference, test []float32) float64 {
	n := min(len(test), len(reference))
	var sig, err float64
	for i := range n {
		s := float64(reference[i])
		d := s - float64(test[i])
		sig += s * s
		err += d * d
	}
	if err == 0 {
		return math.Inf(+1)
	}
	if sig == 0 {
		return math.Inf(-1)
	}

	return 10 * math.Log10(sig/err)
}
