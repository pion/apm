// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRingIndex(t *testing.T) {
	ring := ringIndex{size: 5}

	assert.Equal(t, 1, ring.incIndex(0))
	assert.Equal(t, 0, ring.incIndex(4))
	assert.Equal(t, 4, ring.decIndex(0))
	assert.Equal(t, 3, ring.decIndex(4))
	assert.Equal(t, 3, ring.offsetIndex(1, -3))
	assert.Equal(t, 1, ring.offsetIndex(3, 3))

	ring.updateWriteIndex(-2)
	assert.Equal(t, 3, ring.write)
	ring.incWriteIndex()
	ring.incWriteIndex()
	assert.Equal(t, 0, ring.write)
	ring.decWriteIndex()
	assert.Equal(t, 4, ring.write)

	ring.updateReadIndex(7 - 5)
	assert.Equal(t, 2, ring.read)
	ring.decReadIndex()
	ring.decReadIndex()
	ring.decReadIndex()
	assert.Equal(t, 4, ring.read)
	ring.incReadIndex()
	assert.Equal(t, 0, ring.read)
}

func TestBuffers_Shapes(t *testing.T) {
	blocks := newBlockBuffer(4, 2, 3)
	assert.Len(t, blocks.buffer, 4)
	assert.Equal(t, 2, blocks.buffer[3].numBands)
	assert.Equal(t, 3, blocks.buffer[3].numChannels)
	blocks.buffer[0].view(0, 0)[0] = 1
	assert.Zero(t, blocks.buffer[1].view(0, 0)[0])

	ffts := newFftBuffer(4, 3)
	assert.Len(t, ffts.buffer, 4)
	assert.Len(t, ffts.buffer[0], 3)

	spectra := newSpectrumBuffer(4, 3)
	assert.Len(t, spectra.buffer, 4)
	assert.Len(t, spectra.buffer[0], 3)
	assert.Equal(t, 4, spectra.size)
}
