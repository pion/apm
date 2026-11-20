// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package ns implements the WebRTC noise suppressor.
package ns

const (
	fftSize         = 256
	fftSizeBy2Plus1 = fftSize/2 + 1
	nsFrameSize     = 160
	overlapSize     = fftSize - nsFrameSize

	shortStartupPhaseBlocks = 50
	longStartupPhaseBlocks  = 200
	featureUpdateWindowSize = 500

	ltrFeatureThr   = 0.5
	binSizeLrt      = 0.1
	binSizeSpecFlat = 0.05
	binSizeSpecDiff = 0.1
)
