// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// ERLE returns the Echo Return Loss Enhancement of an echo canceller, in
// dB. Inputs:
//
//   - mic: the near-end microphone signal (echo + any near-end speech).
//   - residual: the canceller's output (echo, hopefully largely removed).
//
// ERLE per frame is 10·log10(P_mic / P_residual). The function returns
// the median per-frame ERLE over non-silent frames (median is more robust
// than mean against the rare divergent frame). Silent mic frames
// (below silenceThreshDBFS) are skipped.
func ERLE(mic, residual []float32, frameLen int, silenceThreshDBFS float64) float64 {
	n := min(len(residual), len(mic))
	if frameLen <= 0 || n < frameLen {
		return math.NaN()
	}
	silencePow := math.Pow(10, silenceThreshDBFS/10)
	var dbs []float64
	for i := 0; i+frameLen <= n; i += frameLen {
		var pm, pr float64
		for j := range frameLen {
			m := float64(mic[i+j])
			r := float64(residual[i+j])
			pm += m * m
			pr += r * r
		}
		pm /= float64(frameLen)
		pr /= float64(frameLen)
		if pm < silencePow {
			continue
		}
		if pr == 0 {
			dbs = append(dbs, 80)

			continue
		}
		dbs = append(dbs, 10*math.Log10(pm/pr))
	}
	if len(dbs) == 0 {
		return math.NaN()
	}

	return median(dbs)
}

// MaxConvergedERLE returns the best per-frame ERLE achieved during the
// steady-state portion of the input (last 50%). Useful for asymptotic
// convergence assertions where the average isn't representative of how
// well the canceller eventually performed.
func MaxConvergedERLE(mic, residual []float32, frameLen int) float64 {
	n := min(len(residual), len(mic))
	startFrame := (n / frameLen) / 2
	startSample := startFrame * frameLen
	if startSample >= n {
		return math.NaN()
	}
	best := math.Inf(-1)
	for i := startSample; i+frameLen <= n; i += frameLen {
		var pm, pr float64
		for j := range frameLen {
			m := float64(mic[i+j])
			r := float64(residual[i+j])
			pm += m * m
			pr += r * r
		}
		if pm == 0 {
			continue
		}
		if pr == 0 {
			return math.Inf(+1)
		}
		db := 10 * math.Log10(pm/pr)
		if db > best {
			best = db
		}
	}

	return best
}

func median(xs []float64) float64 {
	// Partial sort: copy then sort.
	cp := make([]float64, len(xs))
	copy(cp, xs)
	insertionSort(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 0 {
		return (cp[mid-1] + cp[mid]) / 2
	}

	return cp[mid]
}

func insertionSort(a []float64) {
	for i := 1; i < len(a); i++ {
		v := a[i]
		j := i - 1
		for j >= 0 && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}
