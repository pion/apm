// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// frameBlocker mirrors FrameBlocker for one band of one channel: it
// forms 64-sample blocks out of 80-sample sub-frames.
type frameBlocker struct {
	buffer [kBlockSize]float32
	size   int
}

// insertSubFrameAndExtractBlock mirrors InsertSubFrameAndExtractBlock.
// At most kBlockSize-16 samples may be buffered when it is called.
func (b *frameBlocker) insertSubFrameAndExtractBlock(subFrame, block []float32) {
	samplesToBlock := kBlockSize - b.size
	copy(block, b.buffer[:b.size])
	copy(block[b.size:kBlockSize], subFrame[:samplesToBlock])
	b.size = copy(b.buffer[:], subFrame[samplesToBlock:kSubFrameLength])
}

// isBlockAvailable mirrors IsBlockAvailable.
func (b *frameBlocker) isBlockAvailable() bool {
	return b.size == kBlockSize
}

// extractBlock mirrors ExtractBlock. A block must be available.
func (b *frameBlocker) extractBlock(block []float32) {
	copy(block, b.buffer[:])
	b.size = 0
}

// reset drops the buffered samples.
func (b *frameBlocker) reset() {
	b.size = 0
}
