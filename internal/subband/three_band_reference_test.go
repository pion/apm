// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package subband

import (
	"fmt"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

// threeBandReferenceDelta allows for fused multiply-add on some
// architectures. Samples are on the int16 scale.
const threeBandReferenceDelta = 0.01

// TestThreeBand_MatchesUpstream compares the bands and the reconstructed
// signal with the output of the upstream ThreeBandFilterBank at the
// pinned commit, fed the same generated noise.
func TestThreeBand_MatchesUpstream(t *testing.T) {
	// Each line of the reference file is keyed by "name frame".
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/three_band_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	bank := NewThreeBandFilterBank()
	in := make([]float32, ThreeBandFullBandSize)
	out := make([]float32, ThreeBandFullBandSize)
	bands := make([][]float32, ThreeBandNumBands)
	for band := range bands {
		bands[band] = make([]float32, ThreeBandSplitBandSize)
	}

	seed := uint32(1)
	compared := 0
	for frame := range 6 {
		for i := range in {
			seed = seed*1664525 + 1013904223
			in[i] = float32(int(seed>>16) - 32768)
		}
		bank.Analysis(in, bands)
		bank.Synthesis(bands, out)

		for band := range bands {
			if want, ok := reference[fmt.Sprintf("band%d %d", band, frame)]; ok {
				assert.InDeltaSlice(t, want, bands[band], threeBandReferenceDelta, "frame %d, band %d", frame, band)
				compared++
			}
		}
		if want, ok := reference[fmt.Sprintf("out %d", frame)]; ok {
			assert.InDeltaSlice(t, want, out, threeBandReferenceDelta, "frame %d, output", frame)
			compared++
		}
	}
	assert.Equal(t, len(reference), compared)
}
