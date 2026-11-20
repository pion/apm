// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "math"

// Upstream's fast_math.cc currently delegates Sqrt/Pow2 to libm and uses a
// bit-trick approximation for log2. The trick has measurable error but
// the algorithm's tuning is calibrated against it, so we replicate it.

func fastLog2f(in float32) float32 {
	bits := math.Float32bits(in)
	out := float32(bits)
	out *= 1.1920929e-7
	out -= 126.942695

	return out
}

func sqrtFastApprox(f float32) float32 { return float32(math.Sqrt(float64(f))) }

func pow2Approx(p float32) float32 { return float32(math.Pow(2, float64(p))) }

func powApprox(x, p float32) float32 { return pow2Approx(p * fastLog2f(x)) }

func logApprox(x float32) float32 {
	const ln2 = float32(math.Ln2)

	return fastLog2f(x) * ln2
}

func logApproxSpan(x, y []float32) {
	for k, v := range x {
		y[k] = logApprox(v)
	}
}

func expApprox(x float32) float32 {
	const log10e = float32(math.Log10E)

	return powApprox(10, x*log10e)
}

func expApproxSpan(x, y []float32) {
	for k, v := range x {
		y[k] = expApprox(v)
	}
}

func expApproxSignFlipSpan(x, y []float32) {
	for k, v := range x {
		y[k] = expApprox(-v)
	}
}
