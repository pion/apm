// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// renderSignalAnalyzer is a minimal stand-in for the upstream
// RenderSignalAnalyzer. The real class tracks narrow-band excitation and
// per-bin masking; this stub reports the render as well-excited and
// masks no bins, which is appropriate for the basic single-channel path.
type renderSignalAnalyzer struct{}

func newRenderSignalAnalyzer() *renderSignalAnalyzer { return &renderSignalAnalyzer{} }

func (r *renderSignalAnalyzer) poorSignalExcitation() bool { return false }

func (r *renderSignalAnalyzer) maskRegionsAroundNarrowBands(mu *[kFftLengthBy2Plus1]float32) {
	// No-op stub.
}
