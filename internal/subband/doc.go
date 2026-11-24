// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package subband provides the frequency-band splitting filters that
// WebRTC's APM uses to process audio at 32 kHz (2-band QMF) and 48 kHz
// (3-band DCT-modulated FIR). Ported from
// modules/audio_processing/three_band_filter_bank.{h,cc} and
// common_audio/signal_processing/splitting_filter.c.
package subband
