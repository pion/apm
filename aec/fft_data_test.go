// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// testFftData fills x like the upstream tests.
func testFftData() fftData {
	var x fftData
	for k := range x.re {
		x.re[k] = float32(k + 1)
	}
	for k := 1; k < len(x.im)-1; k++ {
		x.im[k] = 2 * float32(k+1)
	}

	return x
}

// Port of Clear.
func TestFftData_Clear(t *testing.T) {
	var x fftData
	for k := range x.re {
		x.re[k] = 1
		x.im[k] = 2
	}
	x.clear()
	assert.Equal(t, fftData{}, x)
}

// Port of Spectrum.
func TestFftData_Spectrum(t *testing.T) {
	x := testFftData()
	var spectrum [kFftLengthBy2Plus1]float32
	x.spectrum(&spectrum)

	assert.Equal(t, x.re[0]*x.re[0], spectrum[0])
	assert.Equal(t, x.re[kFftLengthBy2]*x.re[kFftLengthBy2], spectrum[kFftLengthBy2])
	for k := 1; k < kFftLengthBy2; k++ {
		assert.Equal(t, x.re[k]*x.re[k]+x.im[k]*x.im[k], spectrum[k])
	}
}

// Port of CopyToPackedArray.
func TestFftData_CopyToPackedArray(t *testing.T) {
	x := testFftData()
	var packed [kFftLength]float32
	x.copyToPackedArray(&packed)

	assert.Equal(t, x.re[0], packed[0])
	assert.Equal(t, x.re[kFftLengthBy2], packed[1])
	for k := 1; k < kFftLengthBy2; k++ {
		assert.Equal(t, x.re[k], packed[2*k])
		assert.Equal(t, x.im[k], packed[2*k+1])
	}
}

// Port of CopyFromPackedArray.
func TestFftData_CopyFromPackedArray(t *testing.T) {
	xRef := testFftData()
	var packed [kFftLength]float32
	xRef.copyToPackedArray(&packed)
	var x fftData
	x.copyFromPackedArray(&packed)

	assert.Equal(t, xRef, x)
}
