// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// newTestRenderBuffer returns a render buffer of the given size where
// every bin of the spectrum at position p of channel c holds 10*p + c + 1.
func newTestRenderBuffer(size, numChannels int) *renderBuffer {
	spectra := newSpectrumBuffer(size, numChannels)
	for position := range spectra.buffer {
		for channel := range spectra.buffer[position] {
			for k := range spectra.buffer[position][channel] {
				spectra.buffer[position][channel][k] = float32(10*position + channel + 1)
			}
		}
	}

	return newRenderBuffer(newBlockBuffer(size, 1, numChannels), spectra, newFftBuffer(size, numChannels))
}

func TestRenderBuffer_SpectralSum(t *testing.T) {
	render := newTestRenderBuffer(4, 2)
	render.spectrumBuf.read = 3

	// Positions 3 and 0, both channels: (31 + 32) + (1 + 2).
	var x2 [kFftLengthBy2Plus1]float32
	render.spectralSum(2, &x2)
	for _, value := range x2 {
		assert.InDelta(t, 66, value, 0)
	}

	// Shorter over position 3, longer over positions 3, 0 and 1.
	var shorter, longer [kFftLengthBy2Plus1]float32
	render.spectralSums(1, 3, &shorter, &longer)
	assert.InDelta(t, 63, shorter[0], 0)
	assert.InDelta(t, 63+3+23, longer[kFftLengthBy2], 0)
}

func TestRenderBuffer_Offsets(t *testing.T) {
	render := newTestRenderBuffer(4, 1)
	render.blockBuf.read = 3
	render.spectrumBuf.read = 1
	render.fftBuf.read = 1

	assert.Same(t, render.blockBuf.buffer[0], render.getBlock(1))
	assert.Same(t, render.blockBuf.buffer[2], render.getBlock(-1))
	assert.InDelta(t, 31, render.spectrum(2)[0][0], 0)
	assert.InDelta(t, 1, render.spectrum(-1)[0][0], 0)
	assert.Equal(t, 1, render.position())
	assert.Len(t, render.getFftBuffer(), 4)
	assert.Same(t, render.spectrumBuf, render.getSpectrumBuffer())
	assert.Same(t, render.blockBuf, render.getBlockBuffer())
}

func TestRenderBuffer_HeadroomAndActivity(t *testing.T) {
	render := newTestRenderBuffer(8, 1)

	render.fftBuf.write, render.fftBuf.read = 2, 5
	assert.Equal(t, 3, render.headroom())
	render.fftBuf.write, render.fftBuf.read = 6, 1
	assert.Equal(t, 3, render.headroom())
	render.fftBuf.write, render.fftBuf.read = 4, 4
	assert.Zero(t, render.headroom())

	assert.False(t, render.getRenderActivity())
	render.setRenderActivity(true)
	assert.True(t, render.getRenderActivity())
}
