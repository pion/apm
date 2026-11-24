// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// blockFramer mirrors BlockFramer for one band of one channel: it forms
// 80-sample sub-frames out of 64-sample blocks. It starts with one block
// of zeros, so its output lags its input by kBlockSize samples.
type blockFramer struct {
	buffer [kBlockSize]float32
	size   int
}

func newBlockFramer() *blockFramer {
	return &blockFramer{size: kBlockSize}
}

// insertBlock mirrors InsertBlock. The buffer must be empty.
func (f *blockFramer) insertBlock(block []float32) {
	f.size = copy(f.buffer[:], block[:kBlockSize])
}

// insertBlockAndExtractSubFrame mirrors InsertBlockAndExtractSubFrame.
// At least kSubFrameLength-kBlockSize samples must be buffered.
func (f *blockFramer) insertBlockAndExtractSubFrame(block, subFrame []float32) {
	samplesToFrame := kSubFrameLength - f.size
	copy(subFrame, f.buffer[:f.size])
	copy(subFrame[f.size:kSubFrameLength], block[:samplesToFrame])
	f.size = copy(f.buffer[:], block[samplesToFrame:kBlockSize])
}

// reset restores the initial block of zeros.
func (f *blockFramer) reset() {
	f.buffer = [kBlockSize]float32{}
	f.size = kBlockSize
}
