// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package metrics implements the native DSP metrics used by the test
// infrastructure: segmental SNR, log-spectral distance, cepstral distance,
// ERLE, level-tracker attack/release timing, THD+N, and a simplified
// perceptual model used as a coarse sanity bound.
//
// All metrics operate on []float32 input at a known sample rate, return
// float64 results, and avoid hidden defaults: every windowing or framing
// parameter is explicit in the function signature.
//
// Conventions:
//
//   - "Reference" and "test" parameters follow ITU convention: reference
//     is the clean/ideal signal, test is the system-under-test output.
//   - Frames are zero-padded if the input doesn't divide evenly.
//   - dB values use base-10 (20·log10 for amplitudes, 10·log10 for powers).
package metrics
