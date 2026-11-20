// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import (
	"math"
	"math/cmplx"
)

// A small radix-2 real FFT helper used by lsd/cepstral. Self-contained so
// the metrics package doesn't pull in gonum just for this: keeps test
// runtime fast. Production code uses internal/dsp/fft.

type rfftPlan struct {
	n     int
	twid  []complex128
	logN  int
	bitrv []int
}

func newRFFTPlan(n int) *rfftPlan {
	if n <= 0 || (n&(n-1)) != 0 {
		panic("rfft: n must be a power of two") //nolint:forbidigo // Invariant violated by the caller.
	}
	logN := 0
	for k := n; k > 1; k >>= 1 {
		logN++
	}
	twid := make([]complex128, n/2)
	for k := 0; k < n/2; k++ {
		ang := -2 * math.Pi * float64(k) / float64(n)
		twid[k] = cmplx.Rect(1, ang)
	}
	bitrv := make([]int, n)
	for i := range n {
		var r int
		for b := 0; b < logN; b++ {
			r = (r << 1) | ((i >> b) & 1)
		}
		bitrv[i] = r
	}

	return &rfftPlan{n: n, twid: twid, logN: logN, bitrv: bitrv}
}

// Magnitude returns the n/2+1 magnitudes of a real-input FFT.
// Allocates a fresh slice each call: these helpers are test-only.
func (p *rfftPlan) Magnitude(x []float64) []float64 {
	if len(x) != p.n {
		panic("rfft: wrong input length") //nolint:forbidigo // Invariant violated by the caller.
	}
	buf := make([]complex128, p.n)
	for i, v := range x {
		buf[p.bitrv[i]] = complex(v, 0)
	}
	for s := 1; s <= p.logN; s++ {
		m := 1 << s
		halfM := m >> 1
		step := p.n / m
		for k := 0; k < p.n; k += m {
			for j := range halfM {
				t := p.twid[j*step] * buf[k+j+halfM]
				buf[k+j+halfM] = buf[k+j] - t
				buf[k+j] += t
			}
		}
	}
	out := make([]float64, p.n/2+1)
	for i := range out {
		out[i] = cmplx.Abs(buf[i])
	}

	return out
}

func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}

	return p
}

// hann returns an n-sample Hann window.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range n {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}

	return w
}
