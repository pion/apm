// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/pion/apm/apm"
	"github.com/pion/apm/internal/wav"
	"github.com/pion/apm/test/metrics"
	"github.com/pion/apm/test/synth"
)

var (
	errNoChannels      = errors.New("no channels")
	errUnknownScenario = errors.New("unknown scenario")
)

// Score is the per-cell measurement bundle written to the JSON report.
// All fields are optional: scenarios omit metrics they don't measure.
//
//nolint:tagliatelle // Report keys are snake_case.
type Score struct {
	Cell       Cell    `json:"cell"`
	SegSNRdB   float64 `json:"seg_snr_db,omitempty"`
	SNRGainDB  float64 `json:"snr_gain_db,omitempty"`
	ERLEdB     float64 `json:"erle_db,omitempty"`
	LSDdB      float64 `json:"lsd_db,omitempty"`
	CDdB       float64 `json:"cd_db,omitempty"`
	PEAQLite   float64 `json:"peaq_lite,omitempty"`
	PeakDBFS   float64 `json:"peak_dbfs,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// scoreCell runs one matrix cell end to end: load audio, synthesize the
// test signal as specified by Scenario, process through the apm pipeline,
// compute relevant metrics.
//
// The function is deterministic given a fixed seed so reports are
// reproducible across runs.
func scoreCell(cell Cell) Score {
	score := Score{Cell: cell}
	clip, rate, err := wav.ReadAll(cell.ClipPath)
	if err != nil {
		score.Error = "load clip: " + err.Error()

		return score
	}
	if len(clip) == 0 {
		score.Error = "clip has " + errNoChannels.Error()

		return score
	}
	clean := clip[0]

	switch cell.Scenario {
	case scenarioNSClean:
		err = scoreNSClean(&score, clean, rate)
	case scenarioNSNoisy:
		err = scoreNSNoisy(&score, clean, rate)
	case scenarioAGCTarget:
		err = scoreAGC(&score, clean, rate)
	case scenarioAECEcho:
		rng := rand.New(rand.NewPCG(uint64(len(cell.ClipPath)), 0xCAFE)) //nolint:gosec // Deterministic test signal.
		err = scoreAEC(&score, clean, rate, rng)
	default:
		err = fmt.Errorf("%w %q", errUnknownScenario, cell.Scenario)
	}
	if err != nil {
		score.Error = err.Error()
	}

	return score
}

func scoreNSClean(score *Score, clean []float32, rate int) error {
	cfg := apm.DefaultConfig(apm.SampleRate(rate), 1)
	cfg.NS = apm.NSConfig{Enabled: true, Level: apm.NSModerate}
	out, err := process(cfg, apm.SampleRate(rate), clean)
	if err != nil {
		return err
	}
	score.LSDdB = metrics.LogSpectralDistance(clean, out, rate, 512, 256)
	score.CDdB = metrics.CepstralDistance(clean, out, rate, 512, 256, 13)
	score.PEAQLite = metrics.PEAQLite(clean, out, rate)
	score.DurationMS = (len(clean) * 1000) / rate

	return nil
}

func scoreNSNoisy(score *Score, clean []float32, rate int) error {
	noiseAll, _, err := wav.ReadAll(score.Cell.NoisePath)
	if err != nil {
		return fmt.Errorf("load noise: %w", err)
	}
	if len(noiseAll) == 0 {
		return fmt.Errorf("noise has %w", errNoChannels)
	}
	noise := noiseAll[0]
	// Pad/repeat noise to match clean length.
	if len(noise) < len(clean) {
		noise = repeatTo(noise, len(clean))
	} else {
		noise = noise[:len(clean)]
	}
	noisy, err := synth.MixAtSNR(clean, noise, score.Cell.SNRDB)
	if err != nil {
		return fmt.Errorf("mix: %w", err)
	}
	cfg := apm.DefaultConfig(apm.SampleRate(rate), 1)
	cfg.NS = apm.NSConfig{Enabled: true, Level: apm.NSHigh}
	out, err := process(cfg, apm.SampleRate(rate), noisy)
	if err != nil {
		return err
	}
	inSNR := metrics.SNR(clean, noisy)
	outSNR := metrics.SNR(clean, out)
	score.SNRGainDB = outSNR - inSNR
	score.LSDdB = metrics.LogSpectralDistance(clean, out, rate, 512, 256)
	score.PEAQLite = metrics.PEAQLite(clean, out, rate)
	score.DurationMS = (len(clean) * 1000) / rate

	return nil
}

func scoreAGC(score *Score, clean []float32, rate int) error {
	synth.ScaleToDBFS(clean, -25) // quiet input
	cfg := apm.DefaultConfig(apm.SampleRate(rate), 1)
	cfg.AGC = apm.AGCConfig{Enabled: true, TargetLevelDBFS: -10, CompressionGain: 9}
	out, err := process(cfg, apm.SampleRate(rate), clean)
	if err != nil {
		return err
	}
	score.PeakDBFS = metrics.PeakLevelDBFS(out)
	score.DurationMS = (len(clean) * 1000) / rate

	return nil
}

func scoreAEC(score *Score, clean []float32, rate int, rng *rand.Rand) error {
	var ir []float32
	if score.Cell.IRPath != "" {
		ch, _, err := wav.ReadAll(score.Cell.IRPath)
		if err != nil {
			return fmt.Errorf("load IR: %w", err)
		}
		if len(ch) > 0 {
			ir = ch[0]
		}
	}
	if len(ir) == 0 {
		ir = synth.ExpDecayIR(160, rate, 0.15, rng)
	}
	far := clean // use the clip as far-end
	echo := synth.Convolve(far, ir)[:len(far)]
	mic := append([]float32(nil), echo...) // pure echo, no near-end speech

	cfg := apm.DefaultConfig(apm.SampleRate(rate), 1)
	cfg.AEC.Enabled = true
	proc, err := apm.New(cfg)
	if err != nil {
		return err
	}
	frame := apm.NewFrame(apm.SampleRate(rate), 1)
	per := frame.NumSamples()

	// Push far-end first, then capture.
	for i := 0; i+per <= len(far); i += per {
		copy(frame.Data[0], far[i:i+per])
		_ = proc.ProcessReverseStream(frame)
	}
	out := make([]float32, len(mic))
	for i := 0; i+per <= len(mic); i += per {
		copy(frame.Data[0], mic[i:i+per])
		_ = proc.ProcessStream(frame)
		copy(out[i:i+per], frame.Data[0])
	}
	score.ERLEdB = metrics.MaxConvergedERLE(mic, out, per)
	score.DurationMS = (len(mic) * 1000) / rate

	return nil
}

func process(cfg apm.Config, rate apm.SampleRate, in []float32) ([]float32, error) {
	proc, err := apm.New(cfg)
	if err != nil {
		return nil, err
	}
	frame := apm.NewFrame(rate, 1)
	per := rate.SamplesPerFrame()
	out := make([]float32, (len(in)/per)*per)
	for i := 0; i+per <= len(in); i += per {
		copy(frame.Data[0], in[i:i+per])
		_ = proc.ProcessStream(frame)
		copy(out[i:i+per], frame.Data[0])
	}

	return out, nil
}

func repeatTo(x []float32, n int) []float32 {
	out := make([]float32, n)
	if len(x) == 0 {
		return out
	}
	for i := range n {
		out[i] = x[i%len(x)]
	}

	return out
}
