// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// downsampledRenderBuffer mirrors DownsampledRenderBuffer: a circular
// buffer of downsampled render samples. Samples are written towards
// lower indices, so reading forward goes back in time.
type downsampledRenderBuffer struct {
	size   int
	buffer []float32
	write  int
	read   int
}

func newDownsampledRenderBuffer(downsampledBufferSize int) *downsampledRenderBuffer {
	return &downsampledRenderBuffer{
		size:   downsampledBufferSize,
		buffer: make([]float32, downsampledBufferSize),
	}
}

func (b *downsampledRenderBuffer) incIndex(index int) int {
	if index < b.size-1 {
		return index + 1
	}

	return 0
}

func (b *downsampledRenderBuffer) decIndex(index int) int {
	if index > 0 {
		return index - 1
	}

	return b.size - 1
}

// offsetIndex moves index by offset, which may be negative but not
// below -size.
func (b *downsampledRenderBuffer) offsetIndex(index, offset int) int {
	return (b.size + index + offset) % b.size
}

func (b *downsampledRenderBuffer) updateWriteIndex(offset int) {
	b.write = b.offsetIndex(b.write, offset)
}
func (b *downsampledRenderBuffer) incWriteIndex() { b.write = b.incIndex(b.write) }
func (b *downsampledRenderBuffer) decWriteIndex() { b.write = b.decIndex(b.write) }

func (b *downsampledRenderBuffer) updateReadIndex(offset int) {
	b.read = b.offsetIndex(b.read, offset)
}
func (b *downsampledRenderBuffer) incReadIndex() { b.read = b.incIndex(b.read) }
func (b *downsampledRenderBuffer) decReadIndex() { b.read = b.decIndex(b.read) }
