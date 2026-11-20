// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubtractor_CancelsDelayedEcho(t *testing.T) {
	const (
		partitions = 13
		blocks     = 1000
		delay      = 40
	)
	render := newRenderDelayBuffer(partitions)
	sub := newSubtractor(partitions)
	rng := rand.New(rand.NewPCG(5, 6)) //nolint:gosec // Deterministic test signal.

	far := make([]float32, blocks*kBlockSize)
	for i := range far {
		far[i] = float32(rng.NormFloat64() * 10000)
	}

	var out subtractorOutput
	var y2, e2 float64
	capture := make([]float32, kBlockSize)
	for b := range blocks {
		block := far[b*kBlockSize : (b+1)*kBlockSize]
		for i := range capture {
			if n := b*kBlockSize + i - delay; n >= 0 {
				capture[i] = 0.5 * far[n]
			}
		}
		render.insertBlock(block)
		render.step()
		sub.process(render.renderBuffer(), capture, &out)
		if b >= blocks-100 {
			y2 += float64(out.y2)
			e2 += float64(out.e2Refined)
		}
	}
	assert.Greater(t, y2, 1000*e2, "expected more than 30 dB of echo reduction")

	sub.handleEchoPathChange()
	assert.Zero(t, sub.gain.callCounter)
}
