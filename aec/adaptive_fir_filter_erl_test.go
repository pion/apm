// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The upstream tests compare the optimized variants with ErlComputer on
// this input. Here its sum is checked directly.
func TestComputeErl(t *testing.T) {
	const numPartitions = 12
	h2 := make([][kFftLengthBy2Plus1]float32, numPartitions)
	for j := range h2 {
		for k := range h2[j] {
			h2[j][k] = float32(k) + float32(j)/3
		}
	}

	var erl [kFftLengthBy2Plus1]float32
	fill(erl[:], -1)
	computeErl(h2, &erl)
	for k, value := range erl {
		// Sum over j of k + j/3.
		assert.InEpsilon(t, float32(numPartitions*k)+22, value, 1e-6, "bin %d", k)
	}
}
