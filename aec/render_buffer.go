// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// renderBuffer bundles the render-side circular buffers and exposes
// the read API the adaptive filter needs. Mirrors RenderBuffer.
type renderBuffer struct {
	spectrumBuf *spectrumBuffer
	fftBuf      *fftBuffer
}

func newRenderBuffer(spectrumBuf *spectrumBuffer, fftBuf *fftBuffer) *renderBuffer {
	return &renderBuffer{spectrumBuf: spectrumBuf, fftBuf: fftBuf}
}

// getFftBuffer returns the underlying FFT ring buffer slice (the
// adaptive filter walks it).
func (r *renderBuffer) getFftBuffer() [][]fftData {
	return r.fftBuf.buffer
}

// position returns the current read index in the FFT/spectrum buffers.
func (r *renderBuffer) position() int { return r.fftBuf.read }

// spectralSum sums the per-channel power spectra over the most recent
// numSpectra FFTs into sum.
func (r *renderBuffer) spectralSum(numSpectra int, sum *[kFftLengthBy2Plus1]float32) {
	*sum = [kFftLengthBy2Plus1]float32{}
	pos := r.spectrumBuf.read
	for range numSpectra {
		for _, ch := range r.spectrumBuf.buffer[pos] {
			for k := range kFftLengthBy2Plus1 {
				sum[k] += ch[k]
			}
		}
		pos = r.spectrumBuf.incIndex(pos)
	}
}
