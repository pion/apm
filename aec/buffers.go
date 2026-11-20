// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// fftBuffer mirrors FftBuffer: per-position vector of per-channel FftData.
type fftBuffer struct {
	size   int
	buffer [][]fftData
	write  int
	read   int
}

func newFftBuffer(size, numChannels int) *fftBuffer {
	b := &fftBuffer{size: size, buffer: make([][]fftData, size)}
	for i := range b.buffer {
		b.buffer[i] = make([]fftData, numChannels)
	}

	return b
}

func (b *fftBuffer) decIndex(index int) int {
	if index > 0 {
		return index - 1
	}

	return b.size - 1
}

func (b *fftBuffer) decWriteIndex() { b.write = b.decIndex(b.write) }
func (b *fftBuffer) decReadIndex()  { b.read = b.decIndex(b.read) }

// spectrumBuffer mirrors SpectrumBuffer: per-position vector of per-channel
// power spectrum arrays.
type spectrumBuffer struct {
	size   int
	buffer [][][kFftLengthBy2Plus1]float32
	write  int
	read   int
}

func newSpectrumBuffer(size, numChannels int) *spectrumBuffer {
	b := &spectrumBuffer{size: size, buffer: make([][][kFftLengthBy2Plus1]float32, size)}
	for i := range b.buffer {
		b.buffer[i] = make([][kFftLengthBy2Plus1]float32, numChannels)
	}

	return b
}

func (b *spectrumBuffer) incIndex(index int) int {
	if index < b.size-1 {
		return index + 1
	}

	return 0
}

func (b *spectrumBuffer) decIndex(index int) int {
	if index > 0 {
		return index - 1
	}

	return b.size - 1
}

func (b *spectrumBuffer) decWriteIndex() { b.write = b.decIndex(b.write) }
func (b *spectrumBuffer) decReadIndex()  { b.read = b.decIndex(b.read) }
