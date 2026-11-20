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
