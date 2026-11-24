// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ooura

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/require"
)

// TestRdft_MatchesUpstream compares the output with the output of
// WebRtc_rdft of length 256 at the pinned commit, bit for bit, with the
// tables initialized like NrFft. The input is four frames of noise of
// amplitude 1, 100, 10000 and 32768. Each line of the reference file is
// keyed by "w" for the cos/sin table, "fwd <frame>" for the output of the
// forward transform and "inv <frame>" for the output of the inverse
// transform on it.
func TestRdft_MatchesUpstream(t *testing.T) {
	const length = 256
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/fft4g_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}
	require.Len(t, reference, 9)

	ip := make([]int, length/2)
	table := make([]float32, length/2)
	Rdft(length, 1, make([]float32, length), ip, table)
	require.Equal(t, reference["w"], table)

	seed := uint32(1)
	for frame, amplitude := range []float32{1, 100, 10000, 32768} {
		a := make([]float32, length)
		for i := range a {
			seed = seed*1664525 + 1013904223
			a[i] = amplitude * float32(int32(seed)) * (1.0 / 2147483648.0) //nolint:gosec // The conversion wraps on purpose.
		}
		Rdft(length, 1, a, ip, table)
		require.Equal(t, reference[fmt.Sprintf("fwd %d", frame)], a, "fwd %d", frame)
		Rdft(length, -1, a, ip, table)
		require.Equal(t, reference[fmt.Sprintf("inv %d", frame)], a, "inv %d", frame)
	}
}
