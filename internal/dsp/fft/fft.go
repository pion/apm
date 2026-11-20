// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package fft provides allocation-free radix-2 real FFTs for the audio
// processing modules. A Plan is constructed once per stream, then
// the Forward and Inverse methods run with no further allocations.
package fft

import "math"

// Plan computes forward and inverse real-input FFTs of a fixed length N.
// N must be a power of two; non-power-of-two sizes are not supported.
type Plan struct {
	n        int
	logN     int
	bitRev   []int
	twidReal []float32
	twidImag []float32
	workReal []float32
	workImag []float32
}

// New returns a Plan for length-n real FFTs.
func New(n int) *Plan {
	if n <= 0 || n&(n-1) != 0 {
		panic("fft.New: n must be a positive power of two") //nolint:forbidigo // Invariant violated by the caller.
	}
	logN := 0
	for k := n; k > 1; k >>= 1 {
		logN++
	}
	plan := &Plan{
		n:        n,
		logN:     logN,
		bitRev:   make([]int, n),
		twidReal: make([]float32, n/2),
		twidImag: make([]float32, n/2),
		workReal: make([]float32, n),
		workImag: make([]float32, n),
	}
	for i := range n {
		var r int
		for b := 0; b < logN; b++ {
			r = (r << 1) | ((i >> b) & 1)
		}
		plan.bitRev[i] = r
	}
	for k := 0; k < n/2; k++ {
		ang := -2 * math.Pi * float64(k) / float64(n)
		plan.twidReal[k] = float32(math.Cos(ang))
		plan.twidImag[k] = float32(math.Sin(ang))
	}

	return plan
}

// Size returns the FFT length.
func (p *Plan) Size() int { return p.n }

// Forward computes the real-input DFT of time into half-spectrum outputs.
// outReal and outImag must each have length n/2+1; only those bins are
// written (the upper half is the complex conjugate of the lower).
func (p *Plan) Forward(time, outReal, outImag []float32) {
	n := p.n
	if len(time) != n {
		panic("fft.Forward: time length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}
	if len(outReal) != n/2+1 || len(outImag) != n/2+1 {
		panic("fft.Forward: output length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}
	wr, wi := p.workReal, p.workImag
	for i := range n {
		wr[p.bitRev[i]] = time[i]
		wi[p.bitRev[i]] = 0
	}
	for s := 1; s <= p.logN; s++ {
		m := 1 << s
		halfM := m >> 1
		step := n / m
		for k := 0; k < n; k += m {
			for j := range halfM {
				tw := j * step
				tr := p.twidReal[tw]
				ti := p.twidImag[tw]
				ar := wr[k+j+halfM]
				ai := wi[k+j+halfM]
				ur := tr*ar - ti*ai
				ui := tr*ai + ti*ar
				wr[k+j+halfM] = wr[k+j] - ur
				wi[k+j+halfM] = wi[k+j] - ui
				wr[k+j] += ur
				wi[k+j] += ui
			}
		}
	}
	for k := 0; k <= n/2; k++ {
		outReal[k] = wr[k]
		outImag[k] = wi[k]
	}
}

// Inverse computes the real inverse DFT from half-spectrum inputs. inReal
// and inImag have length n/2+1; time receives n samples.
//
// The result is divided by n so Forward followed by Inverse round-trips.
func (p *Plan) Inverse(inReal, inImag, time []float32) {
	n := p.n
	if len(inReal) != n/2+1 || len(inImag) != n/2+1 {
		panic("fft.Inverse: input length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}
	if len(time) != n {
		panic("fft.Inverse: time length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}
	wr, wi := p.workReal, p.workImag
	// Reconstruct full Hermitian spectrum into bit-reversed slots.
	for k := 0; k <= n/2; k++ {
		wr[p.bitRev[k]] = inReal[k]
		wi[p.bitRev[k]] = inImag[k]
	}
	for k := n/2 + 1; k < n; k++ {
		wr[p.bitRev[k]] = inReal[n-k]
		wi[p.bitRev[k]] = -inImag[n-k]
	}
	// Inverse butterflies = forward with conjugated twiddles, then /n.
	for s := 1; s <= p.logN; s++ {
		m := 1 << s
		halfM := m >> 1
		step := n / m
		for k := 0; k < n; k += m {
			for j := range halfM {
				tw := j * step
				tr := p.twidReal[tw]
				ti := -p.twidImag[tw] // conjugate
				ar := wr[k+j+halfM]
				ai := wi[k+j+halfM]
				ur := tr*ar - ti*ai
				ui := tr*ai + ti*ar
				wr[k+j+halfM] = wr[k+j] - ur
				wi[k+j+halfM] = wi[k+j] - ui
				wr[k+j] += ur
				wi[k+j] += ui
			}
		}
	}
	inv := float32(1) / float32(n)
	for i := range n {
		time[i] = wr[i] * inv
	}
}
