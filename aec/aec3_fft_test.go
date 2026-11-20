// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

// aec3FftReferenceRelativeDelta bounds the difference to upstream
// relative to the largest value of a transform. The Go FFT rounds
// differently than the Ooura FFT used upstream.
const aec3FftReferenceRelativeDelta = 2e-6

func assertTransform(t *testing.T, want, got []float32, name string) {
	t.Helper()
	var largest float32
	for _, value := range want {
		largest = max(largest, abs32(value))
	}
	assert.InDeltaSlice(t, want, got, float64(largest)*aec3FftReferenceRelativeDelta, name)
}

// TestAec3Fft_MatchesUpstream compares every transform with the upstream
// Aec3Fft at the pinned commit, fed the same generated noise. Each line
// of the reference file is keyed by the name of the transform.
func TestAec3Fft_MatchesUpstream(t *testing.T) {
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
		signal[i] = next()
	}
	for i := range first {
		first[i] = next()
	}
	for i := range second {
		second[i] = next()
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
