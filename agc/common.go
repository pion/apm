// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

// Like upstream, the float constants are float32: their arithmetic rounds
// like float operations.
const (
	kMinFloatS16Value    float32 = -32768.0
	kMaxFloatS16Value    float32 = 32767.0
	kMaxAbsFloatS16Value float32 = 32768.0

	kFrameDurationMs                 = 10
	kSubFramesInFrame                = 20
	kMaxSamplesPerChannel            = 480
	kInputLevelScalingFactor float32 = 32768.0
	kMaxInputLevelLinear     float32 = 36766.300710566735

	kInterpolatedGainCurveKneePoints       = 22
	kInterpolatedGainCurveBeyondKneePoints = 10
	kInterpolatedGainCurveTotalPoints      = kInterpolatedGainCurveKneePoints + kInterpolatedGainCurveBeyondKneePoints
)
