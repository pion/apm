// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package ns implements the WebRTC noise suppressor.
package ns

const (
	fftSize         = 256
	fftSizeBy2Plus1 = fftSize/2 + 1

	shortStartupPhaseBlocks = 50
	longStartupPhaseBlocks  = 200
)
