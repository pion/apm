// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package property holds property-based test scenarios that assert DSP
// behavior (ERLE thresholds, SNR-improvement targets, attack times) on
// each APM module.
//
// Thresholds for every scenario live in thresholds.yaml at this package
// root.
package property

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pion/apm/apm"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var (
	// ErrThresholdsPath is returned when thresholds.yaml cannot be located.
	ErrThresholdsPath = errors.New("property: cannot resolve thresholds.yaml path")
	// ErrShortOutput is returned when the output buffer is shorter than the input.
	ErrShortOutput = errors.New("property: outBuf shorter than input")
)

// Thresholds is the parsed contents of thresholds.yaml: scenario name to
// expected metric bounds. Tests query it to keep the spec out of code.
type Thresholds map[string]ScenarioBounds

// ScenarioBounds describes pass/fail bounds for one named scenario.
//
//nolint:tagliatelle // Keys match thresholds.yaml.
type ScenarioBounds struct {
	MinERLE       *float64 `yaml:"min_erle_db,omitempty"`
	MinSNRGain    *float64 `yaml:"min_snr_gain_db,omitempty"`
	MaxLSD        *float64 `yaml:"max_lsd_db,omitempty"`
	MaxCepstralCD *float64 `yaml:"max_cepstral_db,omitempty"`
	MaxAttackS    *float64 `yaml:"max_attack_s,omitempty"`
	MaxPeakDBFS   *float64 `yaml:"max_peak_dbfs,omitempty"`
	MinCornerDB   *float64 `yaml:"min_corner_db,omitempty"`
	Notes         string   `yaml:"notes,omitempty"`
}

// LoadThresholds parses thresholds.yaml relative to this package's
// directory.
func LoadThresholds() (Thresholds, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, ErrThresholdsPath
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "thresholds.yaml"))
	if err != nil {
		return nil, fmt.Errorf("property: read thresholds: %w", err)
	}
	var th Thresholds
	if err := yaml.Unmarshal(data, &th); err != nil {
		return nil, fmt.Errorf("property: parse thresholds: %w", err)
	}

	return th, nil
}

// MustBounds fetches the bounds for the named scenario or fails the test.
func MustBounds(tb testing.TB, scenario string) ScenarioBounds {
	tb.Helper()
	th, err := LoadThresholds()
	require.NoError(tb, err, "loading thresholds")
	bounds, ok := th[scenario]
	require.True(tb, ok, "no thresholds entry for scenario %q", scenario)

	return bounds
}

// Pipeline bridges raw planar buffers and the apm.Processor, splitting a
// long buffer into 10 ms frames and processing each in turn. The frame
// buffer is reused across calls so the hot loop allocates nothing.
type Pipeline struct {
	processor *apm.Processor
	rate      apm.SampleRate
	frame     *apm.Frame
}

// NewPipeline wraps a Processor for batch processing of a single-channel
// stream at the given sample rate.
func NewPipeline(p *apm.Processor, rate apm.SampleRate) *Pipeline {
	return &Pipeline{processor: p, rate: rate, frame: apm.NewFrame(rate, 1)}
}

// ProcessStream runs near-end samples through the pipeline 10 ms at a
// time. Output is written into outBuf (which may alias near). Returns
// the number of samples processed.
func (p *Pipeline) ProcessStream(near, outBuf []float32) (int, error) {
	if len(outBuf) < len(near) {
		return 0, ErrShortOutput
	}
	per := p.rate.SamplesPerFrame()
	n := (len(near) / per) * per
	for i := 0; i < n; i += per {
		copy(p.frame.Data[0], near[i:i+per])
		if err := p.processor.ProcessStream(p.frame); err != nil {
			return i, err
		}
		copy(outBuf[i:i+per], p.frame.Data[0])
	}

	return n, nil
}

// ProcessReverseStream pushes far-end samples through the reverse stream.
// Length must be a multiple of one 10 ms frame.
func (p *Pipeline) ProcessReverseStream(far []float32) error {
	per := p.rate.SamplesPerFrame()
	for i := 0; i+per <= len(far); i += per {
		copy(p.frame.Data[0], far[i:i+per])
		if err := p.processor.ProcessReverseStream(p.frame); err != nil {
			return err
		}
	}

	return nil
}
