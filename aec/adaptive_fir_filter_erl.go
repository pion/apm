// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// computeErl matches ComputeErl: it computes the echo return loss
// estimate of the filter, which is the sum of the partition frequency
// responses.
func computeErl(h2 [][kFftLengthBy2Plus1]float32, erl *[kFftLengthBy2Plus1]float32) {
	*erl = [kFftLengthBy2Plus1]float32{}
	for p := range h2 {
		for k := range erl {
			erl[k] += h2[p][k]
		}
	}
}
