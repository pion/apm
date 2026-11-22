// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// blockDelayBuffer mirrors BlockDelayBuffer: it applies a fixed delay to
// the samples of a signal that is split in frequency bands.
type blockDelayBuffer struct {
	frameLength int
	delay       int
	// buf holds the delayed samples, per channel and band.
	buf        [][][]float32
	lastInsert int
}

func newBlockDelayBuffer(numChannels, numBands, frameLength, delaySamples int) *blockDelayBuffer {
	buffer := &blockDelayBuffer{
		frameLength: frameLength,
		delay:       delaySamples,
		buf:         make([][][]float32, numChannels),
	}
	for ch := range buffer.buf {
		buffer.buf[ch] = make([][]float32, numBands)
		for band := range buffer.buf[ch] {
			buffer.buf[ch][band] = make([]float32, delaySamples)
		}
	}

	return buffer
}

// delaySignal mirrors DelaySignal: it delays the samples of a frame,
// which is indexed by channel, band and sample.
func (b *blockDelayBuffer) delaySignal(frame [][][]float32) {
	if b.delay == 0 {
		return
	}

	start := b.lastInsert
	index := 0
	for ch := range b.buf {
		for band := range b.buf[ch] {
			index = start
			delayed := b.buf[ch][band]
			samples := frame[ch][band]
			for k := range b.frameLength {
				delayed[index], samples[k] = samples[k], delayed[index]
				if index < b.delay-1 {
					index++
				} else {
					index = 0
				}
			}
		}
	}

	b.lastInsert = index
}
