// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package aec is a Go port of WebRTC's AEC3 acoustic echo canceller.
//
// Canceller runs the port of the upstream EchoCanceller3: it estimates
// the delay of the echo, subtracts the echo with adaptive filters and
// suppresses what remains, at 16, 32 and 48 kHz, for any number of
// render and capture channels. The field trials, the neural residual
// echo estimator and the metrics histograms of upstream are not ported.
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

// log2TodB matches Log2TodB: the dB value of a power quantity that is
// expressed in log2.
func log2TodB(inLog2 float32) float32 {
	return float32(3.0102999566398121 * float64(inLog2))
}
