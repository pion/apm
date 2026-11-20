// SPDX-FileCopyrightText: 2012 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package hpf is a Go port of WebRTC's
// modules/audio_processing/high_pass_filter.{h,cc}: a per-channel
// 3-stage cascaded biquad with rate-specific coefficients.
//
// Only 16, 32, and 48 kHz are supported, matching upstream.
package hpf

import (
	"errors"
	"fmt"

	"github.com/pion/apm/internal/dsp/biquad"
)

var (
	// ErrUnsupportedSampleRate is returned for rates other than 16, 32 and 48 kHz.
	ErrUnsupportedSampleRate = errors.New("hpf: unsupported sample rate")
	// ErrChannelCount is returned when a frame does not match the configured channel count.
	ErrChannelCount = errors.New("hpf: channel count mismatch")
)

// Coefficient tables are copied verbatim from upstream
// high_pass_filter.cc kHighPassFilterCoefficients{16,32,48}kHz.

func coeffs16kHz() []biquad.Coefficients {
	return []biquad.Coefficients{
		{
			B: [3]float32{0.8773539420715290582, -1.754683920749088077, 0.8773539420715289472},
			A: [2]float32{-1.881687317862849707, 0.8880584644559580410},
		},
		{
			B: [3]float32{1.0, -1.999810143464515022, 1.0},
			A: [2]float32{-1.976035417167170793, 0.9779708644868606582},
		},
		{
			B: [3]float32{1.0, -1.999669231394235469, 1.0},
			A: [2]float32{-1.994265767864654482, 0.9954861594635392441},
		},
	}
}

func coeffs32kHz() []biquad.Coefficients {
	return []biquad.Coefficients{
		{
			B: [3]float32{0.9102055685511306615, -1.820404922871161624, 0.9102055685511306615},
			A: [2]float32{-1.940710875829138482, 0.9423512845457852061},
		},
		{
			B: [3]float32{1.0, -1.999952541587768806, 1.0},
			A: [2]float32{-1.988434609801665420, 0.9889212529819323416},
		},
		{
			B: [3]float32{1.0, -1.999917315632020021, 1.0},
			A: [2]float32{-1.997434723613889629, 0.9977401885079651978},
		},
	}
}

func coeffs48kHz() []biquad.Coefficients {
	return []biquad.Coefficients{
		{
			B: [3]float32{0.9213790163564168, -1.8427552370064049, 0.9213790163564168},
			A: [2]float32{-1.9604500061078971, 0.9611862979079667},
		},
		{
			B: [3]float32{1.0, -1.9999789078432082, 1.0},
			A: [2]float32{-1.9923834169149972, 0.9926001112941157},
		},
		{
			B: [3]float32{1.0, -1.9999632520325810, 1.0},
			A: [2]float32{-1.9983570340145236, 0.9984928491805198},
		},
	}
}

// Filter is a multi-channel high-pass filter. One Filter per stream.
type Filter struct {
	rate     int
	channels []*biquad.Cascade
}

// New constructs a Filter for the given sample rate and channel count.
func New(sampleRateHz, numChannels int) (*Filter, error) {
	coeffs, err := chooseCoefficients(sampleRateHz)
	if err != nil {
		return nil, err
	}
	f := &Filter{
		rate:     sampleRateHz,
		channels: make([]*biquad.Cascade, numChannels),
	}
	for c := range f.channels {
		f.channels[c] = biquad.NewCascade(coeffs)
	}

	return f, nil
}

// SampleRate returns the rate the filter was constructed for, in Hz.
func (f *Filter) SampleRate() int { return f.rate }

// NumChannels returns the configured channel count.
func (f *Filter) NumChannels() int { return len(f.channels) }

// Process filters one frame in place. channels is [channel][sample];
// each channel slice is filtered through its own cascaded biquad.
func (f *Filter) Process(channels [][]float32) error {
	if len(channels) != len(f.channels) {
		return fmt.Errorf("%w: got %d, want %d", ErrChannelCount, len(channels), len(f.channels))
	}
	for c, ch := range channels {
		f.channels[c].Process(ch)
	}

	return nil
}

// Reset clears all internal state across channels.
func (f *Filter) Reset() {
	for _, c := range f.channels {
		c.Reset()
	}
}

func chooseCoefficients(rate int) ([]biquad.Coefficients, error) {
	switch rate {
	case 16000:
		return coeffs16kHz(), nil
	case 32000:
		return coeffs32kHz(), nil
	case 48000:
		return coeffs48kHz(), nil
	default:
		return nil, fmt.Errorf("%w: %d Hz (want 16000, 32000, or 48000)", ErrUnsupportedSampleRate, rate)
	}
}
