// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// coarseFilterUpdateGain ports CoarseFilterUpdateGain.
type coarseFilterUpdateGain struct {
	currentConfig                   coarseFilterConfig
	targetConfig                    coarseFilterConfig
	oldTargetConfig                 coarseFilterConfig
	configChangeDurationBlocks      int
	oneByConfigChangeDurationBlocks float32
	poorSignalExcitationCounter     int
	callCounter                     int
	configChangeCounter             int
}

func newCoarseFilterUpdateGain(cfg coarseFilterConfig, configChangeDurationBlocks int) *coarseFilterUpdateGain {
	g := &coarseFilterUpdateGain{configChangeDurationBlocks: configChangeDurationBlocks}
	g.oneByConfigChangeDurationBlocks = 1.0 / float32(configChangeDurationBlocks)
	g.setConfig(cfg, true)

	return g
}

func (g *coarseFilterUpdateGain) setConfig(cfg coarseFilterConfig, immediateEffect bool) {
	if immediateEffect {
		g.oldTargetConfig = cfg
		g.currentConfig = cfg
		g.targetConfig = cfg
		g.configChangeCounter = 0
	} else {
		g.oldTargetConfig = g.currentConfig
		g.targetConfig = cfg
		g.configChangeCounter = g.configChangeDurationBlocks
	}
}

func (g *coarseFilterUpdateGain) handleEchoPathChange() {
	g.poorSignalExcitationCounter = 0
	g.callCounter = 0
}

func (g *coarseFilterUpdateGain) updateCurrentConfig() {
	if g.configChangeCounter > 0 {
		g.configChangeCounter--
		if g.configChangeCounter > 0 {
			cf := float32(g.configChangeCounter) * g.oneByConfigChangeDurationBlocks
			g.currentConfig.rate = g.oldTargetConfig.rate*cf + g.targetConfig.rate*(1-cf)
			g.currentConfig.noiseGate = g.oldTargetConfig.noiseGate*cf + g.targetConfig.noiseGate*(1-cf)
		} else {
			g.currentConfig = g.targetConfig
			g.oldTargetConfig = g.targetConfig
		}
	}
}

// compute mirrors CoarseFilterUpdateGain::Compute.
func (g *coarseFilterUpdateGain) compute(
	renderPower *[kFftLengthBy2Plus1]float32,
	rsa *renderSignalAnalyzer,
	errCoarse *fftData,
	sizePartitions int,
	saturatedCapture bool,
	gain *fftData,
) {
	g.callCounter++
	g.updateCurrentConfig()

	if rsa.poorSignalExcitation() {
		g.poorSignalExcitationCounter = 0
	}

	g.poorSignalExcitationCounter++
	if g.poorSignalExcitationCounter < sizePartitions || saturatedCapture || g.callCounter <= sizePartitions {
		gain.clear()

		return
	}

	var mu [kFftLengthBy2Plus1]float32
	for k := range kFftLengthBy2Plus1 {
		x := renderPower[k]
		if x > g.currentConfig.noiseGate {
			mu[k] = g.currentConfig.rate / x
		}
	}
	rsa.maskRegionsAroundNarrowBands(&mu)

	for k := range kFftLengthBy2Plus1 {
		gain.re[k] = mu[k] * errCoarse.re[k]
		gain.im[k] = mu[k] * errCoarse.im[k]
	}
}
