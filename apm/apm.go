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

const (
	// bandSamples is the per-band sample count of a 10 ms frame.
	bandSamples = 160
)

// Processor is a per-stream APM instance. One Processor per audio stream.
// Not safe for concurrent use.
type Processor struct {
	cfg Config
	// procRate is the rate the modules run at, as picked by
	// suitableProcessRate.
	procRate SampleRate

	hpf *hpf.Filter
	ns  *ns.Suppressor
	agc *agc.Controller
	aec *aec.Canceller

	// Band splitting filters: non-nil at 32 and 48 kHz when a module
	// works on the bands.
	splitCapture *subband.SplittingFilter
	splitRender  *subband.SplittingFilter
	captureBands [][][]float32 // [band][channel][sample]; allocated once at New()
	renderBands  [][][]float32
	// Views of the capture and the render signals for the echo canceller,
	// indexed by channel and band like upstream's AudioBuffer::split_bands.
	captureSplit [][][]float32
	renderSplit  [][][]float32

	// Capture stream at the processing rate, with the resamplers to and
	// from it. All nil unless the stream rate differs and a module is
	// enabled.
	captureBuffer       [][]float32
	captureResampler    *rateConverter
	captureOutResampler *rateConverter

	// Render stream at the processing rate, for the echo canceller. The
	// resampler is non-nil when the reverse rate differs.
	renderBuffer    [][]float32
	renderResampler *rateConverter
}

// New constructs a Processor.
func New(cfg Config) (*Processor, error) {
	cfg, err := resolveConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Like upstream, split the capture signal only when a module that
	// works on the bands is enabled, and the render signal only for the
	// echo canceller.
	bandModules := cfg.HPF.Enabled || cfg.NS.Enabled || cfg.AEC.Enabled
	proc := &Processor{
		cfg:      cfg,
		procRate: SampleRate(suitableProcessRate(int(cfg.SampleRate), int(cfg.MaxProcessingRate), bandModules)),
	}
	if err := proc.initCapture(bandModules); err != nil {
		return nil, err
	}
	if cfg.AEC.Enabled {
		if err := proc.initRender(); err != nil {
			return nil, err
		}
	}

	if err := proc.initModules(); err != nil {
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
	if cfg.MaxProcessingRate != Rate32k {
		cfg.MaxProcessingRate = Rate48k
	}
	if !cfg.ReverseSampleRate.Valid() {
		return cfg, fmt.Errorf("%w: reverse %d", ErrInvalidSampleRate, cfg.ReverseSampleRate)
	}
	if cfg.ReverseNumChannels < 0 {
		return cfg, fmt.Errorf("%w: reverse %d", ErrInvalidChannels, cfg.ReverseNumChannels)
	}

	return cfg, nil
}

// initCapture sets up the band split of the capture stream and, when a
// module runs at another rate than the stream, its conversion to the
// processing rate and back.
func (p *Processor) initCapture(bandModules bool) error {
	cfg := p.cfg

	var err error
	if bandModules {
		p.splitCapture, p.captureBands, err = newSplitter(p.procRate, cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: splitting filter: %w", err)
		}
	}
	if cfg.AEC.Enabled {
		p.captureSplit = splitBandViews(p.captureBands, cfg.NumChannels)
	}
	if p.procRate == cfg.SampleRate || (!bandModules && !cfg.AGC.Enabled) {
		return nil
	}

	p.captureBuffer = allocChannels(cfg.NumChannels, p.procRate.SamplesPerFrame())
	p.captureResampler, err = newRateConverter(cfg.SampleRate, p.procRate, cfg.NumChannels)
	if err != nil {
		return err
	}
	p.captureOutResampler, err = newRateConverter(p.procRate, cfg.SampleRate, cfg.NumChannels)

	return err
}

// initRender sets up the render path of the echo canceller. Like upstream,
// the render stream is processed at the capture processing rate.
func (p *Processor) initRender() error {
	cfg := p.cfg
	p.renderBuffer = allocChannels(cfg.ReverseNumChannels, p.procRate.SamplesPerFrame())

	var err error
	if cfg.ReverseSampleRate != p.procRate {
		p.renderResampler, err = newRateConverter(cfg.ReverseSampleRate, p.procRate, cfg.ReverseNumChannels)
		if err != nil {
			return err
		}
	}
	p.splitRender, p.renderBands, err = newSplitter(p.procRate, cfg.ReverseNumChannels)
	if err != nil {
		return fmt.Errorf("apm: render splitting filter: %w", err)
	}
	p.renderSplit = splitBandViews(p.renderBands, cfg.ReverseNumChannels)
	if p.splitRender == nil {
		setSingleBand(p.renderSplit, p.renderBuffer)
	}

	return nil
}

// splitBandViews returns the views of a signal split in bands, indexed by
// channel and band. bands is indexed by band and channel, and is nil for
// a signal with a single band, whose views are set with setSingleBand.
func splitBandViews(bands [][][]float32, numChannels int) [][][]float32 {
	views := make([][][]float32, numChannels)
	for ch := range views {
		views[ch] = make([][]float32, max(len(bands), 1))
		for band := range bands {
			views[ch][band] = bands[band][ch]
		}
	}

	return views
}

// setSingleBand points the views of a signal with a single band to its
// channels.
func setSingleBand(views [][][]float32, channels [][]float32) {
	for ch := range views {
		views[ch][0] = channels[ch]
	}
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

// initModules creates the enabled processing modules.
func (p *Processor) initModules() error {
	cfg := p.cfg
	if cfg.HPF.Enabled {
		// Upstream filters the full band by default, before the split.
		f, err := hpf.New(int(p.procRate), cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: hpf: %w", err)
		}
		p.hpf = f
	}
	if cfg.NS.Enabled {
		s, err := ns.NewSuppressor(ns.Config{TargetLevel: mapNSLevel(cfg.NS.Level)}, int(p.procRate), cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: ns: %w", err)
		}
		p.ns = s
	}
	if cfg.AGC.Enabled {
		// Upstream runs GainController2 on the full band, after the bands
		// are merged.
		c, err := agc.New(agc.Config{FixedDigitalGainDB: cfg.AGC.CompressionGain}, int(p.procRate), cfg.NumChannels)
		if err != nil {
			return fmt.Errorf("apm: agc: %w", err)
		}
		p.agc = c
	}
	if cfg.AEC.Enabled {
		// Like upstream with multi_channel_render and multi_channel_capture,
		// which are on by default: every channel is processed.
		c, err := aec.New(int(p.procRate), cfg.ReverseNumChannels, cfg.NumChannels)
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

	// Like upstream, resample before scaling up and after scaling down.
	data := near.Data
	if p.captureResampler != nil {
		if err := p.captureResampler.convert(near.Data, p.captureBuffer); err != nil {
			return err
		}
		data = p.captureBuffer
	}
	scaleUp(data)
	err := p.processCapture(data)
	scaleDown(data)
	if err != nil {
		return err
	}
	if p.captureOutResampler != nil {
		return p.captureOutResampler.convert(p.captureBuffer, near.Data)
	}

	return nil
}

// processCapture runs the modules on a capture frame at the processing
// rate and on the int16 scale.
func (p *Processor) processCapture(data [][]float32) error {
	if p.hpf != nil {
		if err := p.hpf.Process(data); err != nil {
			return err
		}
	}
	// Upstream detects the saturation of the capture signal on the full
	// band, before the split.
	if p.aec != nil {
		if err := p.aec.AnalyzeCapture(data); err != nil {
			return err
		}
	}
	if err := p.processBands(data); err != nil {
		return err
	}
	if p.agc != nil {
		return p.agc.Process(data)
	}

	return nil
}

// processBands runs the modules that work on the 16 kHz bands: on the
// signal itself at 16 kHz, on its bands at 32 and 48 kHz.
func (p *Processor) processBands(data [][]float32) error {
	if p.splitCapture == nil {
		if p.aec != nil {
			setSingleBand(p.captureSplit, data)
		}

		return p.processInner(data, nil)
	}
	if err := p.splitCapture.Analysis(data, p.captureBands); err != nil {
		return err
	}
	if err := p.processInner(p.captureBands[0], p.captureBands[1:]); err != nil {
		return err
	}

	return p.splitCapture.Synthesis(p.captureBands, data)
}

// processInner runs the band modules. channels is the 16 kHz band: the
// whole signal at 16 kHz, its lowest band at 32 and 48 kHz. upperBands
// holds the bands above it.
func (p *Processor) processInner(channels [][]float32, upperBands [][][]float32) error {
	// Upstream analyzes the noise before the echo canceller changes the
	// signal.
	if p.ns != nil {
		if err := p.ns.Analyze(channels); err != nil {
			return err
		}
	}
	if p.aec != nil {
		if err := p.aec.ProcessCapture(p.captureSplit, false); err != nil {
			return err
		}
	}
	if p.ns != nil {
		return p.ns.Process(channels, upperBands...)
	}

	return nil
}

const apmScale = 32768.0

func scaleUp(data [][]float32) {
	for _, channel := range data {
		for i := range channel {
			channel[i] *= apmScale
		}
	}
}

func scaleDown(data [][]float32) {
	const inv = 1.0 / apmScale
	for _, channel := range data {
		for i := range channel {
			channel[i] *= inv
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
// echo canceller consumes this stream, with the next near-end frame;
// other modules ignore it. The frame is left unchanged.
func (p *Processor) ProcessReverseStream(far *Frame) error {
	if err := p.validateFrame(far, p.cfg.ReverseSampleRate, p.cfg.ReverseNumChannels); err != nil {
		return err
	}
	if p.aec == nil {
		return nil
	}

	if p.renderResampler != nil {
		if err := p.renderResampler.convert(far.Data, p.renderBuffer); err != nil {
			return err
		}
	} else {
		for i, channel := range far.Data {
			copy(p.renderBuffer[i], channel)
		}
	}
	scaleUp(p.renderBuffer)

	if p.splitRender != nil {
		if err := p.splitRender.Analysis(p.renderBuffer, p.renderBands); err != nil {
			return err
		}
	}

	return p.aec.AnalyzeRender(p.renderSplit)
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
