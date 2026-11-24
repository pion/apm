// SPDX-FileCopyrightText: 2015-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testRandom mirrors webrtc::Random, the xorshift generator the upstream
// tests use.
type testRandom struct {
	state uint64
}

//nolint:unparam // Upstream tests all seed with 42 so far.
func newTestRandom(seed uint64) *testRandom { return &testRandom{state: seed} }

func (r *testRandom) nextOutput() uint64 {
	r.state ^= r.state >> 12
	r.state ^= r.state << 25
	r.state ^= r.state >> 27

	return r.state * 2685821657736338717
}

// randFloat mirrors Rand<float>: a number in [0, 1).
func (r *testRandom) randFloat() float32 {
	result := float64(r.nextOutput() - 1)

	return float32(result / float64(0xFFFFFFFFFFFFFFFF))
}

// randomizeSampleVector mirrors RandomizeSampleVector with the default
// amplitude.
func randomizeSampleVector(random *testRandom, v []float32) {
	randomizeSampleVectorAmplitude(random, v, 32767)
}

// randomizeSampleVectorAmplitude mirrors RandomizeSampleVector.
func randomizeSampleVectorAmplitude(random *testRandom, v []float32, amplitude float32) {
	for i := range v {
		v[i] = 2*amplitude*random.randFloat() - amplitude
	}
}

// testDelayBuffer mirrors DelayBuffer<float>.
type testDelayBuffer struct {
	buffer          []float32
	nextInsertIndex int
}

func newTestDelayBuffer(delay int) *testDelayBuffer {
	return &testDelayBuffer{buffer: make([]float32, delay)}
}

// delay produces a delayed copy of x.
func (b *testDelayBuffer) delay(x, xDelayed []float32) {
	if len(b.buffer) == 0 {
		copy(xDelayed, x)

		return
	}
	for k := range x {
		xDelayed[k] = b.buffer[b.nextInsertIndex]
		b.buffer[b.nextInsertIndex] = x[k]
		b.nextInsertIndex = (b.nextInsertIndex + 1) % len(b.buffer)
	}
}

// Port of FloatDelayBuffer.
func TestTestDelayBuffer(t *testing.T) {
	const delay = 10
	const blockSize = 50
	buffer := newTestDelayBuffer(delay)
	v := make([]float32, 1000)
	for k := range v {
		v[k] = float32(k)
	}
	vDelayed := slices.Clone(v)
	for k := 0; k < len(v); k += blockSize {
		buffer.delay(v[k:k+blockSize], vDelayed[k:k+blockSize])
	}
	for k := delay; k < len(v); k++ {
		assert.Equal(t, v[k-delay], vDelayed[k])
	}
}

// Port of RandomizeSampleVector.
func TestRandomizeSampleVector(t *testing.T) {
	random := newTestRandom(42)
	v := make([]float32, 50)
	vRef := slices.Clone(v)
	randomizeSampleVector(random, v)
	assert.NotEqual(t, vRef, v)
	vRef = slices.Clone(v)
	randomizeSampleVector(random, v)
	assert.NotEqual(t, vRef, v)
}

// Port of RandomizeSampleVectorWithAmplitude.
func TestRandomizeSampleVectorAmplitude(t *testing.T) {
	random := newTestRandom(42)
	v := make([]float32, 50)
	for _, amplitude := range []float32{1000, 100} {
		randomizeSampleVectorAmplitude(random, v, amplitude)
		assert.GreaterOrEqual(t, amplitude, slices.Max(v))
		assert.LessOrEqual(t, -amplitude, slices.Min(v))
	}
}
