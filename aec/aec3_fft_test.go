// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

func assertTransform(t *testing.T, want, got []float32, name string) {
	t.Helper()
	assert.Equal(t, want, got, name)
}

// TestAec3Fft_MatchesUpstream compares every transform with the upstream
// Aec3Fft at the pinned commit, bit for bit, fed the same generated
// noise. Each line
// of the reference file is keyed by the name of the transform.
func TestAec3Fft_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/aec3_fft_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	assertFftData := func(name string, data *fftData) {
		assertTransform(t, reference[name+"_re"], data.re[:], name+" re")
		assertTransform(t, reference[name+"_im"], data.im[:], name+" im")
	}

	seed := uint32(1)
	next := func() float32 {
		seed = seed*1664525 + 1013904223

		return float32(int(seed>>16) - 32768)
	}
	var signal [kFftLength]float32
	var first, second [kFftLengthBy2]float32
	for i := range signal {
		signal[i] = next() //nolint:gosec // G602 false positive on a range loop.
	}
	for i := range first {
		first[i] = next() //nolint:gosec // G602 false positive on a range loop.
	}
	for i := range second {
		second[i] = next() //nolint:gosec // G602 false positive on a range loop.
	}

	fft := newAec3Fft()
	var data fftData
	signalCopy := signal
	fft.fwd(&signalCopy, &data)
	assertFftData("fft", &data)

	var inverse [kFftLength]float32
	fft.inv(&data, &inverse)
	assertTransform(t, reference["ifft"], inverse[:], "ifft")

	fft.zeroPaddedFft(&first, windowRectangular, &data)
	assertFftData("zero_padded_rectangular", &data)
	fft.zeroPaddedFft(&first, windowHanning, &data)
	assertFftData("zero_padded_hanning", &data)
	fft.paddedFft(&first, &second, windowRectangular, &data)
	assertFftData("padded_rectangular", &data)
	fft.paddedFft(&first, &second, windowSqrtHanning, &data)
	assertFftData("padded_sqrt_hanning", &data)

	var power [kFftLengthBy2Plus1]float32
	data.spectrum(&power)
	assertTransform(t, reference["spectrum"], power[:], "spectrum")
}

func assertEach(t *testing.T, want float32, got []float32, name string) {
	t.Helper()
	for k, v := range got {
		assert.Equal(t, want, v, "%s[%d]", name, k)
	}
}

// Port of Fft.
func TestAec3Fft_Fft(t *testing.T) {
	fft := newAec3Fft()
	var samples [kFftLength]float32
	var spectrum fftData

	fft.fwd(&samples, &spectrum)
	assertEach(t, 0, spectrum.re[:], "re")
	assertEach(t, 0, spectrum.im[:], "im")

	samples = [kFftLength]float32{}
	samples[0] = 1
	fft.fwd(&samples, &spectrum)
	assertEach(t, 1, spectrum.re[:], "re")
	assertEach(t, 0, spectrum.im[:], "im")

	for k := range samples {
		samples[k] = 1 //nolint:gosec // G602 false positive on a range loop.
	}
	fft.fwd(&samples, &spectrum)
	assert.Equal(t, float32(128), spectrum.re[0])
	assertEach(t, 0, spectrum.re[1:], "re")
	assertEach(t, 0, spectrum.im[:], "im")
}

// Port of Ifft.
func TestAec3Fft_Ifft(t *testing.T) {
	fft := newAec3Fft()
	var samples [kFftLength]float32
	var spectrum fftData

	fft.inv(&spectrum, &samples)
	assertEach(t, 0, samples[:], "samples")

	for k := range spectrum.re {
		spectrum.re[k] = 1
	}
	fft.inv(&spectrum, &samples)
	assert.Equal(t, float32(64), samples[0])
	assertEach(t, 0, samples[1:], "samples")

	spectrum = fftData{}
	spectrum.re[0] = 128
	fft.inv(&spectrum, &samples)
	assertEach(t, 64, samples[:], "samples")
}

// Port of FftAndIfft.
func TestAec3Fft_FftAndIfft(t *testing.T) {
	fft := newAec3Fft()
	var samples, samplesRef [kFftLength]float32
	var spectrum fftData

	v := 0
	for range 20 {
		for j := range samples {
			samples[j] = float32(v)         //nolint:gosec // G602 false positive on a range loop.
			samplesRef[j] = samples[j] * 64 //nolint:gosec // G602 false positive on a range loop.
			v++
		}
		fft.fwd(&samples, &spectrum)
		fft.inv(&spectrum, &samples)
		assert.InDeltaSlice(t, samplesRef[:], samples[:], 0.001)
	}
}

// Port of ZeroPaddedFft.
func TestAec3Fft_ZeroPaddedFft(t *testing.T) {
	fft := newAec3Fft()
	var xIn [kFftLengthBy2]float32
	var xRef, xOut [kFftLength]float32
	var spectrum fftData

	v := 0
	for range 20 {
		for j := range xIn {
			xIn[j] = float32(v)                 //nolint:gosec // G602 false positive on a range loop.
			xRef[j+kFftLengthBy2] = xIn[j] * 64 //nolint:gosec // G602 false positive on a range loop.
			v++
		}
		fft.zeroPaddedFft(&xIn, windowRectangular, &spectrum)
		fft.inv(&spectrum, &xOut)
		assert.InDeltaSlice(t, xRef[:], xOut[:], 0.1)
	}
}

// Port of PaddedFft.
func TestAec3Fft_PaddedFft(t *testing.T) {
	fft := newAec3Fft()
	var xIn, xOld, xOldRef [kFftLengthBy2]float32
	var xRef, xOut [kFftLength]float32
	var spectrum fftData

	v := 0
	for range 20 {
		for j := range xIn {
			xIn[j] = float32(v) //nolint:gosec // G602 false positive on a range loop.
			v++
		}

		copy(xRef[:], xOld[:])
		copy(xRef[kFftLengthBy2:], xIn[:])
		xOldRef = xIn
		for j := range xRef {
			xRef[j] *= 64 //nolint:gosec // G602 false positive on a range loop.
		}

		fft.paddedFft(&xIn, &xOld, windowRectangular, &spectrum)
		xOld = xIn
		fft.inv(&spectrum, &xOut)

		assert.InDeltaSlice(t, xRef[:], xOut[:], 0.1)
		assert.Equal(t, xOldRef, xOld)
	}
}
