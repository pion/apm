// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// blockProcessor mirrors BlockProcessorImpl: it performs the echo
// cancellation on blocks of 64 samples. It buffers the render signal,
// aligns it with the capture signal and removes the echo.
//
// The neural residual echo estimator, the histogram metrics and the echo
// leakage status, which upstream stores without using it, are not
// ported.
type blockProcessor struct {
	config                 echoCanceller3Config
	captureProperlyStarted bool
	renderProperlyStarted  bool
	renderBuffer           *renderDelayBufferImpl
	// delayController is nil when an external delay estimator is used.
	delayController    *renderDelayController
	echoRemover        *echoRemover
	renderEvent        bufferingEvent
	captureCallCounter int
	estimatedDelay     delayEstimate
	hasEstimatedDelay  bool
}

// newBlockProcessor mirrors BlockProcessor::Create.
func newBlockProcessor(
	config echoCanceller3Config, sampleRateHz, numRenderChannels, numCaptureChannels int,
) *blockProcessor {
	processor := &blockProcessor{
		config:       config,
		renderBuffer: newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels),
		echoRemover:  newEchoRemover(config, sampleRateHz, numRenderChannels, numCaptureChannels),
	}
	if !config.delay.useExternalDelayEstimator {
		processor.delayController = newRenderDelayController(config.delay, config.renderLevels, numCaptureChannels)
	}

	return processor
}

// resetDelayController resets the delay controller, when there is one.
func (b *blockProcessor) resetDelayController(resetDelayConfidence bool) {
	if b.delayController != nil {
		b.delayController.reset(resetDelayConfidence)
	}
}

// processCapture mirrors ProcessCapture: it processes a block of capture
// data. linearOutput receives the output of the linear filter when it is
// not nil.
func (b *blockProcessor) processCapture(
	echoPathGainChange, captureSignalSaturation bool, linearOutput, captureBlock *block,
) {
	b.captureCallCounter++

	if !b.renderProperlyStarted {
		// If no render data has yet arrived, do not process the capture
		// signal.
		b.renderBuffer.handleSkippedCaptureProcessing()

		return
	}
	if !b.captureProperlyStarted {
		b.captureProperlyStarted = true
		b.renderBuffer.reset()
		b.resetDelayController(true)
	}

	variability := echoPathVariability{gainChange: echoPathGainChange}

	if b.renderEvent == bufferingEventRenderOverrun {
		variability.delayChange = delayAdjustmentBufferFlush
		b.resetDelayController(true)
	}
	b.renderEvent = bufferingEventNone

	// Update the render buffers with any newly arrived render blocks and
	// prepare them for reading the render data that corresponds to the
	// current capture block.
	bufferEvent := b.renderBuffer.prepareCaptureProcessing()
	// Reset the delay controller at render buffer underrun.
	if bufferEvent == bufferingEventRenderUnderrun {
		b.resetDelayController(false)
	}

	hasDelayEstimator := !b.config.delay.useExternalDelayEstimator
	if hasDelayEstimator {
		b.alignRender(captureBlock, &variability)
	} else {
		b.renderBuffer.alignFromExternalDelay()
	}

	// Remove the echo from the capture signal.
	if hasDelayEstimator || b.renderBuffer.hasReceivedBufferDelay() {
		b.echoRemover.processCapture(variability, captureSignalSaturation, b.estimatedDelay, b.hasEstimatedDelay,
			b.renderBuffer.getRenderBuffer(), linearOutput, captureBlock)
	}
}

// alignRender computes and applies the render delay that is required to
// achieve a proper signal alignment.
func (b *blockProcessor) alignRender(captureBlock *block, variability *echoPathVariability) {
	b.estimatedDelay, b.hasEstimatedDelay = b.delayController.getDelay(
		b.renderBuffer.getDownsampledRenderBuffer(), captureBlock)

	if b.hasEstimatedDelay && b.renderBuffer.alignFromDelay(b.estimatedDelay.delay) {
		variability.delayChange = delayAdjustmentNewDetectedDelay
	}

	variability.clockDrift = b.delayController.hasClockdrift()
}

// bufferRender mirrors BufferRender: it buffers a block of render data.
func (b *blockProcessor) bufferRender(renderBlock *block) {
	b.renderEvent = b.renderBuffer.insert(renderBlock)
	b.renderProperlyStarted = true
}

// getMetrics mirrors GetMetrics: the echo return loss and the echo
// return loss enhancement in dB, and the delay in ms.
func (b *blockProcessor) getMetrics() (echoReturnLoss, echoReturnLossEnhancement float64, delayMs int) {
	const blockSizeMs = 4
	echoReturnLoss, echoReturnLossEnhancement = b.echoRemover.getMetrics()

	return echoReturnLoss, echoReturnLossEnhancement, b.renderBuffer.getDelay() * blockSizeMs
}

// setAudioBufferDelay mirrors SetAudioBufferDelay: it provides an
// external estimate of the audio buffer delay.
func (b *blockProcessor) setAudioBufferDelay(delayMs int) {
	b.renderBuffer.setAudioBufferDelay(delayMs)
}

// setCaptureOutputUsage mirrors SetCaptureOutputUsage: it tells whether
// the capture output will be used.
func (b *blockProcessor) setCaptureOutputUsage(captureOutputUsed bool) {
	b.echoRemover.setCaptureOutputUsage(captureOutputUsed)
}
