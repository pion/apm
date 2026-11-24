// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package agc

// gainApplier applies a scalar gain with per-sample ramping between the
// previous frame's gain and the current. Mirrors gain_applier.{h,cc}.
type gainApplier struct {
	hardClipSamples          bool
	lastGainFactor           float32
	currentGainFactor        float32
	samplesPerChannel        int
	inverseSamplesPerChannel float32
}

func newGainApplier(hardClip bool, initial float32) *gainApplier {
	return &gainApplier{
		hardClipSamples:   hardClip,
		lastGainFactor:    initial,
		currentGainFactor: initial,
	}
}

func (g *gainApplier) setGainFactor(f float32) { g.currentGainFactor = f }

func (g *gainApplier) initialize(samplesPerChannel int) {
	g.samplesPerChannel = samplesPerChannel
	g.inverseSamplesPerChannel = 1.0 / float32(samplesPerChannel)
}

func (g *gainApplier) apply(channels [][]float32) {
	if len(channels) == 0 {
		return
	}
	n := len(channels[0])
	if n != g.samplesPerChannel {
		g.initialize(n)
	}
	g.applyWithRamping(channels)
	g.lastGainFactor = g.currentGainFactor
	if g.hardClipSamples {
		for _, ch := range channels {
			for i, v := range ch {
				ch[i] = safeClamp(v, kMinFloatS16Value, kMaxFloatS16Value)
			}
		}
	}
}

func (g *gainApplier) applyWithRamping(channels [][]float32) {
	last := g.lastGainFactor
	cur := g.currentGainFactor

	if last == cur && gainCloseToOne(cur) {
		return
	}
	if last == cur {
		for _, ch := range channels {
			for i, v := range ch {
				ch[i] = v * cur
			}
		}

		return
	}

	inc := (cur - last) * g.inverseSamplesPerChannel
	for _, ch := range channels {
		gain := last
		for i, v := range ch {
			ch[i] = v * gain
			gain += inc
		}
	}
}

func gainCloseToOne(f float32) bool {
	const eps = 1.0 / kMaxFloatS16Value

	return 1.0-eps <= f && f <= 1.0+eps
}
