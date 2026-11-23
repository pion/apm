// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedSampleRate is returned for sample rates other than 16,
	// 32 and 48 kHz.
	ErrUnsupportedSampleRate = errors.New("aec: sample rate must be 16, 32 or 48 kHz")
	// ErrInvalidChannels is returned when a channel count is not positive.
	ErrInvalidChannels = errors.New("aec: channel counts must be > 0")
	// ErrFrameShape is returned when a frame does not match the configured shape.
	ErrFrameShape = errors.New("aec: frame shape mismatch")
)

// Canceller is the echo canceller of one stream, with the upstream
// default configs of EchoCanceller3. Not safe for concurrent use.
//
// It allocates when it detects stereo content in the render signal, to
// set up the processing of all the render channels, like upstream.
//
// It works on 10 ms frames on the int16 scale. A frame split in bands is
// indexed by channel, band and sample: it has one 16 kHz band at 16 kHz,
// two at 32 kHz and three at 48 kHz, of 160 samples each.
type Canceller struct {
	impl               *echoCanceller3
	numBands           int
	numRenderChannels  int
	numCaptureChannels int
}

// Metrics holds the metrics of a Canceller.
type Metrics struct {
	// EchoReturnLoss is the echo return loss, in dB.
	EchoReturnLoss float64
	// EchoReturnLossEnhancement is the echo return loss enhancement, in dB.
	EchoReturnLossEnhancement float64
	// DelayMs is the delay of the render signal in the capture signal, in ms.
	DelayMs int
}

// New constructs a Canceller for a stream at 16, 32 or 48 kHz.
func New(sampleRateHz, numRenderChannels, numCaptureChannels int) (*Canceller, error) {
	switch sampleRateHz {
	case 16000, 32000, 48000:
	default:
		return nil, fmt.Errorf("%w: got %d Hz", ErrUnsupportedSampleRate, sampleRateHz)
	}
	if numRenderChannels <= 0 || numCaptureChannels <= 0 {
		return nil, ErrInvalidChannels
	}

	// Like upstream APM when the user sets no config.
	multichannelConfig := defaultMultichannelEchoCanceller3Config()
	impl, err := newEchoCanceller3(
		defaultEchoCanceller3Config(), &multichannelConfig, sampleRateHz, numRenderChannels, numCaptureChannels)
	if err != nil {
		return nil, err
	}

	return &Canceller{
		impl:               impl,
		numBands:           numBandsForRate(sampleRateHz),
		numRenderChannels:  numRenderChannels,
		numCaptureChannels: numCaptureChannels,
	}, nil
}

// AnalyzeRender queues one far-end frame split in bands. The frame is
// left unchanged.
func (c *Canceller) AnalyzeRender(render [][][]float32) error {
	if err := c.checkBands(render, c.numRenderChannels); err != nil {
		return err
	}
	c.impl.analyzeRender(render)

	return nil
}

// AnalyzeCapture analyzes one near-end frame before it is split in bands.
// The frame is indexed by channel and sample. Call it before
// ProcessCapture.
func (c *Canceller) AnalyzeCapture(capture [][]float32) error {
	if len(capture) != c.numCaptureChannels {
		return fmt.Errorf("%w: got %d channels, want %d", ErrFrameShape, len(capture), c.numCaptureChannels)
	}
	for _, channel := range capture {
		if len(channel) != c.numBands*splitBandSize {
			return fmt.Errorf("%w: got %d samples, want %d", ErrFrameShape, len(channel), c.numBands*splitBandSize)
		}
	}
	c.impl.analyzeCapture(capture)

	return nil
}

// ProcessCapture removes the echo from one near-end frame split in
// bands, in place. levelChange tells that a gain applied to the capture
// or the render signal has changed since the last frame.
func (c *Canceller) ProcessCapture(capture [][][]float32, levelChange bool) error {
	if err := c.checkBands(capture, c.numCaptureChannels); err != nil {
		return err
	}
	c.impl.processCapture(capture, nil, levelChange)

	return nil
}

// SetAudioBufferDelay provides an external estimate of the delay of the
// audio buffers, in ms.
func (c *Canceller) SetAudioBufferDelay(delayMs int) {
	c.impl.setAudioBufferDelay(delayMs)
}

// Metrics returns the current metrics.
func (c *Canceller) Metrics() Metrics {
	erl, erle, delayMs := c.impl.getMetrics()

	return Metrics{EchoReturnLoss: erl, EchoReturnLossEnhancement: erle, DelayMs: delayMs}
}

// checkBands checks the shape of a frame split in bands.
func (c *Canceller) checkBands(frame [][][]float32, numChannels int) error {
	if len(frame) != numChannels {
		return fmt.Errorf("%w: got %d channels, want %d", ErrFrameShape, len(frame), numChannels)
	}
	for _, bands := range frame {
		if len(bands) != c.numBands {
			return fmt.Errorf("%w: got %d bands, want %d", ErrFrameShape, len(bands), c.numBands)
		}
		for _, band := range bands {
			if len(band) != splitBandSize {
				return fmt.Errorf("%w: got %d samples, want %d", ErrFrameShape, len(band), splitBandSize)
			}
		}
	}

	return nil
}
