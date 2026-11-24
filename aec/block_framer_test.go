// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// framerSampleValue matches ComputeSampleValue in block_framer_unittest.cc
// for band 0 of channel 0.
func framerSampleValue(chunkCounter, chunkSize, sampleIndex, offset int) float32 {
	return float32(100 + chunkCounter*chunkSize + sampleIndex + offset)
}

// Port of RunFramerTest, behind FrameBitexactness, for one band of one
// channel: the Go framer handles a single band of a single channel.
func TestBlockFramer_FrameBitexactness(t *testing.T) {
	const numSubFramesToProcess = 10
	framer := newBlockFramer()
	block := make([]float32, kBlockSize)
	subFrame := make([]float32, kSubFrameLength)
	want := make([]float32, kSubFrameLength)
	fillBlock := func(blockCounter int) {
		for i := range block {
			block[i] = framerSampleValue(blockCounter, kBlockSize, i, 0)
		}
	}

	blockIndex := 0
	for i := range numSubFramesToProcess {
		fillBlock(blockIndex)
		blockIndex++
		framer.insertBlockAndExtractSubFrame(block, subFrame)
		if i > 1 {
			for k := range want {
				want[k] = framerSampleValue(i, kSubFrameLength, k, -kBlockSize)
			}
			assert.Equal(t, want, subFrame, "sub-frame %d", i)
		}

		if (i+1)%4 == 0 {
			fillBlock(blockIndex)
			blockIndex++
			framer.insertBlock(block)
		}
	}
}
