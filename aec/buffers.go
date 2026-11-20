// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// ringIndex holds the size and the read and write indices that upstream's
// BlockBuffer, SpectrumBuffer and FftBuffer each declare.
type ringIndex struct {
	size  int
	write int
	read  int
}

func (r *ringIndex) incIndex(index int) int {
	if index < r.size-1 {
		return index + 1
	}

	return 0
}

func (r *ringIndex) decIndex(index int) int {
	if index > 0 {
		return index - 1
	}

	return r.size - 1
}

// offsetIndex moves index by offset, which may be negative but not
// below -size.
func (r *ringIndex) offsetIndex(index, offset int) int {
	return (r.size + index + offset) % r.size
}

func (r *ringIndex) updateWriteIndex(offset int) { r.write = r.offsetIndex(r.write, offset) }
func (r *ringIndex) incWriteIndex()              { r.write = r.incIndex(r.write) }
func (r *ringIndex) decWriteIndex()              { r.write = r.decIndex(r.write) }
func (r *ringIndex) updateReadIndex(offset int)  { r.read = r.offsetIndex(r.read, offset) }
func (r *ringIndex) incReadIndex()               { r.read = r.incIndex(r.read) }
func (r *ringIndex) decReadIndex()               { r.read = r.decIndex(r.read) }

// blockBuffer mirrors BlockBuffer: a circular buffer of blocks.
type blockBuffer struct {
	ringIndex
	buffer []*block
}

func newBlockBuffer(size, numBands, numChannels int) *blockBuffer {
	b := &blockBuffer{ringIndex: ringIndex{size: size}, buffer: make([]*block, size)}
	for i := range b.buffer {
		b.buffer[i] = newBlock(numBands, numChannels)
	}

	return b
}

// fftBuffer mirrors FftBuffer: a circular buffer of per-channel FftData.
type fftBuffer struct {
	ringIndex
	buffer [][]fftData
}

func newFftBuffer(size, numChannels int) *fftBuffer {
	b := &fftBuffer{ringIndex: ringIndex{size: size}, buffer: make([][]fftData, size)}
	for i := range b.buffer {
		b.buffer[i] = make([]fftData, numChannels)
	}

	return b
}

// spectrumBuffer mirrors SpectrumBuffer: a circular buffer of per-channel
// power spectra.
type spectrumBuffer struct {
	ringIndex
	buffer [][][kFftLengthBy2Plus1]float32
}

func newSpectrumBuffer(size, numChannels int) *spectrumBuffer {
	b := &spectrumBuffer{ringIndex: ringIndex{size: size}, buffer: make([][][kFftLengthBy2Plus1]float32, size)}
	for i := range b.buffer {
		b.buffer[i] = make([][kFftLengthBy2Plus1]float32, numChannels)
	}

	return b
}
