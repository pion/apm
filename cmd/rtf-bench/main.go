// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Command rtf-bench measures the real-time factor (RTF) of a Processor
// configuration over a configurable amount of synthetic audio. It is the
// standalone equivalent of TestRTF_PassthroughIsRealTime, useful for
// profiling under pprof or comparing branches outside of `go test`.
//
// RTF = wall_clock_seconds / audio_seconds. RTF < 1 means real-time.
//
// Usage:
//
//	rtf-bench -seconds 30 -rate 48000 -channels 2 -ns -agc
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime/pprof"
	"time"

	"github.com/pion/apm/apm"
	"github.com/pion/apm/test/synth"
)

var errInvalidRate = errors.New("invalid -rate")

type options struct {
	seconds    int
	rate       int
	channels   int
	hpf        bool
	ns         bool
	agc        bool
	aec        bool
	cpuProfile string
	jsonOut    string
}

func main() {
	var opts options
	flag.IntVar(&opts.seconds, "seconds", 30, "audio duration to process (s)")
	flag.IntVar(&opts.rate, "rate", 16000, "sample rate (Hz): 8000|16000|32000|48000")
	flag.IntVar(&opts.channels, "channels", 1, "channel count")
	flag.BoolVar(&opts.hpf, "hpf", false, "enable HPF")
	flag.BoolVar(&opts.ns, "ns", false, "enable NS")
	flag.BoolVar(&opts.agc, "agc", false, "enable AGC")
	flag.BoolVar(&opts.aec, "aec", false, "enable AEC")
	flag.StringVar(&opts.cpuProfile, "cpuprofile", "", "write CPU profile to file")
	flag.StringVar(&opts.jsonOut, "json", "", "write a one-line JSON result to file")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "rtf-bench:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	sr := apm.SampleRate(opts.rate)
	if !sr.Valid() {
		return fmt.Errorf("%w %d", errInvalidRate, opts.rate)
	}
	cfg := apm.DefaultConfig(sr, opts.channels)
	cfg.HPF.Enabled = opts.hpf
	cfg.NS.Enabled = opts.ns
	cfg.AGC.Enabled = opts.agc
	cfg.AEC.Enabled = opts.aec
	proc, err := apm.New(cfg)
	if err != nil {
		return fmt.Errorf("apm.New: %w", err)
	}

	per := sr.SamplesPerFrame()
	totalFrames := opts.rate * opts.seconds / per
	rng := rand.New(rand.NewPCG(7, 7)) //nolint:gosec // Deterministic benchmark signal.
	// Pre-fill with white noise so the workload isn't trivial-zero data.
	frame := noiseFrame(sr, opts.channels, rng)
	// The echo canceller only processes the capture once it has render
	// frames.
	var far *apm.Frame
	if opts.aec {
		far = noiseFrame(sr, opts.channels, rng)
		_ = proc.ProcessReverseStream(far)
	}
	_ = proc.ProcessStream(frame) // warm-up

	if opts.cpuProfile != "" {
		stop, err := startProfile(opts.cpuProfile)
		if err != nil {
			return err
		}
		defer stop()
	}

	start := time.Now()
	for range totalFrames {
		if far != nil {
			_ = proc.ProcessReverseStream(far)
		}
		_ = proc.ProcessStream(frame)
	}
	wall := time.Since(start)
	audio := time.Duration(opts.seconds) * time.Second
	rtf := float64(wall) / float64(audio)

	fmt.Printf("RTF=%.4f audio=%v wall=%v frames=%d cfg=%+v\n", rtf, audio, wall, totalFrames, cfg)
	if opts.jsonOut == "" {
		return nil
	}

	return writeJSON(opts.jsonOut, map[string]any{
		"seconds":  opts.seconds,
		"rate":     opts.rate,
		"channels": opts.channels,
		"frames":   totalFrames,
		"wall_ns":  wall.Nanoseconds(),
		"rtf":      rtf,
		"config":   cfg,
	})
}

// noiseFrame returns a frame of white noise.
func noiseFrame(rate apm.SampleRate, channels int, rng *rand.Rand) *apm.Frame {
	frame := apm.NewFrame(rate, channels)
	for c := range channels {
		copy(frame.Data[c], synth.WhiteNoise(rate.SamplesPerFrame(), rng))
	}

	return frame
}

// startProfile starts a CPU profile written to path and returns its stop function.
func startProfile(path string) (func(), error) {
	f, err := os.Create(path) //nolint:gosec // Path given by a CLI flag.
	if err != nil {
		return nil, fmt.Errorf("create cpu profile: %w", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("start cpu profile: %w", err)
	}

	return func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	}, nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path) //nolint:gosec // Path given by a CLI flag.
	if err != nil {
		return fmt.Errorf("create json out: %w", err)
	}
	if err := json.NewEncoder(f).Encode(v); err != nil {
		_ = f.Close()

		return fmt.Errorf("encode json: %w", err)
	}

	return f.Close()
}
