// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// bufferingEvent mirrors RenderDelayBuffer::BufferingEvent.
type bufferingEvent int

const (
	bufferingEventNone bufferingEvent = iota
	bufferingEventRenderUnderrun
	bufferingEventRenderOverrun
	bufferingEventAPICallSkew
)

// renderDelayBufferImpl mirrors RenderDelayBufferImpl: it buffers the
// render signal, as blocks, FFTs, spectra and a downsampled copy, and
// applies the delay that aligns it with the capture signal.
type renderDelayBufferImpl struct {
	config                    echoCanceller3Config
	renderLinearAmplitudeGain float32
	downSamplingFactor        int
	subBlockSize              int
	blocks                    *blockBuffer
	spectra                   *spectrumBuffer
	ffts                      *fftBuffer
	delay                     int
	hasDelay                  bool
	echoRemoverBuffer         *renderBuffer
	lowRate                   *downsampledRenderBuffer
	renderMixer               *alignmentMixer
	renderDecimator           *decimator
	fft                       *aec3Fft
	renderDs                  []float32
	bufferHeadroom            int
	lastCallWasRender         bool
	numAPICallsInARow         int
	maxObservedJitter         int
	captureCallCounter        int64
	renderCallCounter         int64
	renderActivity            bool
	renderActivityCounter     int

	externalAudioBufferDelay                   int
	hasExternalAudioBufferDelay                bool
	externalAudioBufferDelayVerifiedAfterReset bool

	minLatencyBlocks             int
	excessRenderDetectionCounter int
}

func newRenderDelayBufferImpl(config echoCanceller3Config, sampleRateHz, numRenderChannels int) *renderDelayBufferImpl {
	downSamplingFactor := config.delay.downSamplingFactor
	subBlockSize := kBlockSize
	if downSamplingFactor > 0 {
		subBlockSize = kBlockSize / downSamplingFactor
	}
	size := getRenderDelayBufferSize(downSamplingFactor, config.delay.numFilters, config.filter.refined.lengthBlocks)

	buffer := &renderDelayBufferImpl{
		config: config,
		renderLinearAmplitudeGain: float32(
			math.Pow(10, float64(config.renderLevels.renderPowerGainDB/20))),
		downSamplingFactor: downSamplingFactor,
		subBlockSize:       subBlockSize,
		blocks:             newBlockBuffer(size, numBandsForRate(sampleRateHz), numRenderChannels),
		spectra:            newSpectrumBuffer(size, numRenderChannels),
		ffts:               newFftBuffer(size, numRenderChannels),
		delay:              config.delay.defaultDelay,
		hasDelay:           true,
		lowRate: newDownsampledRenderBuffer(
			getDownSampledBufferSize(downSamplingFactor, config.delay.numFilters)),
		renderMixer:       newAlignmentMixer(numRenderChannels, config.delay.renderAlignmentMixing),
		renderDecimator:   newDecimator(downSamplingFactor),
		fft:               newAec3Fft(),
		renderDs:          make([]float32, subBlockSize),
		bufferHeadroom:    config.filter.refined.lengthBlocks,
		maxObservedJitter: 1,
	}
	buffer.echoRemoverBuffer = newRenderBuffer(buffer.blocks, buffer.spectra, buffer.ffts)
	buffer.reset()

	return buffer
}

// reset mirrors Reset: it resets the buffer delays and clears the
// reported delays.
func (b *renderDelayBufferImpl) reset() {
	b.lastCallWasRender = false
	b.numAPICallsInARow = 1
	b.minLatencyBlocks = 0
	b.excessRenderDetectionCounter = 0

	// Initialize the read index to one sub-block before the write index.
	b.lowRate.read = b.lowRate.offsetIndex(b.lowRate.write, b.subBlockSize)

	// Check for any external audio buffer delay and whether it is feasible.
	if b.hasExternalAudioBufferDelay {
		const headroom = 2

		// Minimum delay is 1 (like the low-rate render buffer).
		audioBufferDelayToSet := 1
		if b.externalAudioBufferDelay > headroom {
			audioBufferDelayToSet = b.externalAudioBufferDelay - headroom
		}
		audioBufferDelayToSet = min(audioBufferDelayToSet, b.maxDelay())

		// When an external delay estimate is available, use that delay as
		// the initial render buffer delay.
		b.applyTotalDelay(audioBufferDelayToSet)
		b.delay, b.hasDelay = b.computeDelay(), true
		b.externalAudioBufferDelayVerifiedAfterReset = false

		return
	}

	// If an external delay estimate is not available, set the render
	// buffer delays to the default delay.
	b.applyTotalDelay(b.config.delay.defaultDelay)

	// Unset the delays which are set by alignFromDelay.
	b.hasDelay = false
}

// insert mirrors Insert: it inserts a new block into the render buffers.
func (b *renderDelayBufferImpl) insert(blk *block) bufferingEvent {
	b.renderCallCounter++
	if b.hasDelay {
		if !b.lastCallWasRender {
			b.lastCallWasRender = true
			b.numAPICallsInARow = 1
		} else {
			b.numAPICallsInARow++
			b.maxObservedJitter = max(b.maxObservedJitter, b.numAPICallsInARow)
		}
	}

	// Increase the write indices to where the new blocks should be written.
	previousWrite := b.blocks.write
	b.incrementWriteIndices()

	// Allow overrun and do a reset when render overrun occurs due to more
	// render data being inserted than capture data is received.
	event := bufferingEventNone
	if b.renderOverrun() {
		event = bufferingEventRenderOverrun
	}

	// Detect and update render activity.
	if !b.renderActivity {
		if b.detectActiveRender(blk.view(0, 0)) {
			b.renderActivityCounter++
		}
		b.renderActivity = b.renderActivityCounter >= 20
	}

	// Insert the new render block into the specified position.
	b.insertBlock(blk, previousWrite)

	if event != bufferingEventNone {
		b.reset()
	}

	return event
}

// handleSkippedCaptureProcessing mirrors HandleSkippedCaptureProcessing.
func (b *renderDelayBufferImpl) handleSkippedCaptureProcessing() {
	b.captureCallCounter++
}

// prepareCaptureProcessing mirrors PrepareCaptureProcessing: it prepares
// the render buffers for processing another capture block.
func (b *renderDelayBufferImpl) prepareCaptureProcessing() bufferingEvent {
	event := bufferingEventNone
	b.captureCallCounter++

	if b.hasDelay {
		if b.lastCallWasRender {
			b.lastCallWasRender = false
			b.numAPICallsInARow = 1
		} else {
			b.numAPICallsInARow++
			b.maxObservedJitter = max(b.maxObservedJitter, b.numAPICallsInARow)
		}
	}

	switch {
	case b.detectExcessRenderBlocks():
		// Too many render blocks compared to capture blocks. Risk of delay
		// ending up before the filter used by the delay estimator.
		b.reset()
		event = bufferingEventRenderOverrun
	case b.renderUnderrun():
		// Don't increment the read indices of the low rate buffer if there
		// is a render underrun.
		b.incrementReadIndices()

		// Incrementing the buffer index without increasing the low rate
		// buffer index means that the delay is reduced by one.
		if b.hasDelay && b.delay > 0 {
			b.delay--
		}
		event = bufferingEventRenderUnderrun
	default:
		// Increment the read indices in the render buffers to point to the
		// most recent block to use in the capture processing.
		b.incrementLowRateReadIndices()
		b.incrementReadIndices()
	}

	b.echoRemoverBuffer.setRenderActivity(b.renderActivity)
	if b.renderActivity {
		b.renderActivityCounter = 0
		b.renderActivity = false
	}

	return event
}

// alignFromDelay mirrors AlignFromDelay: it sets the delay and returns
// whether the delay was changed.
func (b *renderDelayBufferImpl) alignFromDelay(delay int) bool {
	if !b.externalAudioBufferDelayVerifiedAfterReset && b.hasExternalAudioBufferDelay && b.hasDelay {
		b.externalAudioBufferDelayVerifiedAfterReset = true
	}
	if b.hasDelay && b.delay == delay {
		return false
	}
	b.delay, b.hasDelay = delay, true

	// Compute the total delay and limit the delay to the allowed range.
	totalDelay := min(b.maxDelay(), max(b.mapDelayToTotalDelay(b.delay), 0))

	// Apply the delay to the buffers.
	b.applyTotalDelay(totalDelay)

	return true
}

// setAudioBufferDelay mirrors SetAudioBufferDelay.
func (b *renderDelayBufferImpl) setAudioBufferDelay(delayMs int) {
	// Convert delay from milliseconds to blocks (rounded down).
	const (
		sampleRateForFixedCaptureDelay = 16000
		numSamplesPerMs                = sampleRateForFixedCaptureDelay / 1000
	)
	b.externalAudioBufferDelay = (delayMs*numSamplesPerMs + b.config.delay.fixedCaptureDelaySamples) /
		(kBlockSizeMs * numSamplesPerMs)
	b.hasExternalAudioBufferDelay = true
}

// hasReceivedBufferDelay mirrors HasReceivedBufferDelay.
func (b *renderDelayBufferImpl) hasReceivedBufferDelay() bool {
	return b.hasExternalAudioBufferDelay
}

// getDelay mirrors Delay.
func (b *renderDelayBufferImpl) getDelay() int { return b.computeDelay() }

// maxDelay mirrors MaxDelay.
func (b *renderDelayBufferImpl) maxDelay() int {
	return len(b.blocks.buffer) - 1 - b.bufferHeadroom
}

// getRenderBuffer mirrors GetRenderBuffer.
func (b *renderDelayBufferImpl) getRenderBuffer() *renderBuffer { return b.echoRemoverBuffer }

// getDownsampledRenderBuffer mirrors GetDownsampledRenderBuffer.
func (b *renderDelayBufferImpl) getDownsampledRenderBuffer() *downsampledRenderBuffer {
	return b.lowRate
}

// mapDelayToTotalDelay maps the externally computed delay to the delay
// used internally.
func (b *renderDelayBufferImpl) mapDelayToTotalDelay(externalDelayBlocks int) int {
	return b.bufferLatency() + externalDelayBlocks
}

// computeDelay returns the delay (not including call jitter).
func (b *renderDelayBufferImpl) computeDelay() int {
	internalDelay := b.spectra.read - b.spectra.write
	if b.spectra.read < b.spectra.write {
		internalDelay += b.spectra.size
	}

	return internalDelay - b.bufferLatency()
}

// applyTotalDelay sets the read indices according to the delay.
func (b *renderDelayBufferImpl) applyTotalDelay(delay int) {
	b.blocks.read = b.blocks.offsetIndex(b.blocks.write, -delay)
	b.spectra.read = b.spectra.offsetIndex(b.spectra.write, delay)
	b.ffts.read = b.ffts.offsetIndex(b.ffts.write, delay)
}

// alignFromExternalDelay mirrors AlignFromExternalDelay.
func (b *renderDelayBufferImpl) alignFromExternalDelay() {
	if !b.hasExternalAudioBufferDelay {
		return
	}
	delay := b.renderCallCounter - b.captureCallCounter + int64(b.externalAudioBufferDelay)
	delayWithHeadroom := delay - int64(b.config.delay.delayHeadroomSamples/kBlockSize)
	b.applyTotalDelay(int(delayWithHeadroom))
}

// insertBlock inserts a block into the render buffers.
func (b *renderDelayBufferImpl) insertBlock(blk *block, previousWrite int) {
	current := b.blocks.buffer[b.blocks.write]
	copy(current.data, blk.data)

	if b.renderLinearAmplitudeGain != 1 {
		for i := range current.data {
			current.data[i] *= b.renderLinearAmplitudeGain
		}
	}

	var downmixedRender [kBlockSize]float32
	b.renderMixer.produceOutput(current, downmixedRender[:])
	b.renderDecimator.decimate(downmixedRender[:], b.renderDs)
	for i, sample := range b.renderDs {
		b.lowRate.buffer[b.lowRate.write+len(b.renderDs)-1-i] = sample
	}

	previous := b.blocks.buffer[previousWrite]
	for channel := range current.numChannels {
		b.fft.paddedFft(
			(*[kFftLengthBy2]float32)(current.view(0, channel)),
			(*[kFftLengthBy2]float32)(previous.view(0, channel)),
			windowRectangular,
			&b.ffts.buffer[b.ffts.write][channel])
		b.ffts.buffer[b.ffts.write][channel].spectrum(&b.spectra.buffer[b.spectra.write][channel])
	}
}

func (b *renderDelayBufferImpl) detectActiveRender(x []float32) bool {
	var energy float32
	for _, sample := range x {
		energy += sample * sample
	}
	limit := b.config.renderLevels.activeRenderLimit

	return energy > limit*limit*kFftLengthBy2
}

func (b *renderDelayBufferImpl) detectExcessRenderBlocks() bool {
	excessRenderDetected := false
	latencyBlocks := b.bufferLatency()

	// The recently seen minimum latency in blocks. Should be close to 0.
	b.minLatencyBlocks = min(b.minLatencyBlocks, latencyBlocks)

	// After processing a configurable number of blocks the minimum latency
	// is checked.
	b.excessRenderDetectionCounter++
	if b.excessRenderDetectionCounter >= b.config.buffering.excessRenderDetectionIntervalBlocks {
		// If the minimum latency is not lower than the threshold there have
		// been more render than capture frames.
		excessRenderDetected = b.minLatencyBlocks > b.config.buffering.maxAllowedExcessRenderBlocks

		// Reset the counter and let the minimum latency be the current
		// latency.
		b.minLatencyBlocks = latencyBlocks
		b.excessRenderDetectionCounter = 0
	}

	return excessRenderDetected
}

// bufferLatency computes the latency in the buffer (the number of unread
// sub-blocks).
func (b *renderDelayBufferImpl) bufferLatency() int {
	size := len(b.lowRate.buffer)
	latencySamples := (size + b.lowRate.read - b.lowRate.write) % size

	return latencySamples / b.subBlockSize
}

// incrementWriteIndices increments the write indices for the render
// buffers.
func (b *renderDelayBufferImpl) incrementWriteIndices() {
	b.lowRate.updateWriteIndex(-b.subBlockSize)
	b.blocks.incWriteIndex()
	b.spectra.decWriteIndex()
	b.ffts.decWriteIndex()
}

// incrementLowRateReadIndices increments the read indices of the low rate
// render buffers.
func (b *renderDelayBufferImpl) incrementLowRateReadIndices() {
	b.lowRate.updateReadIndex(-b.subBlockSize)
}

// incrementReadIndices increments the read indices for the render buffers.
func (b *renderDelayBufferImpl) incrementReadIndices() {
	if b.blocks.read != b.blocks.write {
		b.blocks.incReadIndex()
		b.spectra.decReadIndex()
		b.ffts.decReadIndex()
	}
}

// renderOverrun checks for a render buffer overrun.
func (b *renderDelayBufferImpl) renderOverrun() bool {
	return b.lowRate.read == b.lowRate.write || b.blocks.read == b.blocks.write
}

// renderUnderrun checks for a render buffer underrun.
func (b *renderDelayBufferImpl) renderUnderrun() bool {
	return b.lowRate.read == b.lowRate.write
}
