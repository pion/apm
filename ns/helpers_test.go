// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ns

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFastMathApproximations(t *testing.T) {
	for x := 0.01; x < 1e6; x *= 1.01 {
		assert.InDelta(t, math.Log2(x), fastLog2f(float32(x)), 0.06, "log2(%g)", x)
		assert.InDelta(t, math.Log(x), logApprox(float32(x)), 0.04, "ln(%g)", x)
		assert.InDelta(t, math.Sqrt(x), sqrtFastApprox(float32(x)), 1e-6*math.Sqrt(x)+1e-6, "sqrt(%g)", x)
	}
	for x := -5.0; x <= 5; x += 0.01 {
		assert.InEpsilon(t, math.Exp(x), expApprox(float32(x)), 0.025, "exp(%g)", x)
	}
}

func TestFastMathSpans(t *testing.T) {
	in := []float32{0.5, 1, 2, 10}
	out := make([]float32, len(in))

	logApproxSpan(in, out)
	for i, v := range in {
		assert.Equal(t, logApprox(v), out[i])
	}
	expApproxSpan(in, out)
	for i, v := range in {
		assert.Equal(t, expApprox(v), out[i])
	}
	expApproxSignFlipSpan(in, out)
	for i, v := range in {
		assert.Equal(t, expApprox(-v), out[i])
	}
}

func TestFilterBankWindowIsPowerComplementary(t *testing.T) {
	var window [fftSize]float32
	for i := range window {
		window[i] = 1
	}
	applyFilterBankWindow(&window)

	// Overlap-add with a 160 sample hop needs w[i]^2 + w[i+160]^2 == 1.
	for i := range fftSize - 160 {
		sum := window[i]*window[i] + window[i+160]*window[i+160]
		assert.InDelta(t, 1, sum, 1e-5, "i=%d", i)
	}
}

func TestFFTRoundTrip(t *testing.T) {
	f := newFFT()
	var in, out [fftSize]float32
	for i := range in {
		in[i] = float32(1000 * math.Sin(float64(i)*0.3))
	}
	// Like upstream, the forward transform overwrites its input.
	data := in
	var re, im [fftSizeBy2Plus1]float32
	f.forward(&data, &re, &im)
	f.inverse(&re, &im, &out)
	assert.InDeltaSlice(t, in[:], out[:], 1e-2)
}
