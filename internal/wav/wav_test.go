// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package wav

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip_PCM16(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.wav")

	const numSamples = 480 // 10 ms at 48 kHz
	src := make([]float32, numSamples)
	for i := range src {
		src[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 48000))
	}
	require.NoError(t, WriteAll(path, [][]float32{src}, 48000, PCM16))

	chans, rate, err := ReadAll(path)
	require.NoError(t, err)
	require.Equal(t, 48000, rate)
	require.Len(t, chans, 1)
	require.Len(t, chans[0], numSamples)
	// PCM16 quantization noise is bounded by 1/32768.
	const tol = 1.0 / 32768
	require.InDeltaSlice(t, src, chans[0], tol*2)
}

func TestRoundTrip_Float32(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rtf.wav")

	const numSamples = 480
	src := make([]float32, numSamples)
	for i := range src {
		src[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
	}
	require.NoError(t, WriteAll(path, [][]float32{src}, 48000, Float32))
	chans, _, err := ReadAll(path)
	require.NoError(t, err)
	require.Len(t, chans, 1)
	assert.Equal(t, src, chans[0], "float32 should be exact")
}

func TestRoundTrip_Stereo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stereo.wav")

	const numSamples = 160
	left := make([]float32, numSamples)
	right := make([]float32, numSamples)
	for i := range left {
		left[i] = float32(i) / numSamples
		right[i] = -float32(i) / numSamples
	}
	require.NoError(t, WriteAll(path, [][]float32{left, right}, 16000, PCM16))
	chans, rate, err := ReadAll(path)
	require.NoError(t, err)
	require.Equal(t, 16000, rate)
	require.Len(t, chans, 2)
	assert.Len(t, chans[0], numSamples)
	assert.Len(t, chans[1], numSamples)
}

func TestPCM16_ClipsOverrange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.wav")
	require.NoError(t, WriteAll(path, [][]float32{{2.0, -2.0, 0.5, -0.5}}, 16000, PCM16))
	chans, _, err := ReadAll(path)
	require.NoError(t, err)
	got := chans[0]
	// Clipped values round to +/-1.0 (within 1 LSB).
	assert.InDelta(t, 1.0, got[0], 1.0/32768*2, "overrange positive")
	assert.InDelta(t, -1.0, got[1], 1.0/32768*2, "overrange negative")
}
