// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package subband

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// threeBandReferenceDelta allows for fused multiply-add on some
// architectures. Samples are on the int16 scale.
const threeBandReferenceDelta = 0.01

// loadThreeBandReference reads testdata/three_band_upstream.txt. Each
// line reads "name frame: samples" and is keyed by "name frame".
func loadThreeBandReference(t *testing.T) map[string][]float32 {
	t.Helper()
	file, err := os.Open("testdata/three_band_upstream.txt")
	require.NoError(t, err)
	defer func() { assert.NoError(t, file.Close()) }()

	reference := map[string][]float32{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		key, samples, found := strings.Cut(scanner.Text(), ":")
		require.True(t, found)
		for field := range strings.FieldsSeq(samples) {
			value, err := strconv.ParseFloat(field, 32)
			require.NoError(t, err)
			reference[key] = append(reference[key], float32(value))
		}
	}
	require.NoError(t, scanner.Err())

	return reference
}

// TestThreeBand_MatchesUpstream compares the bands and the reconstructed
// signal with the output of the upstream ThreeBandFilterBank at the
// pinned commit, fed the same generated noise.
func TestThreeBand_MatchesUpstream(t *testing.T) {
	reference := loadThreeBandReference(t)
	require.NotEmpty(t, reference)

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
