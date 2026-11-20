// SPDX-FileCopyrightText: 2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// block mirrors Block: one or more channels of 4 ms of audio, split in
// one or more 16 kHz frequency bands.
type block struct {
	numBands    int
	numChannels int
	data        []float32
}

func newBlock(numBands, numChannels int) *block {
	return &block{
		numBands:    numBands,
		numChannels: numChannels,
		data:        make([]float32, numBands*numChannels*kBlockSize),
	}
}

// setNumChannels changes the number of channels and zeroes all samples.
func (b *block) setNumChannels(numChannels int) {
	b.numChannels = numChannels
	size := b.numBands * numChannels * kBlockSize
	if cap(b.data) < size {
		b.data = make([]float32, size)
	}
	b.data = b.data[:size]
	clear(b.data)
}

// view returns the kBlockSize samples of a band of a channel.
func (b *block) view(band, channel int) []float32 {
	index := (band*b.numChannels + channel) * kBlockSize

	return b.data[index : index+kBlockSize : index+kBlockSize]
}

// swap lets two blocks swap audio data.
func (b *block) swap(other *block) {
	*b, *other = *other, *b
}
