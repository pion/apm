// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "fmt"

// echoCanceller wires the subtractor + render delay buffer together and
// adapts the 10 ms / 160-sample APM frame size to the 4 ms / 64-sample
// internal block size used by AEC3.
type echoCanceller struct {
	renderDelay *renderDelayBuffer
	sub         *subtractor

	renderQueue  []float32 // pending render samples awaiting block extraction
	captureQueue []float32 // pending capture samples awaiting block extraction
	outputQueue  []float32 // cleaned capture samples awaiting frame extraction

	scratchBlock [kBlockSize]float32
	subOutput    subtractorOutput
}

// newEchoCanceller constructs the canceller. filterLengthBlocks sets the
// adaptive filter length in 64-sample partitions (13 ≈ 52 ms at 16 kHz,
// the upstream default for the refined filter).
func newEchoCanceller(filterLengthBlocks int) *echoCanceller {
	return &echoCanceller{
		renderDelay: newRenderDelayBuffer(filterLengthBlocks),
		sub:         newSubtractor(filterLengthBlocks),
	}
}

// AnalyzeRender pushes one 10 ms / 160-sample far-end frame. Always
// call before ProcessCapture for the same time step.
func (e *echoCanceller) AnalyzeRender(far []float32) error {
	if len(far) != kFrameSize {
		return fmt.Errorf("%w: render frame has %d samples, want %d", ErrFrameShape, len(far), kFrameSize)
	}
	e.renderQueue = append(e.renderQueue, far...)
	for len(e.renderQueue) >= kBlockSize {
		copy(e.scratchBlock[:], e.renderQueue[:kBlockSize])
		e.renderDelay.insertBlock(e.scratchBlock[:])
		e.renderQueue = e.renderQueue[kBlockSize:]
	}

	return nil
}

// ProcessCapture runs one 10 ms / 160-sample near-end frame through the
// subtractor in place. Far-end frames must already have been provided
// via AnalyzeRender to drive the filter.
func (e *echoCanceller) ProcessCapture(near []float32) error {
	if len(near) != kFrameSize {
		return fmt.Errorf("%w: capture frame has %d samples, want %d", ErrFrameShape, len(near), kFrameSize)
	}
	e.captureQueue = append(e.captureQueue, near...)
	for len(e.captureQueue) >= kBlockSize {
		// Advance render-side read position so the filter sees the most
		// recent inserted block first.
		e.renderDelay.step()
		copy(e.scratchBlock[:], e.captureQueue[:kBlockSize])
		e.sub.process(e.renderDelay.renderBuffer(), e.scratchBlock[:], &e.subOutput)
		e.outputQueue = append(e.outputQueue, e.subOutput.eRefined[:]...)
		e.captureQueue = e.captureQueue[kBlockSize:]
	}
	if len(e.outputQueue) < kFrameSize {
		// Not enough cleaned samples yet; emit input unchanged for this frame.
		return nil
	}
	copy(near, e.outputQueue[:kFrameSize])
	e.outputQueue = e.outputQueue[kFrameSize:]

	return nil
}

// HandleEchoPathChange resets adaptation state.
func (e *echoCanceller) HandleEchoPathChange() {
	e.renderDelay.reset()
	e.sub.handleEchoPathChange()
	e.renderQueue = e.renderQueue[:0]
	e.captureQueue = e.captureQueue[:0]
	e.outputQueue = e.outputQueue[:0]
}
