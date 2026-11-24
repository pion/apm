// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ooura

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/require"
)

// TestFft128_MatchesUpstream compares the output with the output of
// OouraFft on the C path at the pinned commit, bit for bit. The input is
// four frames of noise of amplitude 1, 100, 10000 and 32768. Each line
// of the reference file is keyed by "fwd <frame>" for the output of Fft
// and "inv <frame>" for the output of InverseFft on it.
func TestFft128_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/fft128_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	require.Len(t, reference, 8)

	seed := uint32(1)
	for frame, amplitude := range []float32{1, 100, 10000, 32768} {
		var a [128]float32
		for i := range a {
			seed = seed*1664525 + 1013904223
			a[i] = amplitude * float32(int32(seed)) * (1.0 / 2147483648.0) //nolint:gosec // The conversion wraps on purpose.
		}
		Fft128(&a)
		require.Equal(t, reference[fmt.Sprintf("fwd %d", frame)], a[:], "fwd %d", frame)
		InverseFft128(&a)
		require.Equal(t, reference[fmt.Sprintf("inv %d", frame)], a[:], "inv %d", frame)
	}
}
