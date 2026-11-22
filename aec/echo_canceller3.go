// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"slices"

	"github.com/pion/apm/hpf"
)

const (
	kRenderTransferQueueSizeFrames = 100
	// splitBandSize mirrors AudioBuffer::kSplitBandSize: the number of
	// samples of a band of a 10 ms frame.
	splitBandSize = kFrameSize
)

// detectSaturation matches DetectSaturation.
func detectSaturation(samples []float32) bool {
	for _, sample := range samples {
		if sample >= 32700 || sample <= -32700 {
			return true
		}
	}

	return false
}

// blockProcessing is the part of BlockProcessor that EchoCanceller3 uses.
type blockProcessing interface {
	processCapture(echoPathGainChange, captureSignalSaturation bool, linearOutput, captureBlock *block)
	bufferRender(renderBlock *block)
	getMetrics() (echoReturnLoss, echoReturnLossEnhancement float64, delayMs int)
	setAudioBufferDelay(delayMs int)
	setCaptureOutputUsage(captureOutputUsed bool)
}

// newBandFrame returns a frame that is indexed by band, channel and
// sample.
func newBandFrame(numBands, numChannels int) [][][]float32 {
	frame := make([][][]float32, numBands)
	for band := range frame {
		frame[band] = make([][]float32, numChannels)
		for ch := range frame[band] {
			frame[band][ch] = make([]float32, splitBandSize)
		}
	}

	return frame
}

// frameBlockers mirrors FrameBlocker: a frameBlocker per band and
// channel.
type frameBlockers [][]frameBlocker

func newFrameBlockers(numBands, numChannels int) frameBlockers {
	blockers := make(frameBlockers, numBands)
	for band := range blockers {
		blockers[band] = make([]frameBlocker, numChannels)
	}

	return blockers
}

// insertSubFrameAndExtractBlock mirrors InsertSubFrameAndExtractBlock.
// The sub-frame is indexed by band, channel and sample.
func (f frameBlockers) insertSubFrameAndExtractBlock(subFrame [][][]float32, blk *block) {
	for band := range f {
		for ch := range f[band] {
			f[band][ch].insertSubFrameAndExtractBlock(subFrame[band][ch], blk.view(band, ch))
		}
	}
}

// isBlockAvailable mirrors IsBlockAvailable.
func (f frameBlockers) isBlockAvailable() bool { return f[0][0].isBlockAvailable() }

// extractBlock mirrors ExtractBlock.
func (f frameBlockers) extractBlock(blk *block) {
	for band := range f {
		for ch := range f[band] {
			f[band][ch].extractBlock(blk.view(band, ch))
		}
	}
}

// blockFramers mirrors BlockFramer: a blockFramer per band and channel.
type blockFramers [][]*blockFramer

func newBlockFramers(numBands, numChannels int) blockFramers {
	framers := make(blockFramers, numBands)
	for band := range framers {
		framers[band] = make([]*blockFramer, numChannels)
		for ch := range framers[band] {
			framers[band][ch] = newBlockFramer()
		}
	}

	return framers
}

// insertBlock mirrors InsertBlock.
func (f blockFramers) insertBlock(blk *block) {
	for band := range f {
		for ch := range f[band] {
			f[band][ch].insertBlock(blk.view(band, ch))
		}
	}
}

// insertBlockAndExtractSubFrame mirrors InsertBlockAndExtractSubFrame.
// The sub-frame is indexed by band, channel and sample.
func (f blockFramers) insertBlockAndExtractSubFrame(blk *block, subFrame [][][]float32) {
	for band := range f {
		for ch := range f[band] {
			f[band][ch].insertBlockAndExtractSubFrame(blk.view(band, ch), subFrame[band][ch])
		}
	}
}

// renderTransferQueue stands for the SwapQueue of render frames between
// the render and the capture sides: a queue of
// kRenderTransferQueueSizeFrames frames.
type renderTransferQueue struct {
	frames [][][][]float32
	first  int
	size   int
}

func newRenderTransferQueue(numBands, numChannels int) *renderTransferQueue {
	queue := &renderTransferQueue{frames: make([][][][]float32, kRenderTransferQueueSizeFrames)}
	for i := range queue.frames {
		queue.frames[i] = newBandFrame(numBands, numChannels)
	}

	return queue
}

// insert copies a frame into the queue. It returns false, and drops the
// frame, when the queue is full.
func (q *renderTransferQueue) insert(frame [][][]float32) bool {
	if q.size == len(q.frames) {
		return false
	}
	copyBandFrame(q.frames[(q.first+q.size)%len(q.frames)], frame)
	q.size++

	return true
}

// remove copies the oldest frame of the queue into frame. It returns
// false when the queue is empty.
func (q *renderTransferQueue) remove(frame [][][]float32) bool {
	if q.size == 0 {
		return false
	}
	copyBandFrame(frame, q.frames[q.first])
	q.first = (q.first + 1) % len(q.frames)
	q.size--

	return true
}

// copyBandFrame copies the samples of a frame that is indexed by band,
// channel and sample.
func copyBandFrame(dst, src [][][]float32) {
	for band := range src {
		for ch := range src[band] {
			copy(dst[band][ch], src[band][ch])
		}
	}
}

// subFrameView returns the view of a sub-frame of a frame that is indexed
// by band, channel and sample.
func subFrameView(frame, view [][][]float32, subFrameIndex int) {
	for band := range view {
		for ch := range view[band] {
			view[band][ch] = frame[band][ch][subFrameIndex*kSubFrameLength : (subFrameIndex+1)*kSubFrameLength]
		}
	}
}

// echoCanceller3 mirrors EchoCanceller3: the main class of the echo
// canceller. It splits the render and the capture frames in blocks and
// passes them to the block processor.
//
// The frames hold 10 ms of audio, split in 16 kHz frequency bands. The
// render and the capture frames are indexed by channel, band and sample,
// like AudioBuffer::split_bands.
//
// Unlike upstream, it must not be used from several goroutines: the
// render frames are queued and processed with the next capture frame.
// The field trials, the neural residual echo estimator and the API call
// metrics of upstream are not ported.
type echoCanceller3 struct {
	sampleRateHz                int
	numBands                    int
	numRenderInputChannels      int
	numRenderChannelsToAec      int
	numCaptureChannels          int
	configSelector              *configSelector
	multichannelContentDetector *multiChannelContentDetector
	outputFramer                blockFramers
	captureBlocker              frameBlockers
	renderBlocker               frameBlockers
	renderTransferQueue         *renderTransferQueue
	blockProcessor              blockProcessing
	renderQueueInputFrame       [][][]float32
	renderQueueOutputFrame      [][][]float32
	// highPassFilter filters the first band of the render signal when
	// the config asks for it.
	highPassFilter            *hpf.Filter
	saturatedMicrophoneSignal bool
	renderBlock               *block
	captureBlock              *block
	// linearOutputFramer and linearOutputBlock are set when the output of
	// the linear filter is exported.
	linearOutputFramer       blockFramers
	linearOutputBlock        *block
	blockDelayBuffer         *blockDelayBuffer
	renderSubFrameView       [][][]float32
	captureSubFrameView      [][][]float32
	linearOutputSubFrameView [][][]float32
	captureBandFrame         [][][]float32
	linearOutputBandFrame    [][][]float32
}

// newEchoCanceller3 creates an echo canceller. multichannelConfig is the
// config to use for proper multichannel content, or nil.
func newEchoCanceller3(
	config echoCanceller3Config,
	multichannelConfig *echoCanceller3Config,
	sampleRateHz, numRenderChannels, numCaptureChannels int,
) (*echoCanceller3, error) {
	numBands := numBandsForRate(sampleRateHz)
	selector := newConfigSelector(config, multichannelConfig, numRenderChannels)
	active := selector.activeConfig()
	canceller := &echoCanceller3{
		sampleRateHz:           sampleRateHz,
		numBands:               numBands,
		numRenderInputChannels: numRenderChannels,
		numCaptureChannels:     numCaptureChannels,
		configSelector:         selector,
		multichannelContentDetector: newMultiChannelContentDetector(
			active.multiChannel.detectStereoContent,
			numRenderChannels,
			active.multiChannel.stereoDetectionThreshold,
			active.multiChannel.stereoDetectionTimeoutThresholdSeconds,
			active.multiChannel.stereoDetectionHysteresisSeconds),
		outputFramer:           newBlockFramers(numBands, numCaptureChannels),
		captureBlocker:         newFrameBlockers(numBands, numCaptureChannels),
		renderTransferQueue:    newRenderTransferQueue(numBands, numRenderChannels),
		renderQueueInputFrame:  newBandFrame(numBands, numRenderChannels),
		renderQueueOutputFrame: newBandFrame(numBands, numRenderChannels),
		renderBlock:            newBlock(numBands, numRenderChannels),
		captureBlock:           newBlock(numBands, numCaptureChannels),
		captureSubFrameView:    newSubFrameView(numBands, numCaptureChannels),
		captureBandFrame:       newSubFrameView(numBands, numCaptureChannels),
	}

	if active.delay.fixedCaptureDelaySamples > 0 {
		canceller.blockDelayBuffer = newBlockDelayBuffer(
			numCaptureChannels, numBands, splitBandSize, config.delay.fixedCaptureDelaySamples)
	}
	if active.filter.highPassFilterEchoReference {
		filter, err := hpf.New(16000, numRenderChannels)
		if err != nil {
			return nil, err
		}
		canceller.highPassFilter = filter
	}
	if active.filter.exportLinearAecOutput {
		canceller.linearOutputFramer = newBlockFramers(1, numCaptureChannels)
		canceller.linearOutputBlock = newBlock(1, numCaptureChannels)
		canceller.linearOutputSubFrameView = newSubFrameView(1, numCaptureChannels)
		canceller.linearOutputBandFrame = newSubFrameView(1, numCaptureChannels)
	}

	canceller.initialize()

	return canceller, nil
}

// newSubFrameView returns the slices of a view that is indexed by band
// and channel.
func newSubFrameView(numBands, numChannels int) [][][]float32 {
	view := make([][][]float32, numBands)
	for band := range view {
		view[band] = make([][]float32, numChannels)
	}

	return view
}

// initialize mirrors Initialize: it sets up the processing for the
// number of render channels that the content calls for.
func (e *echoCanceller3) initialize() {
	proper := e.multichannelContentDetector.isProperMultiChannelContentDetected()
	e.numRenderChannelsToAec = 1
	if proper {
		e.numRenderChannelsToAec = e.numRenderInputChannels
	}

	e.configSelector.update(proper)

	e.renderBlock.setNumChannels(e.numRenderChannelsToAec)
	e.renderBlocker = newFrameBlockers(e.numBands, e.numRenderChannelsToAec)
	e.blockProcessor = newBlockProcessor(
		*e.configSelector.activeConfig(), e.sampleRateHz, e.numRenderChannelsToAec, e.numCaptureChannels)
	e.renderSubFrameView = newSubFrameView(e.numBands, e.numRenderChannelsToAec)
}

// analyzeRender mirrors AnalyzeRender: it queues a render frame, which is
// indexed by channel, band and sample.
func (e *echoCanceller3) analyzeRender(render [][][]float32) {
	for band := range e.renderQueueInputFrame {
		for ch := range e.renderQueueInputFrame[band] {
			copy(e.renderQueueInputFrame[band][ch], render[ch][band])
		}
	}
	if e.highPassFilter != nil {
		// The filter was created for this number of channels at 16 kHz.
		_ = e.highPassFilter.Process(e.renderQueueInputFrame[0])
	}

	e.renderTransferQueue.insert(e.renderQueueInputFrame)
}

// analyzeCapture mirrors AnalyzeCapture: it detects the saturation of
// the capture signal. The capture signal holds the samples of each
// channel, before the band split.
func (e *echoCanceller3) analyzeCapture(capture [][]float32) {
	e.saturatedMicrophoneSignal = slices.ContainsFunc(capture, detectSaturation)
}

// processCapture mirrors ProcessCapture: it removes the echo from a
// capture frame, which is indexed by channel, band and sample.
// linearOutput receives the output of the linear filter when it is not
// nil, which requires the config to export it. levelChange tells that the
// level of the capture signal was changed.
func (e *echoCanceller3) processCapture(capture, linearOutput [][][]float32, levelChange bool) {
	if linearOutput != nil && e.linearOutputFramer == nil {
		linearOutput = nil
	}

	// Optionally delay the capture signal.
	if e.blockDelayBuffer != nil {
		e.blockDelayBuffer.delaySignal(capture)
	}

	e.emptyRenderQueue()

	for band := range e.captureBandFrame {
		for ch := range e.captureBandFrame[band] {
			e.captureBandFrame[band][ch] = capture[ch][band]
		}
	}
	if linearOutput != nil {
		for ch := range e.linearOutputBandFrame[0] {
			e.linearOutputBandFrame[0][ch] = linearOutput[ch][0]
		}
	}

	echoPathGainChange := levelChange || e.multichannelContentDetector.isTemporaryMultiChannelContentDetected()
	for subFrameIndex := range 2 {
		e.processCaptureFrameContent(linearOutput != nil, echoPathGainChange, subFrameIndex)
	}
	e.processRemainingCaptureFrameContent(linearOutput != nil, echoPathGainChange)
}

// processCaptureFrameContent matches ProcessCaptureFrameContent.
func (e *echoCanceller3) processCaptureFrameContent(exportLinearOutput, echoPathGainChange bool, subFrameIndex int) {
	subFrameView(e.captureBandFrame, e.captureSubFrameView, subFrameIndex)
	if exportLinearOutput {
		subFrameView(e.linearOutputBandFrame, e.linearOutputSubFrameView, subFrameIndex)
	}

	e.captureBlocker.insertSubFrameAndExtractBlock(e.captureSubFrameView, e.captureBlock)
	e.blockProcessor.processCapture(echoPathGainChange, e.saturatedMicrophoneSignal, e.linearOutputBlock,
		e.captureBlock)
	e.outputFramer.insertBlockAndExtractSubFrame(e.captureBlock, e.captureSubFrameView)

	if exportLinearOutput {
		e.linearOutputFramer.insertBlockAndExtractSubFrame(e.linearOutputBlock, e.linearOutputSubFrameView)
	}
}

// processRemainingCaptureFrameContent matches
// ProcessRemainingCaptureFrameContent.
func (e *echoCanceller3) processRemainingCaptureFrameContent(exportLinearOutput, echoPathGainChange bool) {
	if !e.captureBlocker.isBlockAvailable() {
		return
	}

	e.captureBlocker.extractBlock(e.captureBlock)
	e.blockProcessor.processCapture(echoPathGainChange, e.saturatedMicrophoneSignal, e.linearOutputBlock,
		e.captureBlock)
	e.outputFramer.insertBlock(e.captureBlock)

	if exportLinearOutput {
		e.linearOutputFramer.insertBlock(e.linearOutputBlock)
	}
}

// getMetrics mirrors GetMetrics: the echo return loss and the echo
// return loss enhancement in dB, and the delay in ms.
func (e *echoCanceller3) getMetrics() (echoReturnLoss, echoReturnLossEnhancement float64, delayMs int) {
	return e.blockProcessor.getMetrics()
}

// setAudioBufferDelay mirrors SetAudioBufferDelay: it provides an
// external estimate of the audio buffer delay.
func (e *echoCanceller3) setAudioBufferDelay(delayMs int) {
	e.blockProcessor.setAudioBufferDelay(delayMs)
}

// setCaptureOutputUsage mirrors SetCaptureOutputUsage: it tells whether
// the capture output will be used.
func (e *echoCanceller3) setCaptureOutputUsage(captureOutputUsed bool) {
	e.blockProcessor.setCaptureOutputUsage(captureOutputUsed)
}

// emptyRenderQueue mirrors EmptyRenderQueue: it buffers the queued render
// frames in the block processor.
func (e *echoCanceller3) emptyRenderQueue() {
	for e.renderTransferQueue.remove(e.renderQueueOutputFrame) {
		if e.multichannelContentDetector.updateDetection(e.renderQueueOutputFrame) {
			// Reinitialize the AEC when proper stereo is detected.
			e.initialize()
		}

		// Buffer the frame content.
		properDownmixNeeded := e.multichannelContentDetector.isTemporaryMultiChannelContentDetected()
		for subFrameIndex := range 2 {
			e.fillRenderSubFrameView(properDownmixNeeded, subFrameIndex)
			e.renderBlocker.insertSubFrameAndExtractBlock(e.renderSubFrameView, e.renderBlock)
			e.blockProcessor.bufferRender(e.renderBlock)
		}

		if e.renderBlocker.isBlockAvailable() {
			e.renderBlocker.extractBlock(e.renderBlock)
			e.blockProcessor.bufferRender(e.renderBlock)
		}
	}
}

// fillRenderSubFrameView matches FillSubFrameView for the render frame.
// When the frame has more channels than the echo canceller processes, the
// first channel is used. When a proper downmix is needed, which is the
// case when proper stereo is present in the echo reference signal but the
// echo canceller does the processing in mono, the first channel is first
// replaced by the average of the channels.
func (e *echoCanceller3) fillRenderSubFrameView(properDownmixNeeded bool, subFrameIndex int) {
	frame := e.renderQueueOutputFrame
	first := subFrameIndex * kSubFrameLength
	if e.numRenderInputChannels > e.numRenderChannelsToAec && properDownmixNeeded {
		oneByNumChannels := 1 / float32(e.numRenderInputChannels)
		for band := range frame {
			mono := frame[band][0][first : first+kSubFrameLength]
			for _, channel := range frame[band][1:] {
				for k, sample := range channel[first : first+kSubFrameLength] {
					mono[k] += sample
				}
			}
			for k := range mono {
				mono[k] *= oneByNumChannels
			}
		}
	}

	subFrameView(frame, e.renderSubFrameView, subFrameIndex)
}
