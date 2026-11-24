// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// fftData mirrors FftData: real and imaginary halves of a 128-point real
// FFT, indexed [0..kFftLengthBy2]. Imag[0] = Imag[kFftLengthBy2] = 0.
type fftData struct {
	re [kFftLengthBy2Plus1]float32
	im [kFftLengthBy2Plus1]float32
}

// clear zeros both halves.
func (f *fftData) clear() {
	f.re = [kFftLengthBy2Plus1]float32{}
	f.im = [kFftLengthBy2Plus1]float32{}
}

// spectrum computes |X|² into the supplied buffer of length kFftLengthBy2Plus1.
func (f *fftData) spectrum(power *[kFftLengthBy2Plus1]float32) {
	for k := range kFftLengthBy2Plus1 {
		power[k] = f.re[k]*f.re[k] + f.im[k]*f.im[k]
	}
}

// copyFromPackedArray mirrors FftData::CopyFromPackedArray: it reads the
// output of the Ooura FFT.
func (f *fftData) copyFromPackedArray(v *[kFftLength]float32) {
	f.re[0] = v[0]
	f.re[kFftLengthBy2] = v[1]
	f.im[0] = 0
	f.im[kFftLengthBy2] = 0
	for k, j := 1, 2; k < kFftLengthBy2; k, j = k+1, j+2 {
		f.re[k] = v[j]
		f.im[k] = v[j+1]
	}
}

// copyToPackedArray mirrors FftData::CopyToPackedArray: it writes the
// input of the inverse Ooura FFT.
func (f *fftData) copyToPackedArray(v *[kFftLength]float32) {
	v[0] = f.re[0]
	v[1] = f.re[kFftLengthBy2]
	for k, j := 1, 2; k < kFftLengthBy2; k, j = k+1, j+2 {
		v[j] = f.re[k]
		v[j+1] = f.im[k]
	}
}
