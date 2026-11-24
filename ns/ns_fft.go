// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "github.com/pion/apm/internal/ooura"

// fftWrapper mirrors NrFft: the Ooura FFT of length fftSize.
type fftWrapper struct {
	bitReversalState [fftSize / 2]int
	tables           [fftSize / 2]float32
}

// newFFT mirrors the NrFft constructor: like upstream, a first transform
// with a zero bit reversal state initializes the tables.
func newFFT() *fftWrapper {
	f := &fftWrapper{}
	var tmp [fftSize]float32
	ooura.Rdft(fftSize, 1, tmp[:], f.bitReversalState[:], f.tables[:])

	return f
}

// forward mirrors NrFft::Fft. Like upstream, timeData is overwritten.
func (f *fftWrapper) forward(timeData *[fftSize]float32, re, im *[fftSizeBy2Plus1]float32) {
	ooura.Rdft(fftSize, 1, timeData[:], f.bitReversalState[:], f.tables[:])

	im[0] = 0
	re[0] = timeData[0]

	im[fftSizeBy2Plus1-1] = 0
	re[fftSizeBy2Plus1-1] = timeData[1]

	for i := 1; i < fftSizeBy2Plus1-1; i++ {
		re[i] = timeData[2*i]
		im[i] = timeData[2*i+1]
	}
}

// inverse mirrors NrFft::Ifft.
func (f *fftWrapper) inverse(re, im *[fftSizeBy2Plus1]float32, timeData *[fftSize]float32) {
	timeData[0] = re[0]
	timeData[1] = re[fftSizeBy2Plus1-1]
	for i := 1; i < fftSizeBy2Plus1-1; i++ {
		timeData[2*i] = re[i]
		timeData[2*i+1] = im[i]
	}
	ooura.Rdft(fftSize, -1, timeData[:], f.bitReversalState[:], f.tables[:])

	// Scale the output.
	const scaling = 2.0 / fftSize
	for i := range timeData {
		timeData[i] *= scaling
	}
}
