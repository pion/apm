// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// downsampledRenderBuffer mirrors DownsampledRenderBuffer: a circular
// buffer of downsampled render samples. Samples are written towards
// lower indices, so reading forward goes back in time.
type downsampledRenderBuffer struct {
	ringIndex
	buffer []float32
}

func newDownsampledRenderBuffer(downsampledBufferSize int) *downsampledRenderBuffer {
	return &downsampledRenderBuffer{
		ringIndex: ringIndex{size: downsampledBufferSize},
		buffer:    make([]float32, downsampledBufferSize),
	}
}
