// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlock_Views(t *testing.T) {
	blk := newBlock(2, 3)
	assert.Len(t, blk.data, 2*3*kBlockSize)

	blk.view(1, 2)[0] = 7
	assert.InDelta(t, 7, blk.data[(1*3+2)*kBlockSize], 0)
	assert.Len(t, blk.view(0, 0), kBlockSize)
	assert.Equal(t, kBlockSize, cap(blk.view(0, 0)))
}

func TestBlock_SetNumChannelsZeroes(t *testing.T) {
	blk := newBlock(1, 2)
	blk.view(0, 1)[5] = 3

	blk.setNumChannels(4)
	assert.Equal(t, 4, blk.numChannels)
	assert.Equal(t, make([]float32, 4*kBlockSize), blk.data)

	blk.view(0, 0)[0] = 1
	blk.setNumChannels(1)
	assert.Equal(t, make([]float32, kBlockSize), blk.data)
}

func TestBlock_Swap(t *testing.T) {
	first, second := newBlock(1, 1), newBlock(3, 2)
	first.view(0, 0)[0] = 1
	second.view(2, 1)[0] = 2

	first.swap(second)
	assert.Equal(t, 3, first.numBands)
	assert.Equal(t, 2, first.numChannels)
	assert.InDelta(t, 2, first.view(2, 1)[0], 0)
	assert.Equal(t, 1, second.numBands)
	assert.InDelta(t, 1, second.view(0, 0)[0], 0)
}
