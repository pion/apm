// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import "github.com/pion/apm/internal/dsp/fft"

// fftWrapper exposes the kFftSize FFT used by NS using the internal dsp/fft
// plan. Mirrors upstream NrFft.
type fftWrapper struct {
	plan *fft.Plan
}

func newFFT() *fftWrapper { return &fftWrapper{plan: fft.New(fftSize)} }

// forward computes |X|+jY from the time-domain frame. Output arrays are
// of length fftSizeBy2Plus1, with imag[0] = imag[N/2] = 0.
func (f *fftWrapper) forward(timeData *[fftSize]float32, re, im *[fftSizeBy2Plus1]float32) {
	f.plan.Forward(timeData[:], re[:], im[:])
}

// inverse reconstructs the time-domain frame from the half spectrum. Out
// length is fftSize. The dsp/fft Inverse normalizes by 1/N which matches
// the math; no extra 2/N step is needed.
func (f *fftWrapper) inverse(re, im *[fftSizeBy2Plus1]float32, timeData *[fftSize]float32) {
	f.plan.Inverse(re[:], im[:], timeData[:])
}
