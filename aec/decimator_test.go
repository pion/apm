// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

// produceDecimatedSinusoidalOutputPower ports the helper of the same
// name in decimator_unittest.cc.
func produceDecimatedSinusoidalOutputPower(
	sampleRateHz, downSamplingFactor int, sinusoidalFrequencyHz float32,
) (inputPower, outputPower float32) {
	const (
		numStartupBlocks = 50
		numBlocks        = 1000
	)
	subBlockSize := kBlockSize / downSamplingFactor

	// Produce a sinusoid of the specified frequency.
	input := make([]float32, kBlockSize*numBlocks)
	for k := range input {
		phase := 2 * math.Pi * float64(sinusoidalFrequencyHz) * float64(k) / float64(sampleRateHz)
		input[k] = 32767 * float32(math.Sin(phase))
	}

	dec := newDecimator(downSamplingFactor)
	output := make([]float32, subBlockSize*numBlocks)
	for k := range numBlocks {
		dec.decimate(input[k*kBlockSize:(k+1)*kBlockSize], output[k*subBlockSize:(k+1)*subBlockSize])
	}

	power := func(samples []float32) float32 {
		var sum float32
		for _, sample := range samples {
			sum += sample * sample
		}

		return sum / float32(len(samples))
	}

	return power(input[numStartupBlocks*kBlockSize:]), power(output[numStartupBlocks*subBlockSize:])
}

// Port of Decimator.NoLeakageFromUpperFrequencies: there is little
// aliasing from upper frequencies in the downsampling.
func TestDecimator_NoLeakageFromUpperFrequencies(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		for _, downSamplingFactor := range []int{4, 8} {
			inputPower, outputPower := produceDecimatedSinusoidalOutputPower(
				rate, downSamplingFactor, 3.0/8.0*float32(rate))
			assert.Greater(t, 0.0001*inputPower, outputPower, "rate %d, factor %d", rate, downSamplingFactor)
		}
	}
}

// decimatorReferenceDelta allows for fused multiply-add on some
// architectures. Samples are on the int16 scale.
const decimatorReferenceDelta = 0.05

// TestDecimator_MatchesUpstream compares the output with the output of
// the upstream Decimator at the pinned commit, fed the same generated
// noise. Each line of the reference file is keyed by "factor block".
func TestDecimator_MatchesUpstream(t *testing.T) {
	reference := map[string][]float32{}
	for _, entry := range refdata.Load(t, "testdata/decimator_upstream.txt") {
		reference[entry.Key] = entry.Samples
	}

	compared := 0
	for _, downSamplingFactor := range []int{4, 8} {
		dec := newDecimator(downSamplingFactor)
		in := make([]float32, kBlockSize)
		out := make([]float32, kBlockSize/downSamplingFactor)
		seed := uint32(1)
		for block := range 20 {
			for i := range in {
				seed = seed*1664525 + 1013904223
				in[i] = float32(int(seed>>16) - 32768)
			}
			dec.decimate(in, out)
			if want, ok := reference[fmt.Sprintf("%d %d", downSamplingFactor, block)]; ok {
				assert.InDeltaSlice(t, want, out, decimatorReferenceDelta, "factor %d, block %d", downSamplingFactor, block)
				compared++
			}
		}
	}
	assert.Equal(t, len(reference), compared)
}
