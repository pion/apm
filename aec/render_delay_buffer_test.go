// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAec3Fft_RoundTrip(t *testing.T) {
	fft := newAec3Fft()
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Deterministic test signal.
	var in, out [kFftLength]float32
	for i := range in {
		in[i] = float32(rng.NormFloat64())
	}
	var spec fftData
	fft.fwd(&in, &spec)
	fft.inv(&spec, &out)
	for i := range out {
		out[i] /= kFftLengthBy2
	}
	assert.InDeltaSlice(t, in[:], out[:], 1e-4)
}

func TestAec3Fft_ZeroPaddedHanningDC(t *testing.T) {
	fft := newAec3Fft()
	var x [kFftLengthBy2]float32
	for i := range x {
		x[i] = 1
	}
	var spec fftData
	fft.zeroPaddedFft(&x, windowHanning, &spec)
	var want float32
	for _, w := range kHanning64 {
		want += w
	}
	assert.InDelta(t, want, spec.re[0], 1e-3)
	assert.Zero(t, spec.im[0])
}

func TestRenderDelayBuffer_ReadsLatestBlock(t *testing.T) {
	buf := newRenderDelayBuffer(13)
	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // Deterministic test signal.

	var prev, cur [kFftLengthBy2]float32
	for range 5 {
		prev = cur
		for i := range cur {
			cur[i] = float32(rng.NormFloat64() * 1000)
		}
		buf.insertBlock(cur[:])
		buf.step()
	}

	// The latest block sits one slot past the read position, in the
	// filter's second partition.
	var want fftData
	newAec3Fft().paddedFft(&cur, &prev, windowRectangular, &want)
	view := buf.renderBuffer()
	fftBuf := view.getFftBuffer()
	latest := (view.position() + 1) % len(fftBuf)
	assert.Equal(t, want, fftBuf[latest][0])

	var power, sum [kFftLengthBy2Plus1]float32
	want.spectrum(&power)
	view.spectralSum(2, &sum)
	assert.Equal(t, power, sum, "the slot at the read position is still empty")

	buf.reset()
	view.spectralSum(13, &sum)
	assert.Equal(t, [kFftLengthBy2Plus1]float32{}, sum)
}
