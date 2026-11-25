// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package biquad implements a cascaded biquad filter in Direct Form I,
// matching upstream WebRTC's modules/audio_processing/utility/
// cascaded_biquad_filter.{h,cc}.
//
// The difference equation per section is:
//
//	y[k] = b[0]*x[k] + b[1]*x[k-1] + b[2]*x[k-2] - a[0]*y[k-1] - a[1]*y[k-2]
//
// where a[0]/a[1] are the conventional a1/a2 (the upstream struct omits
// a0 because it is normalized to 1).
package biquad

// Coefficients holds one biquad section. Layout mirrors upstream's
// CascadedBiQuadFilter::BiQuadCoefficients exactly: B is {b0, b1, b2},
// A is {a1, a2} with a0 implicitly 1.
type Coefficients struct {
	B [3]float32
	A [2]float32
}

// Biquad is one stage of a cascade: coefficients plus the two-sample
// delay lines for input and output.
type Biquad struct {
	C Coefficients
	x [2]float32
	y [2]float32
}

// Reset zeros the delay lines.
func (b *Biquad) Reset() {
	b.x[0], b.x[1] = 0, 0
	b.y[0], b.y[1] = 0, 0
}

// Cascade is an ordered list of biquads applied in series.
type Cascade struct {
	stages []Biquad
}

// NewCascade constructs a cascade from the given per-stage coefficients.
// One Cascade per channel.
func NewCascade(coeffs []Coefficients) *Cascade {
	c := &Cascade{stages: make([]Biquad, len(coeffs))}
	for i, k := range coeffs {
		c.stages[i] = Biquad{C: k}
	}

	return c
}

// Reset zeros every stage's delay line.
func (c *Cascade) Reset() {
	for i := range c.stages {
		c.stages[i].Reset()
	}
}

// Stages returns the number of biquad sections.
func (c *Cascade) Stages() int { return len(c.stages) }

// Process filters samples in place. Allocation-free.
func (c *Cascade) Process(samples []float32) {
	for i := range c.stages {
		applyBiquad(&c.stages[i], samples)
	}
}

func applyBiquad(stage *Biquad, y []float32) {
	cA0 := stage.C.A[0]
	cA1 := stage.C.A[1]
	cB0 := stage.C.B[0]
	cB1 := stage.C.B[1]
	cB2 := stage.C.B[2]
	mX0 := stage.x[0]
	mX1 := stage.x[1]
	mY0 := stage.y[0]
	mY1 := stage.y[1]
	for k, tmp := range y {
		out := cB0*tmp + cB1*mX0 + cB2*mX1 - cA0*mY0 - cA1*mY1
		y[k] = out
		mX1 = mX0
		mX0 = tmp
		mY1 = mY0
		mY0 = out
	}
	stage.x[0] = mX0
	stage.x[1] = mX1
	stage.y[0] = mY0
	stage.y[1] = mY1
}
