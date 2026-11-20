// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package apm is a Go port of WebRTC's Audio Processing Module: a
// per-stream pipeline of high-pass filter, acoustic echo cancellation,
// noise suppression, and automatic gain control.
package apm

import (
	"errors"
	"fmt"

	"github.com/pion/apm/aec"
	"github.com/pion/apm/agc"
	"github.com/pion/apm/hpf"
	"github.com/pion/apm/internal/subband"
	"github.com/pion/apm/ns"
)

var (
	// ErrFrameShape is returned when a frame's shape mismatches the
	// Processor's configured rate or channel count.
	ErrFrameShape = errors.New("apm: frame shape mismatch")
	// ErrInvalidChannels is returned for a channel count below one.
	ErrInvalidChannels = errors.New("apm: invalid channel count")
	// ErrInvalidSampleRate is returned for an unsupported sample rate.
	ErrInvalidSampleRate = errors.New("apm: invalid sample rate")
	// ErrStreamDelay is returned for a stream delay outside [0, 500] ms.
	ErrStreamDelay = errors.New("apm: stream delay out of [0, 500] ms")
)

// bandSamples is the per-band sample count of a 10 ms split-band frame.
const bandSamples = 160

// Processor is a per-stream APM instance. One Processor per audio stream.
// Not safe for concurrent use.
type Processor struct {
	cfg Config

	hpf *hpf.Filter
	ns  *ns.Suppressor
	agc *agc.Controller
	aec *aec.Canceller

	// Multi-band splitting filter: non-nil for 32/48 kHz where the main
	// pipeline runs on band 0 (lowest sub-band) at an effective 16 kHz.
	splitCapture *subband.SplittingFilter
	splitRender  *subband.SplittingFilter
	captureBands [][][]float32 // [band][channel][sample]; allocated once at New()
	renderBands  [][][]float32
}

// New constructs a Processor.
func New(cfg Config) (*Processor, error) {
	cfg, err := resolveConfig(cfg)
	if err != nil {
		return nil, err
	}

	proc := &Processor{cfg: cfg}

	proc.splitCapture, proc.captureBands, err = newSplitter(cfg.SampleRate, cfg.NumChannels)
	if err != nil {
		return nil, fmt.Errorf("apm: splitting filter: %w", err)
	}
	proc.splitRender, proc.renderBands, err = newSplitter(cfg.ReverseSampleRate, cfg.ReverseNumChannels)
	if err != nil {
		return nil, fmt.Errorf("apm: render splitting filter: %w", err)
	}

	// For 32/48 kHz inputs the inner pipeline processes band 0 of the
	// split-band representation at 16 kHz.
	innerRate := int(cfg.SampleRate)
	if proc.splitCapture != nil {
		innerRate = 16000
	}
	if err := proc.initModules(innerRate); err != nil {
		return nil, err
	}

	return proc, nil
}

// resolveConfig fills in the reverse stream defaults and validates cfg.
func resolveConfig(cfg Config) (Config, error) {
	if cfg.NumChannels <= 0 {
		return cfg, fmt.Errorf("%w: %d", ErrInvalidChannels, cfg.NumChannels)
	}
	if !cfg.SampleRate.Valid() {
		return cfg, fmt.Errorf("%w: %d", ErrInvalidSampleRate, cfg.SampleRate)
	}
	if cfg.ReverseSampleRate == 0 {
		cfg.ReverseSampleRate = cfg.SampleRate
	}
	if cfg.ReverseNumChannels == 0 {
		cfg.ReverseNumChannels = cfg.NumChannels
	}
	if !cfg.ReverseSampleRate.Valid() {
		return cfg, fmt.Errorf("%w: reverse %d", ErrInvalidSampleRate, cfg.ReverseSampleRate)
	}
	if cfg.ReverseNumChannels < 0 {
		return cfg, fmt.Errorf("%w: reverse %d", ErrInvalidChannels, cfg.ReverseNumChannels)
	}
	// The echo canceller pairs each capture channel with one render channel.
	if cfg.AEC.Enabled && cfg.ReverseNumChannels != cfg.NumChannels {
		return cfg, fmt.Errorf("%w: echo cancellation needs %d reverse channels, got %d",
			ErrInvalidChannels, cfg.NumChannels, cfg.ReverseNumChannels)
	}

	return cfg, nil
}

// newSplitter returns a band splitting filter and its buffers for 32 and
// 48 kHz streams, or nils for rates processed in a single band.
func newSplitter(rate SampleRate, numChannels int) (*subband.SplittingFilter, [][][]float32, error) {
	var numBands int
	switch rate {
	case Rate32k:
		numBands = 2
	case Rate48k:
		numBands = 3
	default:
		return nil, nil, nil
	}
	sf, err := subband.NewSplittingFilter(numChannels, numBands)
	if err != nil {
		return nil, nil, err
	}

	return sf, allocBandBuffers(numBands, numChannels), nil
}

// initModules creates the enabled processing modules at the inner rate.
func (p *Processor) initModules(innerRate int) error {
	cfg := p.cfg
	if cfg.HPF.Enabled {
		// Upstream uses 16 kHz HPF coefficients in the split-band path.
		f, err := hpf.New(innerRate, cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: hpf: %w", err)
		}
		p.hpf = f
	}
	if cfg.NS.Enabled {
		s, err := ns.NewSuppressor(ns.Config{TargetLevel: mapNSLevel(cfg.NS.Level)}, innerRate, cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: ns: %w", err)
		}
		p.ns = s
	}
	if cfg.AGC.Enabled {
		c, err := agc.New(agc.Config{FixedDigitalGainDB: cfg.AGC.CompressionGain}, innerRate, cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: agc: %w", err)
		}
		p.agc = c
	}
	if cfg.AEC.Enabled {
		c, err := aec.New(aec.DefaultConfig(), innerRate, cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: aec: %w", err)
		}
		p.aec = c
	}

	return nil
}

// Config returns a copy of the active configuration.
func (p *Processor) Config() Config { return p.cfg }

// ProcessStream processes one 10 ms near-end frame in place.
//
// Public API takes samples in the [-1, +1] convention. Internally the
// pipeline runs at WebRTC's [-32768, +32767] scale (the upstream modules
// are calibrated for that range). Conversion happens at the boundary.
func (p *Processor) ProcessStream(near *Frame) error {
	if err := p.validateFrame(near, p.cfg.SampleRate, p.cfg.NumChannels); err != nil {
		return err
	}
	scaleUp(near.Data)
	defer scaleDown(near.Data)

	if p.splitCapture == nil {
		// Single-band fast path (16 kHz native).
		return p.processInner(near.Data)
	}

	// Multi-band path: split → process band 0 → synthesize.
	if err := p.splitCapture.Analysis(near.Data, p.captureBands); err != nil {
		return err
	}
	if err := p.processInner(p.captureBands[0]); err != nil {
		return err
	}

	return p.splitCapture.Synthesis(p.captureBands, near.Data)
}

// processInner runs the per-module pipeline at the inner (16 kHz) rate
// on whatever per-channel buffers the caller supplies (the full-band
// signal at 16 kHz, or band 0 at 32/48 kHz).
func (p *Processor) processInner(channels [][]float32) error {
	if p.hpf != nil {
		if err := p.hpf.Process(channels); err != nil {
			return err
		}
	}
	if p.aec != nil {
		if err := p.aec.ProcessCapture(channels); err != nil {
			return err
		}
	}
	if p.ns != nil {
		if err := p.ns.Analyze(channels); err != nil {
			return err
		}
		if err := p.ns.Process(channels); err != nil {
			return err
		}
	}
	if p.agc != nil {
		if err := p.agc.Process(channels); err != nil {
			return err
		}
	}

	return nil
}

const apmScale = 32768.0

func scaleUp(data [][]float32) {
	for c := range data {
		for i := range data[c] {
			data[c][i] *= apmScale
		}
	}
}

func scaleDown(data [][]float32) {
	const inv = 1.0 / apmScale
	for c := range data {
		for i := range data[c] {
			data[c][i] *= inv
		}
	}
}

// allocBandBuffers allocates a [band][channel][sample] buffer matrix.
func allocBandBuffers(numBands, numChannels int) [][][]float32 {
	bands := make([][][]float32, numBands)
	for b := range bands {
		bands[b] = make([][]float32, numChannels)
		for c := range bands[b] {
			bands[b][c] = make([]float32, bandSamples)
		}
	}

	return bands
}

// ProcessReverseStream analyzes one 10 ms far-end (render) frame. The
// echo canceller consumes this stream; other modules ignore it. The
// frame is left unchanged.
func (p *Processor) ProcessReverseStream(far *Frame) error {
	if err := p.validateFrame(far, p.cfg.ReverseSampleRate, p.cfg.ReverseNumChannels); err != nil {
		return err
	}
	if p.aec == nil {
		return nil
	}

	scaleUp(far.Data)
	defer scaleDown(far.Data)

	if p.splitRender == nil {
		return p.aec.AnalyzeRender(far.Data)
	}
	if err := p.splitRender.Analysis(far.Data, p.renderBands); err != nil {
		return err
	}

	return p.aec.AnalyzeRender(p.renderBands[0])
}

// SetStreamDelayMS reports round-trip stream delay (ms) to the echo canceller.
func (p *Processor) SetStreamDelayMS(ms int) error {
	if ms < 0 || ms > 500 {
		return fmt.Errorf("%w: %d", ErrStreamDelay, ms)
	}

	return nil
}

func (p *Processor) validateFrame(f *Frame, wantRate SampleRate, wantCh int) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.SampleRate != wantRate {
		return fmt.Errorf("%w: rate %d, want %d", ErrFrameShape, f.SampleRate, wantRate)
	}
	if f.NumChannels() != wantCh {
		return fmt.Errorf("%w: channels %d, want %d", ErrFrameShape, f.NumChannels(), wantCh)
	}

	return nil
}

func mapNSLevel(l NSLevel) ns.SuppressionLevel {
	switch l {
	case NSLow:
		return ns.Level6dB
	case NSModerate:
		return ns.Level12dB
	case NSHigh:
		return ns.Level18dB
	case NSVeryHigh:
		return ns.Level21dB
	default:
		return ns.Level12dB
	}
}
