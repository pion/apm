// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownsampledRenderBuffer_IndicesWrap(t *testing.T) {
	buffer := newDownsampledRenderBuffer(8)
	assert.Len(t, buffer.buffer, 8)

	assert.Equal(t, 1, buffer.incIndex(0))
	assert.Equal(t, 0, buffer.incIndex(7))
	assert.Equal(t, 7, buffer.decIndex(0))
	assert.Equal(t, 6, buffer.decIndex(7))
	assert.Equal(t, 5, buffer.offsetIndex(1, -4))
	assert.Equal(t, 1, buffer.offsetIndex(5, 4))

	buffer.updateWriteIndex(-3)
	assert.Equal(t, 5, buffer.write)
	buffer.incWriteIndex()
	buffer.incWriteIndex()
	buffer.incWriteIndex()
	assert.Equal(t, 0, buffer.write)
	buffer.decWriteIndex()
	assert.Equal(t, 7, buffer.write)

	buffer.updateReadIndex(10 - 8)
	assert.Equal(t, 2, buffer.read)
	buffer.decReadIndex()
	buffer.decReadIndex()
	buffer.decReadIndex()
	assert.Equal(t, 7, buffer.read)
	buffer.incReadIndex()
	assert.Equal(t, 0, buffer.read)
}
