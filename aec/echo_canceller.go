// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "fmt"

// echoCanceller wires the subtractor + render delay buffer together and
// adapts the 10 ms / 160-sample APM frame size to the 4 ms / 64-sample
// internal block size used by AEC3, the way EchoCanceller3 does.
type echoCanceller struct {
	renderDelay *renderDelayBuffer
	sub         *subtractor

	renderBlocker  frameBlocker
	captureBlocker frameBlocker
	outputFramer   *blockFramer

	block     [kBlockSize]float32
	subOutput subtractorOutput
}

// newEchoCanceller constructs the canceller. filterLengthBlocks sets the
// adaptive filter length in 64-sample partitions (13 ≈ 52 ms at 16 kHz,
// the upstream default for the refined filter).
func newEchoCanceller(filterLengthBlocks int) *echoCanceller {
	return &echoCanceller{
		renderDelay:  newRenderDelayBuffer(filterLengthBlocks),
		sub:          newSubtractor(filterLengthBlocks),
		outputFramer: newBlockFramer(),
	}
}

// AnalyzeRender pushes one 10 ms / 160-sample far-end frame. Always
// call before ProcessCapture for the same time step.
func (e *echoCanceller) AnalyzeRender(far []float32) error {
	if len(far) != kFrameSize {
		return fmt.Errorf("%w: render frame has %d samples, want %d", ErrFrameShape, len(far), kFrameSize)
	}
	block := e.block[:]
	for start := 0; start < kFrameSize; start += kSubFrameLength {
		e.renderBlocker.insertSubFrameAndExtractBlock(far[start:start+kSubFrameLength], block)
		e.renderDelay.insertBlock(block)
	}
	if e.renderBlocker.isBlockAvailable() {
		e.renderBlocker.extractBlock(block)
		e.renderDelay.insertBlock(block)
	}

	return nil
}

// ProcessCapture runs one 10 ms / 160-sample near-end frame through the
// subtractor in place. Far-end frames must already have been provided
// via AnalyzeRender to drive the filter. The output lags the input by
// one block.
func (e *echoCanceller) ProcessCapture(near []float32) error {
	if len(near) != kFrameSize {
		return fmt.Errorf("%w: capture frame has %d samples, want %d", ErrFrameShape, len(near), kFrameSize)
	}
	block := e.block[:]
	for start := 0; start < kFrameSize; start += kSubFrameLength {
		subFrame := near[start : start+kSubFrameLength]
		e.captureBlocker.insertSubFrameAndExtractBlock(subFrame, block)
		e.processBlock(block)
		e.outputFramer.insertBlockAndExtractSubFrame(block, subFrame)
	}
	if e.captureBlocker.isBlockAvailable() {
		e.captureBlocker.extractBlock(block)
		e.processBlock(block)
		e.outputFramer.insertBlock(block)
	}

	return nil
}

// processBlock cancels the echo in one capture block in place.
func (e *echoCanceller) processBlock(block []float32) {
	// Advance render-side read position so the filter sees the most
	// recent inserted block first.
	e.renderDelay.step()
	e.sub.process(e.renderDelay.renderBuffer(), block, &e.subOutput)
	copy(block, e.subOutput.eRefined[:])
}

// HandleEchoPathChange resets adaptation state.
func (e *echoCanceller) HandleEchoPathChange() {
	e.renderDelay.reset()
	e.sub.handleEchoPathChange()
	e.renderBlocker.reset()
	e.captureBlocker.reset()
	e.outputFramer.reset()
}
