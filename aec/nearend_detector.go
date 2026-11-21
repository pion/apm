// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// nearendDetector mirrors NearendDetector: it selects whether the
// suppressor is in the nearend or the echo state.
type nearendDetector interface {
	// isNearendState mirrors IsNearendState: whether the current state is
	// the nearend state.
	isNearendState() bool
	// update mirrors Update: it updates the state selection based on the
	// latest spectral estimates.
	update(
		nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32,
		initialState bool,
	)
	// setConfig mirrors SetConfig.
	setConfig(config suppressorConfig)
}
