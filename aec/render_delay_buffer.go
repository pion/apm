// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// renderDelayBuffer holds the recent render history as padded FFTs and
// per-channel power spectra, and exposes a renderBuffer view onto them.
//
// This is a simplified port: upstream's RenderDelayBuffer also handles
// alignment with the matched-filter delay estimate, downsampled buffers
// for the delay estimator, and API-call jitter compensation. For a
// single-channel single-band path with no delay tracking, those moving
// parts are not required to drive the adaptive filter.
type renderDelayBuffer struct {
	fft         *aec3Fft
	fftBuf      *fftBuffer
	spectrumBuf *spectrumBuffer
	view        *renderBuffer
	xOld        [kFftLengthBy2]float32 // saved previous 64-sample render block
}

func newRenderDelayBuffer(filterLengthBlocks int) *renderDelayBuffer {
	// Buffer needs to be large enough to span the filter plus a small
	// safety margin (mirrors GetRenderDelayBufferSize for 1-band/1-channel
	// configurations).
	size := max(
		getRenderDelayBufferSize(4, 4, filterLengthBlocks), // 4x down-sampling, 4 matched filters.
		filterLengthBlocks+2,
	)
	r := &renderDelayBuffer{
		fft:         newAec3Fft(),
		fftBuf:      newFftBuffer(size, 1),
		spectrumBuf: newSpectrumBuffer(size, 1),
	}
	r.view = newRenderBuffer(r.spectrumBuf, r.fftBuf)

	return r
}

// insertBlock writes a 64-sample far-end block into the buffer at the
// write position, FFTs it (padded with the previous block), computes its power
// spectrum, and advances the write index. Read position is advanced
// after the subtractor has consumed the block.
func (r *renderDelayBuffer) insertBlock(farBlock []float32) {
	var x [kFftLengthBy2]float32
	copy(x[:], farBlock)
	r.fft.paddedFft(&x, &r.xOld, windowRectangular, &r.fftBuf.buffer[r.fftBuf.write][0])
	r.fftBuf.buffer[r.fftBuf.write][0].spectrum(&r.spectrumBuf.buffer[r.spectrumBuf.write][0])
	r.xOld = x

	// As upstream, writes go backward so the adaptive filter walks older
	// partitions by incrementing the index. Reads move the same way.
	r.fftBuf.decWriteIndex()
	r.spectrumBuf.decWriteIndex()
}

// step advances the read position by one block.
func (r *renderDelayBuffer) step() {
	r.fftBuf.decReadIndex()
	r.spectrumBuf.decReadIndex()
}

// renderBuffer returns the read-side view onto the buffer for use by
// the subtractor.
func (r *renderDelayBuffer) renderBuffer() *renderBuffer { return r.view }

// reset zeros all state.
func (r *renderDelayBuffer) reset() {
	r.xOld = [kFftLengthBy2]float32{}
	for i := range r.fftBuf.buffer {
		for j := range r.fftBuf.buffer[i] {
			r.fftBuf.buffer[i][j].clear()
		}
	}
	for i := range r.spectrumBuf.buffer {
		for j := range r.spectrumBuf.buffer[i] {
			r.spectrumBuf.buffer[i][j] = [kFftLengthBy2Plus1]float32{}
		}
	}
	r.fftBuf.write = 0
	r.fftBuf.read = 0
	r.spectrumBuf.write = 0
	r.spectrumBuf.read = 0
}
