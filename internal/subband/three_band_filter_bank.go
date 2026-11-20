// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import "math"

const (
	kSparsity          = 4
	kStrideLog2        = 2
	kStride            = 1 << kStrideLog2
	kNumZeroFilters    = 2
	kFilterSize        = 4
	kMemorySize        = kFilterSize*kStride - 1
	kSubSampling       = 3
	kDctSize           = 3
	kNumNonZeroFilters = kSparsity*ThreeBandNumBands - kNumZeroFilters
)

// Public constants for 3-band filter bank (48 kHz path).
const (
	ThreeBandNumBands      = 3
	ThreeBandFullBandSize  = 480
	ThreeBandSplitBandSize = ThreeBandFullBandSize / ThreeBandNumBands
)

// Filter prototype coefficients (10 non-zero polyphase pieces × 4 taps).
// Verbatim from three_band_filter_bank.cc kFilterCoeffs.
func filterCoeffs() [kNumNonZeroFilters][kFilterSize]float32 {
	return [kNumNonZeroFilters][kFilterSize]float32{
		{-0.00047749, -0.00496888, +0.16547118, +0.00425496},
		{-0.00173287, -0.01585778, +0.14989004, +0.00994113},
		{-0.00304815, -0.02536082, +0.12154542, +0.01157993},
		{-0.00346946, -0.02587886, +0.04760441, +0.00607594},
		{-0.00154717, -0.01136076, +0.01387458, +0.00186353},
		{+0.00186353, +0.01387458, -0.01136076, -0.00154717},
		{+0.00607594, +0.04760441, -0.02587886, -0.00346946},
		{+0.00983212, +0.08543175, -0.02982767, -0.00383509},
		{+0.00994113, +0.14989004, -0.01585778, -0.00173287},
		{+0.00425496, +0.16547118, -0.00496888, -0.00047749},
	}
}

const (
	kZeroFilterIndex1 = 3
	kZeroFilterIndex2 = 9
)

func dctModulation() [kNumNonZeroFilters][kDctSize]float32 {
	sqrt3 := float32(math.Sqrt(3))

	return [kNumNonZeroFilters][kDctSize]float32{
		{2, 2, 2},
		{sqrt3, 0, -sqrt3},
		{1, -2, 1},
		{-1, 2, -1},
		{-sqrt3, 0, sqrt3},
		{-2, -2, -2},
		{-sqrt3, 0, sqrt3},
		{-1, 2, -1},
		{1, -2, 1},
		{sqrt3, 0, -sqrt3},
	}
}

// ThreeBandFilterBank splits a 480-sample full-band signal into 3
// 160-sample subbands and synthesizes back. One instance per channel.
type ThreeBandFilterBank struct {
	stateAnalysis  [kNumNonZeroFilters][kMemorySize]float32
	stateSynthesis [kNumNonZeroFilters][kMemorySize]float32
	filterCoeffs   [kNumNonZeroFilters][kFilterSize]float32
	dctModulation  [kNumNonZeroFilters][kDctSize]float32
}

// NewThreeBandFilterBank constructs a filter bank with zeroed state.
func NewThreeBandFilterBank() *ThreeBandFilterBank {
	return &ThreeBandFilterBank{filterCoeffs: filterCoeffs(), dctModulation: dctModulation()}
}

// filterCore implements the per-polyphase FIR convolution that powers
// both analysis and synthesis. Mirrors FilterCore in upstream.
func filterCore(
	filter *[kFilterSize]float32,
	in *[ThreeBandSplitBandSize]float32,
	inShift int,
	out *[ThreeBandSplitBandSize]float32,
	state *[kMemorySize]float32,
) {
	for i := range out {
		out[i] = 0
	}

	for k := range inShift {
		j := kMemorySize + k - inShift
		for i := range kFilterSize {
			out[k] += state[j] * filter[i]
			j -= kStride
		}
	}

	for k, shift := inShift, 0; k < kFilterSize*kStride; k, shift = k+1, shift+1 {
		loopLimit := min(1+(shift>>kStrideLog2), kFilterSize)
		j := shift
		for i := range loopLimit {
			out[k] += in[j] * filter[i]
			j -= kStride
		}
		j = kMemorySize + shift - loopLimit*kStride
		for i := loopLimit; i < kFilterSize; i++ {
			out[k] += state[j] * filter[i]
			j -= kStride
		}
	}

	for k, shift := kFilterSize*kStride, kFilterSize*kStride-inShift; k < ThreeBandSplitBandSize; k, shift = k+1, shift+1 {
		j := shift
		for i := range kFilterSize {
			out[k] += in[j] * filter[i]
			j -= kStride
		}
	}

	// Update state with the last kMemorySize samples of the input.
	copy(state[:], in[ThreeBandSplitBandSize-kMemorySize:])
}

// nonZeroFilterIndex maps a polyphase index to its filter, skipping the
// two all-zero filters. ok is false for those.
func nonZeroFilterIndex(index int) (filterIndex int, ok bool) {
	switch {
	case index == kZeroFilterIndex1 || index == kZeroFilterIndex2:
		return 0, false
	case index < kZeroFilterIndex1:
		return index, true
	case index < kZeroFilterIndex2:
		return index - 1, true
	default:
		return index - 2, true
	}
}

func checkAnalysisArgs(in []float32, out [][]float32) {
	if len(in) != ThreeBandFullBandSize {
		panic("subband: Analysis input must be 480 samples") //nolint:forbidigo // Invariant violated by the caller.
	}
	if len(out) != ThreeBandNumBands {
		panic("subband: Analysis output must have 3 bands") //nolint:forbidigo // Invariant violated by the caller.
	}
	for b := range ThreeBandNumBands {
		if len(out[b]) != ThreeBandSplitBandSize {
			panic("subband: each band must be 160 samples") //nolint:forbidigo // Invariant violated by the caller.
		}
	}
}

// Analysis splits one 480-sample input into 3 × 160-sample bands.
// out must be a slice with 3 inner slices, each of length 160.
func (f *ThreeBandFilterBank) Analysis(in []float32, out [][]float32) {
	checkAnalysisArgs(in, out)
	for b := range ThreeBandNumBands {
		clear(out[b])
	}

	for downsamplingIndex := range kSubSampling {
		var inSubsampled [ThreeBandSplitBandSize]float32
		for k := range ThreeBandSplitBandSize {
			inSubsampled[k] = in[(kSubSampling-1)-downsamplingIndex+kSubSampling*k]
		}

		for inShift := range kStride {
			filterIndex, ok := nonZeroFilterIndex(downsamplingIndex + inShift*kSubSampling)
			if !ok {
				continue
			}

			var outSubsampled [ThreeBandSplitBandSize]float32
			filterCore(&f.filterCoeffs[filterIndex], &inSubsampled, inShift, &outSubsampled, &f.stateAnalysis[filterIndex])

			for band := range ThreeBandNumBands {
				mod := f.dctModulation[filterIndex][band]
				for n := range ThreeBandSplitBandSize {
					out[band][n] += mod * outSubsampled[n]
				}
			}
		}
	}
}

// Synthesis merges 3 × 160-sample bands back into a 480-sample signal.
func (f *ThreeBandFilterBank) Synthesis(in [][]float32, out []float32) {
	if len(out) != ThreeBandFullBandSize {
		panic("subband: Synthesis output must be 480 samples") //nolint:forbidigo // Invariant violated by the caller.
	}
	if len(in) != ThreeBandNumBands {
		panic("subband: Synthesis input must have 3 bands") //nolint:forbidigo // Invariant violated by the caller.
	}
	for i := range out {
		out[i] = 0
	}

	for upsamplingIndex := range kSubSampling {
		for inShift := range kStride {
			filterIndex, ok := nonZeroFilterIndex(upsamplingIndex + inShift*kSubSampling)
			if !ok {
				continue
			}

			var inSubsampled [ThreeBandSplitBandSize]float32
			for band := range ThreeBandNumBands {
				mod := f.dctModulation[filterIndex][band]
				for n := range ThreeBandSplitBandSize {
					inSubsampled[n] += mod * in[band][n]
				}
			}

			var outSubsampled [ThreeBandSplitBandSize]float32
			filterCore(&f.filterCoeffs[filterIndex], &inSubsampled, inShift, &outSubsampled, &f.stateSynthesis[filterIndex])

			const kUpsamplingScaling = float32(kSubSampling)
			for k := range ThreeBandSplitBandSize {
				out[upsamplingIndex+kSubSampling*k] += kUpsamplingScaling * outSubsampled[k]
			}
		}
	}
}
