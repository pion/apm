// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	hErrorInitial                = 10000
	poorExcitationCounterInitial = 1000
)

// refinedFilterUpdateGain mirrors RefinedFilterUpdateGain: it computes
// the adaptation gain of the refined filter.
type refinedFilterUpdateGain struct {
	configChangeDurationBlocks      int
	oneByConfigChangeDurationBlocks float32
	currentConfig                   refinedFilterConfig
	targetConfig                    refinedFilterConfig
	oldTargetConfig                 refinedFilterConfig
	hError                          [kFftLengthBy2Plus1]float32
	poorExcitationCounter           int
	callCounter                     int
	configChangeCounter             int
}

func newRefinedFilterUpdateGain(cfg refinedFilterConfig, configChangeDurationBlocks int) *refinedFilterUpdateGain {
	gain := &refinedFilterUpdateGain{
		configChangeDurationBlocks:      configChangeDurationBlocks,
		oneByConfigChangeDurationBlocks: 1 / float32(configChangeDurationBlocks),
		poorExcitationCounter:           poorExcitationCounterInitial,
	}
	gain.setConfig(cfg, true)
	fill(gain.hError[:], hErrorInitial)

	return gain
}

// handleEchoPathChange mirrors HandleEchoPathChange.
func (g *refinedFilterUpdateGain) handleEchoPathChange(variability echoPathVariability) {
	if variability.delayChange != delayAdjustmentNone {
		fill(g.hError[:], hErrorInitial)
	}
	if !variability.gainChange {
		g.poorExcitationCounter = poorExcitationCounterInitial
		g.callCounter = 0
	}
}

// compute mirrors Compute: it computes the gain.
func (g *refinedFilterUpdateGain) compute(
	renderPower *[kFftLengthBy2Plus1]float32,
	analyzer *renderSignalAnalyzer,
	output *subtractorOutput,
	erl *[kFftLengthBy2Plus1]float32,
	sizePartitions int,
	saturatedCaptureSignal bool,
	disallowLeakageDiverged bool,
	gain *fftData,
) {
	g.callCounter++
	g.updateCurrentConfig()

	if analyzer.poorSignalExcitation() {
		g.poorExcitationCounter = 0
	}

	// Do not update the filter if the render is not sufficiently excited.
	g.poorExcitationCounter++
	if g.poorExcitationCounter < sizePartitions || saturatedCaptureSignal || g.callCounter <= sizePartitions {
		gain.clear()
	} else {
		g.computeGain(renderPower, analyzer, output, sizePartitions, gain)
	}

	// H_error = H_error + factor * erl.
	for k := range g.hError {
		if output.E2Refined[k] <= output.E2Coarse[k] || disallowLeakageDiverged {
			g.hError[k] += g.currentConfig.leakageConverged * erl[k]
		} else {
			g.hError[k] += g.currentConfig.leakageDiverged * erl[k]
		}
		g.hError[k] = max(g.hError[k], g.currentConfig.errorFloor)
		g.hError[k] = min(g.hError[k], g.currentConfig.errorCeil)
	}
}

func (g *refinedFilterUpdateGain) computeGain(
	x2 *[kFftLengthBy2Plus1]float32,
	analyzer *renderSignalAnalyzer,
	output *subtractorOutput,
	sizePartitions int,
	gain *fftData,
) {
	// mu = H_error / (0.5 * H_error * X2 + n * E2).
	var mu [kFftLengthBy2Plus1]float32
	for k := range mu {
		if x2[k] >= g.currentConfig.noiseGate {
			mu[k] = g.hError[k] / (0.5*g.hError[k]*x2[k] + float32(sizePartitions)*output.E2Refined[k])
		}
	}

	// Avoid updating the filter close to narrow bands in the render
	// signals.
	analyzer.maskRegionsAroundNarrowBands(&mu)

	// H_error = H_error - 0.5 * mu * X2 * H_error.
	for k := range g.hError {
		g.hError[k] -= 0.5 * mu[k] * x2[k] * g.hError[k]
	}

	// G = mu * E.
	for k := range mu {
		gain.re[k] = mu[k] * output.ERefined.re[k]
		gain.im[k] = mu[k] * output.ERefined.im[k]
	}
}

// setConfig mirrors SetConfig: it sets a new config.
func (g *refinedFilterUpdateGain) setConfig(cfg refinedFilterConfig, immediateEffect bool) {
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

// updateCurrentConfig updates the current config towards the target
// config.
func (g *refinedFilterUpdateGain) updateCurrentConfig() {
	if g.configChangeCounter == 0 {
		return
	}
	g.configChangeCounter--
	if g.configChangeCounter == 0 {
		g.currentConfig = g.targetConfig
		g.oldTargetConfig = g.targetConfig

		return
	}

	changeFactor := float32(g.configChangeCounter) * g.oneByConfigChangeDurationBlocks
	average := func(from, to float32) float32 {
		return from*changeFactor + to*(1-changeFactor)
	}
	g.currentConfig.leakageConverged = average(g.oldTargetConfig.leakageConverged, g.targetConfig.leakageConverged)
	g.currentConfig.leakageDiverged = average(g.oldTargetConfig.leakageDiverged, g.targetConfig.leakageDiverged)
	g.currentConfig.errorFloor = average(g.oldTargetConfig.errorFloor, g.targetConfig.errorFloor)
	g.currentConfig.errorCeil = average(g.oldTargetConfig.errorCeil, g.targetConfig.errorCeil)
	g.currentConfig.noiseGate = average(g.oldTargetConfig.noiseGate, g.targetConfig.noiseGate)
}
