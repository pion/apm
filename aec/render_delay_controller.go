// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// computeBufferDelay matches ComputeBufferDelay: it converts an estimate
// in samples to a delay in blocks, with hysteresis against the current
// delay.
func computeBufferDelay(
	currentDelay delayEstimate, hasCurrentDelay bool, hysteresisLimitBlocks int, estimatedDelay delayEstimate,
) delayEstimate {
	// Compute the buffer delay increase required to achieve the desired
	// latency.
	newDelayBlocks := estimatedDelay.delay >> kBlockSizeLog2

	// Add hysteresis.
	if hasCurrentDelay {
		currentDelayBlocks := currentDelay.delay
		if newDelayBlocks > currentDelayBlocks && newDelayBlocks <= currentDelayBlocks+hysteresisLimitBlocks {
			newDelayBlocks = currentDelayBlocks
		}
	}

	newDelay := estimatedDelay
	newDelay.delay = newDelayBlocks

	return newDelay
}

// renderDelayController mirrors RenderDelayControllerImpl: it turns the
// echo path delay estimates into the delay, in blocks, to apply to the
// render delay buffer. The metrics reporting of upstream is not ported.
type renderDelayController struct {
	hysteresisLimitBlocks    int
	delay                    delayEstimate
	hasDelay                 bool
	delayEstimator           *echoPathDelayEstimator
	delaySamples             delayEstimate
	hasDelaySamples          bool
	lastDelayEstimateQuality delayEstimateQuality
}

func newRenderDelayController(
	delay delayConfig, renderLevels renderLevelsConfig, numCaptureChannels int,
) *renderDelayController {
	return &renderDelayController{
		hysteresisLimitBlocks:    delay.hysteresisLimitBlocks,
		delayEstimator:           newEchoPathDelayEstimator(delay, renderLevels, numCaptureChannels),
		lastDelayEstimateQuality: delayQualityCoarse,
	}
}

// reset mirrors Reset.
func (c *renderDelayController) reset(resetDelayConfidence bool) {
	c.hasDelay = false
	c.hasDelaySamples = false
	c.delayEstimator.reset(resetDelayConfidence)
	if resetDelayConfidence {
		c.lastDelayEstimateQuality = delayQualityCoarse
	}
}

// getDelay mirrors GetDelay: it aligns the render buffer content with the
// capture signal and returns the delay in blocks, if one is known.
func (c *renderDelayController) getDelay(
	renderBuffer *downsampledRenderBuffer, capture *block,
) (delayEstimate, bool) {
	if delaySamples, ok := c.delayEstimator.estimateDelay(renderBuffer, capture); ok {
		c.delaySamples, c.hasDelaySamples = delaySamples, true
	}

	if c.hasDelaySamples {
		// Compute the render delay buffer delay.
		hysteresisLimitBlocks := 0
		if c.lastDelayEstimateQuality == delayQualityRefined && c.delaySamples.quality == delayQualityRefined {
			hysteresisLimitBlocks = c.hysteresisLimitBlocks
		}
		c.delay = computeBufferDelay(c.delay, c.hasDelay, hysteresisLimitBlocks, c.delaySamples)
		c.hasDelay = true
		c.lastDelayEstimateQuality = c.delay.quality
	}

	return c.delay, c.hasDelay
}

// hasClockdrift mirrors HasClockdrift.
func (c *renderDelayController) hasClockdrift() bool {
	return c.delayEstimator.clockdrift() != clockdriftNone
}
