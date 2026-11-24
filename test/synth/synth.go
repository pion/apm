// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package synth generates synthetic audio signals for property and
// regression tests: sine tones, swept sines, noise (white / pink /
// speech-shaped), impulses, exponentially-decaying echo IRs, and clean
// speech-plus-noise mixes at target SNR.
//
// Every generator is deterministic given an explicit Source: tests should
// pass an *rand.Rand seeded from a fixed value to keep CI stable.
package synth

import (
	"errors"
	"math"
	"math/rand/v2"
)

var (
	errEmptyInput = errors.New("synth: empty input")
	errZeroPower  = errors.New("synth: signal or noise has zero power")
)

// Sine returns n samples of a unit-amplitude sine at freqHz, rate Hz.
func Sine(n, rate int, freqHz, amplitude float64) []float32 {
	out := make([]float32, n)
	w := 2 * math.Pi * freqHz / float64(rate)
	for i := range n {
		out[i] = float32(amplitude * math.Sin(w*float64(i)))
	}

	return out
}

// Sweep returns n samples of a logarithmically-swept sine from f0 to f1.
// Useful for frequency-response tests (HPF corner, stopband, etc.).
func Sweep(n, rate int, f0, f1, amplitude float64) []float32 {
	if f0 <= 0 || f1 <= 0 {
		panic("synth.Sweep: frequencies must be positive") //nolint:forbidigo // Invariant violated by the caller.
	}
	out := make([]float32, n)
	T := float64(n) / float64(rate)
	k := math.Pow(f1/f0, 1/T)
	for i := range n {
		t := float64(i) / float64(rate)
		phase := 2 * math.Pi * f0 * (math.Pow(k, t) - 1) / math.Log(k)
		out[i] = float32(amplitude * math.Sin(phase))
	}

	return out
}

// WhiteNoise returns n samples of zero-mean unit-RMS Gaussian noise.
func WhiteNoise(n int, src *rand.Rand) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(src.NormFloat64())
	}

	return out
}

// PinkNoise returns n samples of 1/f noise via the Voss-McCartney algorithm.
// Output is roughly unit-RMS.
func PinkNoise(n int, src *rand.Rand) []float32 {
	const rows = 16
	var arr [rows]float64
	out := make([]float32, n)
	var key int
	for i := range n {
		key++
		// Find lowest set bit.
		var b int
		for b = 0; b < rows; b++ {
			if key&(1<<b) != 0 {
				break
			}
		}
		if b >= rows {
			b = rows - 1
		}
		arr[b] = src.NormFloat64()
		var sum float64
		for _, v := range arr {
			sum += v
		}
		out[i] = float32(sum / math.Sqrt(rows))
	}

	return out
}

// SpeechShapedNoise returns n samples of noise whose long-term average
// spectrum approximates ITU-T P.50 speech (LTASS). Simple second-order
// shaping is applied to white noise: good enough for masking tests.
func SpeechShapedNoise(n, rate int, src *rand.Rand) []float32 {
	white := WhiteNoise(n, src)
	// Two-pole low-shelf (cutoff ~1 kHz) gives a rough -6 dB/oct rolloff
	// from the noise floor: close enough to LTASS for masking tests.
	cutoff := 1000.0 / float64(rate)
	a := math.Exp(-2 * math.Pi * cutoff)
	var y1, y2 float64
	out := make([]float32, n)
	for i, x := range white {
		y := float64(x) + 2*a*y1 - a*a*y2
		y2, y1 = y1, y
		out[i] = float32(y * (1 - a) * (1 - a))
	}

	return out
}

// Impulse returns n samples with a single unit impulse at index pos.
func Impulse(n, pos int) []float32 {
	out := make([]float32, n)
	if pos >= 0 && pos < n {
		out[pos] = 1
	}

	return out
}

// ExpDecayIR returns an n-sample exponentially-decaying room impulse
// response with the given RT60 (in seconds). The first sample is the
// direct path (unit amplitude), subsequent samples decay with the noise
// floor set by white noise modulated by the decay envelope.
//
// Use this for AEC tests where you need a known echo path without
// committing a real-room WAV. Not a substitute for measured IRs.
func ExpDecayIR(n, rate int, rt60 float64, src *rand.Rand) []float32 {
	out := make([]float32, n)
	// −60 dB after rt60 seconds → decay factor per sample.
	decay := math.Pow(10, -3/(rt60*float64(rate)))
	env := 1.0
	for i := range n {
		out[i] = float32(env * src.NormFloat64() * 0.3)
		env *= decay
	}
	out[0] = 1 // direct path

	return out
}

// Convolve returns the linear convolution of x and h (length len(x)+len(h)-1).
// Pure-Go reference implementation: O(n*m); fine for short IRs in tests.
func Convolve(x, h []float32) []float32 {
	y := make([]float32, len(x)+len(h)-1)
	for i, xi := range x {
		if xi == 0 {
			continue
		}
		for j, hj := range h {
			y[i+j] += xi * hj
		}
	}

	return y
}

// MixAtSNR mixes clean and noise such that the output has the requested
// signal-to-noise ratio (in dB). Output length is min(len(clean), len(noise)).
// Returned signal is normalized so peak ≤ 1.
func MixAtSNR(clean, noise []float32, snrDB float64) ([]float32, error) {
	n := min(len(noise), len(clean))
	if n == 0 {
		return nil, errEmptyInput
	}
	var sigPow, noisePow float64
	for i := range n {
		sigPow += float64(clean[i]) * float64(clean[i])
		noisePow += float64(noise[i]) * float64(noise[i])
	}
	sigPow /= float64(n)
	noisePow /= float64(n)
	if sigPow == 0 || noisePow == 0 {
		return nil, errZeroPower
	}
	target := sigPow / math.Pow(10, snrDB/10)
	gain := math.Sqrt(target / noisePow)
	out := make([]float32, n)
	var peak float32
	for i := range n {
		s := clean[i] + float32(gain)*noise[i]
		out[i] = s
		if a := abs32(s); a > peak {
			peak = a
		}
	}
	if peak > 1 {
		inv := 1 / peak
		for i := range out {
			out[i] *= inv
		}
	}

	return out, nil
}

// LevelDBFS returns the RMS level of x in dBFS (full-scale reference = 1.0).
func LevelDBFS(x []float32) float64 {
	if len(x) == 0 {
		return math.Inf(-1)
	}
	var sum float64
	for _, v := range x {
		sum += float64(v) * float64(v)
	}
	rms := math.Sqrt(sum / float64(len(x)))
	if rms <= 0 {
		return math.Inf(-1)
	}

	return 20 * math.Log10(rms)
}

// ScaleToDBFS scales x in place so its RMS level matches the target
// dBFS. Returns x for chaining.
func ScaleToDBFS(x []float32, dbfs float64) []float32 {
	cur := LevelDBFS(x)
	if math.IsInf(cur, -1) {
		return x
	}
	gain := math.Pow(10, (dbfs-cur)/20)
	for i := range x {
		x[i] = float32(float64(x[i]) * gain)
	}

	return x
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}

	return x
}
