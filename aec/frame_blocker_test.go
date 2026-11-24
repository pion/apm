// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const numSubFramesToProcess = 20

// computeSampleValue matches ComputeSampleValue in
// frame_blocker_unittest.cc for band 0 of channel 0.
func computeSampleValue(chunkCounter, chunkSize, sampleIndex, offset int) float32 {
	return float32(max(chunkCounter*chunkSize+sampleIndex+offset, 0))
}

func fillSubFrame(subFrameCounter, offset int, subFrame []float32) {
	for i := range subFrame {
		subFrame[i] = computeSampleValue(subFrameCounter, kSubFrameLength, i, offset)
	}
}

func referenceBlock(blockCounter int) []float32 {
	block := make([]float32, kBlockSize)
	for i := range block {
		block[i] = computeSampleValue(blockCounter, kBlockSize, i, 0)
	}

	return block
}

// Port of RunBlockerTest.
func TestFrameBlocker_FormsBlocks(t *testing.T) {
	var blocker frameBlocker
	subFrame := make([]float32, kSubFrameLength)
	block := make([]float32, kBlockSize)
	blockCounter := 0

	for i := range numSubFramesToProcess {
		fillSubFrame(i, 0, subFrame)
		blocker.insertSubFrameAndExtractBlock(subFrame, block)
		assert.Equal(t, referenceBlock(blockCounter), block)
		blockCounter++

		assert.Equal(t, (i+1)%4 == 0, blocker.isBlockAvailable(), "sub-frame %d", i)
		if blocker.isBlockAvailable() {
			blocker.extractBlock(block)
			assert.Equal(t, referenceBlock(blockCounter), block)
			blockCounter++
		}
	}
}

// Port of RunBlockerAndFramerTest.
func TestFrameBlockerAndBlockFramer(t *testing.T) {
	var blocker frameBlocker
	framer := newBlockFramer()
	input := make([]float32, kSubFrameLength)
	output := make([]float32, kSubFrameLength)
	want := make([]float32, kSubFrameLength)
	block := make([]float32, kBlockSize)

	for i := range numSubFramesToProcess {
		fillSubFrame(i, 0, input)
		blocker.insertSubFrameAndExtractBlock(input, block)
		framer.insertBlockAndExtractSubFrame(block, output)

		assert.Equal(t, (i+1)%4 == 0, blocker.isBlockAvailable(), "sub-frame %d", i)
		if blocker.isBlockAvailable() {
			blocker.extractBlock(block)
			framer.insertBlock(block)
		}
		if i > 1 {
			fillSubFrame(i, -kBlockSize, want)
			assert.Equal(t, want, output, "sub-frame %d", i)
		}
	}
}

func TestFrameBlockerAndBlockFramer_Reset(t *testing.T) {
	var blocker frameBlocker
	framer := newBlockFramer()
	subFrame := make([]float32, kSubFrameLength)
	block := make([]float32, kBlockSize)

	fillSubFrame(1, 0, subFrame)
	blocker.insertSubFrameAndExtractBlock(subFrame, block)
	framer.insertBlockAndExtractSubFrame(block, subFrame)
	blocker.reset()
	framer.reset()

	fillSubFrame(1, 0, subFrame)
	blocker.insertSubFrameAndExtractBlock(subFrame, block)
	assert.Equal(t, subFrame[:kBlockSize], block)
	framer.insertBlockAndExtractSubFrame(block, subFrame)
	assert.Equal(t, make([]float32, kBlockSize), subFrame[:kBlockSize])
}
