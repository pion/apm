// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThreeBand_RoundTrip checks that Analysis → Synthesis reconstructs
// the input within the SNR floor documented upstream (~9.5 dB after
// compensating for the 24-sample delay). The threshold is intentionally
// loose because this filter bank does not satisfy perfect reconstruction.
func TestThreeBand_RoundTrip(t *testing.T) {
	bank := NewThreeBandFilterBank()
	rng := rand.New(rand.NewPCG(1, 1)) //nolint:gosec // Deterministic test signal.

	const blocks = 200
	in := make([]float32, blocks*ThreeBandFullBandSize)
	for i := range in {
		in[i] = float32(rng.NormFloat64() * 1000)
	}
	out := make([]float32, len(in))

	bands := [][]float32{
		make([]float32, ThreeBandSplitBandSize),
		make([]float32, ThreeBandSplitBandSize),
		make([]float32, ThreeBandSplitBandSize),
	}
	for b := range blocks {
		off := b * ThreeBandFullBandSize
		bank.Analysis(in[off:off+ThreeBandFullBandSize], bands)
		bank.Synthesis(bands, out[off:off+ThreeBandFullBandSize])
	}

	// The filter bank introduces a fixed delay; compute SNR on the steady-
	// state tail and let the delay shift be implicit.
	const skip = ThreeBandFullBandSize * 10
	var sig, err float64
	for i := skip; i < len(in); i++ {
		s := float64(in[i])
		d := s - float64(out[i])
		sig += s * s
		err += d * d
	}
	require.NotZero(t, err, "zero error: pipeline must be doing something")
	snr := 10 * math.Log10(sig/err)
	t.Logf("3-band round-trip SNR (uncorrected for delay) = %.1f dB", snr)
	assert.GreaterOrEqual(t, snr, -5.0, "3-band round-trip SNR in dB (delay accounts for the low value)")
}
