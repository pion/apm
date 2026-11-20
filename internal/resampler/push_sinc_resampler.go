// SPDX-FileCopyrightText: 2013 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package resampler

import (
	"errors"
	"fmt"
)

// ErrFrameShape is returned when a source or destination does not have
// the configured number of frames.
var ErrFrameShape = errors.New("resampler: frame shape mismatch")

// PushSincResampler mirrors PushSincResampler: a push interface to the
// sinc resampler for fixed-size chunks. Each call to Resample consumes
// sourceFrames of input and produces destinationFrames of output. The
// output lags the input by half the kernel.
type PushSincResampler struct {
	resampler         *sincResampler
	source            []float32
	destinationFrames int
	firstPass         bool
}

// NewPushSincResampler returns a resampler that converts chunks of
// sourceFrames into chunks of destinationFrames. Both must cover the
// same duration, and sourceFrames must exceed the 32-tap kernel.
func NewPushSincResampler(sourceFrames, destinationFrames int) (*PushSincResampler, error) {
	if sourceFrames <= kernelSize+kernelSize/2 || destinationFrames <= 0 {
		return nil, fmt.Errorf("%w: %d source frames, %d destination frames",
			ErrFrameShape, sourceFrames, destinationFrames)
	}
	push := &PushSincResampler{destinationFrames: destinationFrames, firstPass: true}
	push.resampler = newSincResampler(float64(sourceFrames)/float64(destinationFrames), sourceFrames, push.run)

	return push, nil
}

// Resample converts one chunk of source into destination.
func (p *PushSincResampler) Resample(source, destination []float32) error {
	if len(source) != p.resampler.requestFrames {
		return fmt.Errorf("%w: %d source frames, want %d", ErrFrameShape, len(source), p.resampler.requestFrames)
	}
	if len(destination) != p.destinationFrames {
		return fmt.Errorf("%w: %d destination frames, want %d", ErrFrameShape, len(destination), p.destinationFrames)
	}
	// Cache the source. Calling resample immediately triggers run, which
	// provides the cached value.
	p.source = source

	// On the first pass, resample is called twice. The first call gets
	// dummy input and its output is discarded. This primes the buffer
	// with the correct delay (half the kernel size), so that every later
	// call only results in one input request through run.
	//
	// chunkSize is exactly the amount of output to request in order to
	// prime the buffer with a single request for sourceFrames.
	if p.firstPass {
		p.resampler.resample(destination[:p.resampler.chunkSize()])
	}

	p.resampler.resample(destination)
	p.source = nil

	return nil
}

// run provides the cached source to the sinc resampler.
func (p *PushSincResampler) run(destination []float32) {
	if p.firstPass {
		// Dummy input on the first pass, the output of which is discarded.
		clear(destination)
		p.firstPass = false

		return
	}
	copy(destination, p.source)
}
