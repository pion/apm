// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedSampleRate is returned for sample rates other than 16 kHz.
	ErrUnsupportedSampleRate = errors.New("aec: only 16 kHz is supported")
	// ErrInvalidChannels is returned when the channel count is not positive.
	ErrInvalidChannels = errors.New("aec: numChannels must be > 0")
	// ErrFrameShape is returned when a frame does not match the configured shape.
	ErrFrameShape = errors.New("aec: frame shape mismatch")
)

// Canceller is the public single-stream AEC. One per stream; not safe
// for concurrent use.
type Canceller struct {
	rate     int
	channels int
	impls    []*echoCanceller // one per capture channel
}

// Config configures a Canceller.
type Config struct {
	// FilterLengthBlocks sets the adaptive filter length in 4 ms blocks.
	// Defaults to 13 (≈52 ms at 16 kHz), matching upstream's refined.
	FilterLengthBlocks int
}

// DefaultConfig returns the upstream default refined-filter length:
// 13 blocks (≈52 ms at 16 kHz).
func DefaultConfig() Config { return Config{FilterLengthBlocks: 13} }

// New constructs a Canceller for the given stream parameters. Only
// 16 kHz is supported in this port; other rates return an error.
func New(cfg Config, sampleRateHz, numChannels int) (*Canceller, error) {
	if sampleRateHz != 16000 {
		return nil, fmt.Errorf("%w: got %d Hz", ErrUnsupportedSampleRate, sampleRateHz)
	}
	if numChannels <= 0 {
		return nil, ErrInvalidChannels
	}
	if cfg.FilterLengthBlocks <= 0 {
		cfg.FilterLengthBlocks = 13
	}
	c := &Canceller{rate: sampleRateHz, channels: numChannels, impls: make([]*echoCanceller, numChannels)}
	for i := range c.impls {
		c.impls[i] = newEchoCanceller(cfg.FilterLengthBlocks)
	}

	return c, nil
}

// AnalyzeRender pushes one 10 ms / 160-sample far-end frame per channel.
// Call once per frame before ProcessCapture.
func (c *Canceller) AnalyzeRender(far [][]float32) error {
	if len(far) != c.channels {
		return fmt.Errorf("%w: far has %d channels, want %d", ErrFrameShape, len(far), c.channels)
	}
	for i, ch := range far {
		if err := c.impls[i].AnalyzeRender(ch); err != nil {
			return err
		}
	}

	return nil
}

// ProcessCapture applies echo cancellation to one 10 ms / 160-sample
// near-end frame in place per channel.
func (c *Canceller) ProcessCapture(near [][]float32) error {
	if len(near) != c.channels {
		return fmt.Errorf("%w: near has %d channels, want %d", ErrFrameShape, len(near), c.channels)
	}
	for i, ch := range near {
		if err := c.impls[i].ProcessCapture(ch); err != nil {
			return err
		}
	}

	return nil
}

// Reset clears all adaptation state.
func (c *Canceller) Reset() {
	for _, e := range c.impls {
		e.HandleEchoPathChange()
	}
}
