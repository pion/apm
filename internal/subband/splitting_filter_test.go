// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplittingFilter_RejectsBadBands(t *testing.T) {
	_, err := NewSplittingFilter(1, 4)
	assert.ErrorIs(t, err, ErrInvalidConfig, "4 bands")
	_, err = NewSplittingFilter(0, 2)
	assert.ErrorIs(t, err, ErrInvalidConfig, "0 channels")
}

func TestSplittingFilter_2Band(t *testing.T) {
	sf, err := NewSplittingFilter(1, 2)
	require.NoError(t, err)
	in := [][]float32{make([]float32, 320)}
	rng := rand.New(rand.NewPCG(3, 3)) //nolint:gosec // Deterministic test signal.
	for i := range in[0] {
		in[0][i] = float32(rng.NormFloat64() * 1000)
	}
	bands := [][][]float32{
		{make([]float32, 160)},
		{make([]float32, 160)},
	}
	require.NoError(t, sf.Analysis(in, bands))
	out := [][]float32{make([]float32, 320)}
	require.NoError(t, sf.Synthesis(bands, out))
	// Just confirm shape: full SNR check is in TestQMF_RoundTrip.
	assert.Len(t, out[0], 320)
}
