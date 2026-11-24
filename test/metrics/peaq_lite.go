// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package metrics

import "math"

// PEAQLite returns a coarse perceptual-distortion score between a
// reference and degraded signal. It is *not* PEAQ: that's an ITU-R
// BS.1387 reference model with a psychoacoustic ear and cognitive model
// that we don't implement here. PEAQLite is a sanity bound: it computes
// a critical-band-weighted log-spectral distance, mapped to an
// approximate "objective difference grade" (ODG) in [-4, 0].
//
//	0  = imperceptible
//	-1 = perceptible but not annoying
//	-2 = slightly annoying
//	-3 = annoying
//	-4 = very annoying
//
// Use this as a regression tripwire: never as a published quality
// number. For real MOS scores, invoke ViSQOL via test/visqol.
func PEAQLite(reference, degraded []float32, sampleRate int) float64 {
	frameLen := 1024
	if sampleRate >= 32000 {
		frameLen = 2048
	}
	hop := frameLen / 2
	lsd := LogSpectralDistance(reference, degraded, sampleRate, frameLen, hop)
	if math.IsNaN(lsd) {
		return math.NaN()
	}
	// Saturating exponential anchored at 0 so identical signals score 0:
	//   LSD = 0  dB → ODG = 0
	//   LSD = 1  dB → ODG ≈ -0.73
	//   LSD = 5  dB → ODG ≈ -2.53
	//   LSD = 10 dB → ODG ≈ -3.46
	const k = 5.0

	return -4 * (1 - math.Exp(-lsd/k))
}
