// SPDX-FileCopyrightText: 2013-2021 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package agc is a partial Go port of WebRTC's GainController2: it
// implements the fixed-digital-gain + limiter chain (gain_applier and
// limiter from modules/audio_processing/agc2/). The adaptive-digital
// path (VAD-driven gain adaptation) is not yet ported.
//
// Inputs are expected at WebRTC's float-int16 scale (samples in
// [-32768, 32767]).
package agc

import (
	"errors"
	"fmt"
	"math"
)

var (
	// ErrInvalidChannels is returned when the channel count is not positive.
	ErrInvalidChannels = errors.New("agc: numChannels must be > 0")
	// ErrInvalidSampleRate is returned for sample rates that do not give whole sub-frames.
	ErrInvalidSampleRate = errors.New("agc: unsupported sample rate")
	// ErrFrameShape is returned when a frame does not match the configured shape.
	ErrFrameShape = errors.New("agc: frame shape mismatch")
)

// Config configures a Controller.
type Config struct {
	// FixedDigitalGainDB is applied uniformly to every sample.
	FixedDigitalGainDB float32
}

// Controller composes a fixed-digital GainApplier with a Limiter.
type Controller struct {
	sampleRate      int
	numChannels     int
	samplesPerFrame int
	gain            *gainApplier
	limiter         *limiter
}

// New constructs a Controller for the given stream parameters.
func New(cfg Config, sampleRateHz, numChannels int) (*Controller, error) {
	if numChannels <= 0 {
		return nil, ErrInvalidChannels
	}
	if sampleRateHz <= 0 {
		return nil, fmt.Errorf("%w: %d Hz", ErrInvalidSampleRate, sampleRateHz)
	}
	samplesPerFrame := sampleRateHz * kFrameDurationMs / 1000
	if samplesPerFrame > kMaxSamplesPerChannel {
		return nil, fmt.Errorf("%w: %d samples per frame exceeds max %d", ErrInvalidSampleRate, samplesPerFrame, kMaxSamplesPerChannel)
	}
	if samplesPerFrame%kSubFramesInFrame != 0 {
		return nil, fmt.Errorf("%w: %d samples per frame not divisible by %d sub-frames", ErrInvalidSampleRate, samplesPerFrame, kSubFramesInFrame)
	}

	return &Controller{
		sampleRate:      sampleRateHz,
		numChannels:     numChannels,
		samplesPerFrame: samplesPerFrame,
		gain:            newGainApplier(false, dbToRatio(cfg.FixedDigitalGainDB)),
		limiter:         newLimiter(samplesPerFrame),
	}, nil
}

// SetFixedGainDB changes the fixed digital gain in dB. The next frame
// applies a smooth ramp from the previous gain. Like upstream, a change of
// gain resets the limiter, so that it reacts quickly to the change of
// level.
func (c *Controller) SetFixedGainDB(db float32) {
	gainFactor := dbToRatio(db)
	if c.gain.currentGainFactor != gainFactor {
		c.limiter.reset()
	}
	c.gain.setGainFactor(gainFactor)
}

// Process applies fixed gain + limiter to one 10 ms frame in place.
func (c *Controller) Process(channels [][]float32) error {
	if len(channels) != c.numChannels {
		return fmt.Errorf("%w: got %d channels, want %d", ErrFrameShape, len(channels), c.numChannels)
	}
	for i, ch := range channels {
		if len(ch) != c.samplesPerFrame {
			return fmt.Errorf("%w: channel %d has %d samples, want %d", ErrFrameShape, i, len(ch), c.samplesPerFrame)
		}
	}
	c.gain.apply(channels)
	c.limiter.process(channels)

	return nil
}

// Reset clears the limiter state.
func (c *Controller) Reset() { c.limiter.reset() }

func dbToRatio(db float32) float32 {
	return float32(math.Pow(10, float64(db)/20))
}
