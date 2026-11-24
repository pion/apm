// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// renderBuffer mirrors RenderBuffer: the read access to the render
// buffers that the echo remover needs.
type renderBuffer struct {
	blockBuf       *blockBuffer
	spectrumBuf    *spectrumBuffer
	fftBuf         *fftBuffer
	renderActivity bool
}

func newRenderBuffer(blockBuf *blockBuffer, spectrumBuf *spectrumBuffer, fftBuf *fftBuffer) *renderBuffer {
	return &renderBuffer{blockBuf: blockBuf, spectrumBuf: spectrumBuf, fftBuf: fftBuf}
}

// getBlock mirrors GetBlock.
func (r *renderBuffer) getBlock(bufferOffsetBlocks int) *block {
	return r.blockBuf.buffer[r.blockBuf.offsetIndex(r.blockBuf.read, bufferOffsetBlocks)]
}

// spectrum mirrors Spectrum: the per-channel spectrum of one of the FFTs
// in the buffer.
func (r *renderBuffer) spectrum(bufferOffsetFfts int) [][kFftLengthBy2Plus1]float32 {
	return r.spectrumBuf.buffer[r.spectrumBuf.offsetIndex(r.spectrumBuf.read, bufferOffsetFfts)]
}

// getFftBuffer mirrors GetFftBuffer: the circular FFT buffer.
func (r *renderBuffer) getFftBuffer() [][]fftData {
	return r.fftBuf.buffer
}

// position mirrors Position: the current position in the circular buffer.
func (r *renderBuffer) position() int { return r.fftBuf.read }

// spectralSum mirrors SpectralSum: the sum of the spectra of numSpectra
// FFTs.
func (r *renderBuffer) spectralSum(numSpectra int, x2 *[kFftLengthBy2Plus1]float32) {
	*x2 = [kFftLengthBy2Plus1]float32{}
	r.addSpectra(r.spectrumBuf.read, numSpectra, x2)
}

// spectralSums mirrors SpectralSums: the sums of the spectra for two
// numbers of FFTs.
func (r *renderBuffer) spectralSums(
	numSpectraShorter, numSpectraLonger int, x2Shorter, x2Longer *[kFftLengthBy2Plus1]float32,
) {
	*x2Shorter = [kFftLengthBy2Plus1]float32{}
	position := r.addSpectra(r.spectrumBuf.read, numSpectraShorter, x2Shorter)
	*x2Longer = *x2Shorter
	r.addSpectra(position, numSpectraLonger-numSpectraShorter, x2Longer)
}

// addSpectra adds the spectra of every channel of numSpectra FFTs to x2,
// starting at position. It returns the position after the last one.
func (r *renderBuffer) addSpectra(position, numSpectra int, x2 *[kFftLengthBy2Plus1]float32) int {
	for range numSpectra {
		for _, channelSpectrum := range r.spectrumBuf.buffer[position] {
			for k := range x2 {
				x2[k] += channelSpectrum[k] //nolint:gosec // G602 false positive on a range loop.
			}
		}
		position = r.spectrumBuf.incIndex(position)
	}

	return position
}

// getRenderActivity mirrors GetRenderActivity.
func (r *renderBuffer) getRenderActivity() bool { return r.renderActivity }

// setRenderActivity mirrors SetRenderActivity.
func (r *renderBuffer) setRenderActivity(activity bool) { r.renderActivity = activity }

// headroom mirrors Headroom: the distance between the write and the read
// positions. Both indices decrease over time.
func (r *renderBuffer) headroom() int {
	if r.fftBuf.write <= r.fftBuf.read {
		return r.fftBuf.read - r.fftBuf.write
	}

	return r.fftBuf.size - r.fftBuf.write + r.fftBuf.read
}

// getSpectrumBuffer mirrors GetSpectrumBuffer.
func (r *renderBuffer) getSpectrumBuffer() *spectrumBuffer { return r.spectrumBuf }

// getBlockBuffer mirrors GetBlockBuffer.
func (r *renderBuffer) getBlockBuffer() *blockBuffer { return r.blockBuf }
