// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package aec exposes a Go port of WebRTC's AEC3 acoustic echo canceller.
//
// The single-channel 16 kHz path is functional: a frequency-domain
// partitioned-block adaptive filter (AdaptiveFirFilter) driven by the
// coarse NLMS gain rule (CoarseFilterUpdateGain) subtracts the
// loudspeaker echo from the microphone. Upstream's dual coarse/refined
// filter design, AecState, suppression gain, matched-filter delay
// estimation, and multi-band/multi-channel paths are not yet ported;
// expect the residual echo to remain audible.
package aec

import "math"

const (
	kFftLengthBy2      = 64
	kFftLengthBy2Plus1 = kFftLengthBy2 + 1
	kFftLength         = 2 * kFftLengthBy2

	kFrameSize      = 160
	kSubFrameLength = kFrameSize / 2

	kBlockSize     = kFftLengthBy2
	kBlockSizeLog2 = 6
	kBlockSizeMs   = kFftLengthBy2 * 1000 / 16000

	kNumBlocksPerSecond = 250

	kMatchedFilterWindowSizeSubBlocks         = 32
	kMatchedFilterAlignmentShiftSizeSubBlocks = kMatchedFilterWindowSizeSubBlocks * 3 / 4
)

// numBandsForRate matches NumBandsForRate.
func numBandsForRate(sampleRateHz int) int {
	return sampleRateHz / 16000
}

// getTimeDomainLength matches GetTimeDomainLength.
func getTimeDomainLength(filterLengthBlocks int) int {
	return filterLengthBlocks * kFftLengthBy2
}

// getDownSampledBufferSize matches GetDownSampledBufferSize.
func getDownSampledBufferSize(downSamplingFactor, numMatchedFilters int) int {
	return kBlockSize / downSamplingFactor *
		(kMatchedFilterAlignmentShiftSizeSubBlocks*numMatchedFilters +
			kMatchedFilterWindowSizeSubBlocks + 1)
}

// getRenderDelayBufferSize matches GetRenderDelayBufferSize.
func getRenderDelayBufferSize(downSamplingFactor, numMatchedFilters, filterLengthBlocks int) int {
	return getDownSampledBufferSize(downSamplingFactor, numMatchedFilters)/
		(kBlockSize/downSamplingFactor) +
		filterLengthBlocks + 1
}

// fastApproxLog2f matches FastApproxLog2f: it reads the exponent of the
// float and removes the bias.
func fastApproxLog2f(in float32) float32 {
	out := float32(math.Float32bits(in))
	out *= 1.1920929e-7 // 1/2^23

	return out - 126.942695
}
