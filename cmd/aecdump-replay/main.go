// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Command aecdump-replay reads a WebRTC .aecdump debug recording, drives
// the APM Processor with its events, and writes a WAV of the near-end
// output. It is the equivalent of upstream's audioproc_f.
//
// Usage:
//
//	aecdump-replay -in capture.aecdump -out result.wav
//	aecdump-replay -in capture.aecdump -out result.wav -force-ns -force-ns-level=2
//
// The -force-* flags override the configuration recorded in the dump,
// which is useful when validating one module at a time.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pion/apm/apm"
	"github.com/pion/apm/internal/wav"
	webrtcproto "github.com/pion/apm/third_party/webrtc-proto"
)

var (
	errInitPayload     = errors.New("INIT event without payload")
	errMultipleInit    = errors.New("multiple INIT events not supported")
	errBeforeInit      = errors.New("stream event before INIT")
	errSampleRate      = errors.New("unsupported sample rate")
	errChannelMismatch = errors.New("frame channel mismatch")
	errFrameLength     = errors.New("frame length mismatch")
)

// options holds the -force-* overrides.
type options struct {
	hpf     bool
	ns      bool
	nsLevel int
	agc     bool
	aec     bool
}

func main() {
	in := flag.String("in", "", "input .aecdump file")
	out := flag.String("out", "", "output WAV file (near-end processed)")
	var opts options
	flag.BoolVar(&opts.hpf, "force-hpf", false, "force HPF on regardless of dump config")
	flag.BoolVar(&opts.ns, "force-ns", false, "force NS on")
	flag.IntVar(&opts.nsLevel, "force-ns-level", -1, "override NS level (0..3); -1 leaves untouched")
	flag.BoolVar(&opts.agc, "force-agc", false, "force AGC on")
	flag.BoolVar(&opts.aec, "force-aec", false, "force AEC on")
	flag.Parse()

	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, opts); err != nil {
		fmt.Fprintln(os.Stderr, "aecdump-replay:", err)
		os.Exit(1)
	}
}

// replayer applies dump events to a Processor and writes the output WAV.
type replayer struct {
	opts      options
	outPath   string
	pipeline  *replayPipeline
	writer    *wav.Writer
	numFrames int
}

func run(inPath, outPath string, opts options) error {
	f, err := os.Open(inPath) //nolint:gosec // Path given by a CLI flag.
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	rep := &replayer{opts: opts, outPath: outPath}
	if err := rep.replay(webrtcproto.NewReader(f)); err != nil {
		if rep.writer != nil {
			_ = rep.writer.Close()
		}

		return err
	}
	log.Printf("replayed %d capture frames to %s", rep.numFrames, outPath)
	if rep.writer == nil {
		return nil
	}

	return rep.writer.Close()
}

func (r *replayer) replay(rd *webrtcproto.Reader) error {
	for {
		ev, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dump read at frame %d: %w", r.numFrames, err)
		}
		if err := r.handle(ev); err != nil {
			return err
		}
	}
}

func (r *replayer) handle(ev *webrtcproto.Event) error {
	switch ev.Type {
	case webrtcproto.EventInit:
		return r.init(ev.Init)
	case webrtcproto.EventConfig:
		// Surface the recorded config on stderr for transparency.
		if ev.Config != nil {
			log.Printf("dump config: aec=%t agc=%t ns=%t hpf=%t ns_lvl=%d",
				ev.Config.AECEnabled, ev.Config.AGCEnabled,
				ev.Config.NSEnabled, ev.Config.HPFEnabled, ev.Config.NSLevel)
		}
	case webrtcproto.EventReverseStream:
		if r.pipeline == nil {
			return errBeforeInit
		}
		if ev.ReverseStream == nil || len(ev.ReverseStream.Channels) == 0 {
			return nil // Legacy interleaved data is not supported.
		}

		return r.pipeline.processReverse(ev.ReverseStream.Channels)
	case webrtcproto.EventStream:
		return r.stream(ev.Stream)
	default:
	}

	return nil
}

func (r *replayer) init(in *webrtcproto.Init) error {
	if in == nil {
		return errInitPayload
	}
	if r.pipeline != nil {
		return errMultipleInit
	}
	pipe, err := newReplayPipeline(in, r.opts)
	if err != nil {
		return err
	}
	r.pipeline = pipe
	r.writer, err = wav.CreateWriter(r.outPath, int(in.SampleRate), int(in.NumOutputChannels), wav.Float32)

	return err
}

func (r *replayer) stream(st *webrtcproto.Stream) error {
	if r.pipeline == nil {
		return errBeforeInit
	}
	if st == nil || len(st.InputChannels) == 0 {
		return nil
	}
	if st.DelayMS > 0 {
		_ = r.pipeline.proc.SetStreamDelayMS(int(st.DelayMS))
	}
	out, err := r.pipeline.processStream(st.InputChannels)
	if err != nil {
		return err
	}
	if err := writeInterleaved(r.writer, out); err != nil {
		return err
	}
	r.numFrames++

	return nil
}

type replayPipeline struct {
	proc      *apm.Processor
	nearFrame *apm.Frame
	farFrame  *apm.Frame
}

func newReplayPipeline(in *webrtcproto.Init, opts options) (*replayPipeline, error) {
	nearRate := apm.SampleRate(in.SampleRate)
	if !nearRate.Valid() {
		return nil, fmt.Errorf("%w: %d", errSampleRate, in.SampleRate)
	}
	farRate := nearRate
	if in.ReverseSampleRate > 0 {
		farRate = apm.SampleRate(in.ReverseSampleRate)
	}
	cfg := apm.DefaultConfig(nearRate, int(in.NumInputChannels))
	cfg.ReverseSampleRate = farRate
	cfg.ReverseNumChannels = int(in.NumReverseChannels)
	cfg.HPF.Enabled = opts.hpf
	cfg.NS.Enabled = opts.ns
	if opts.nsLevel >= 0 {
		cfg.NS.Level = apm.NSLevel(opts.nsLevel)
	}
	cfg.AGC.Enabled = opts.agc
	cfg.AEC.Enabled = opts.aec
	proc, err := apm.New(cfg)
	if err != nil {
		return nil, err
	}

	return &replayPipeline{
		proc:      proc,
		nearFrame: apm.NewFrame(nearRate, int(in.NumInputChannels)),
		farFrame:  apm.NewFrame(farRate, int(in.NumReverseChannels)),
	}, nil
}

func (p *replayPipeline) processStream(in [][]float32) ([][]float32, error) {
	if err := fillFrame(p.nearFrame, in); err != nil {
		return nil, fmt.Errorf("near: %w", err)
	}
	if err := p.proc.ProcessStream(p.nearFrame); err != nil {
		return nil, err
	}
	out := make([][]float32, p.nearFrame.NumChannels())
	for c := range out {
		out[c] = append([]float32(nil), p.nearFrame.Data[c]...)
	}

	return out, nil
}

func (p *replayPipeline) processReverse(in [][]float32) error {
	if err := fillFrame(p.farFrame, in); err != nil {
		return fmt.Errorf("far: %w", err)
	}

	return p.proc.ProcessReverseStream(p.farFrame)
}

// fillFrame copies in into frame after checking its shape.
func fillFrame(frame *apm.Frame, in [][]float32) error {
	if len(in) != frame.NumChannels() {
		return fmt.Errorf("%w: got %d want %d", errChannelMismatch, len(in), frame.NumChannels())
	}
	n := frame.NumSamples()
	for c := range in {
		if len(in[c]) != n {
			return fmt.Errorf("%w: channel %d has %d, want %d", errFrameLength, c, len(in[c]), n)
		}
		copy(frame.Data[c], in[c])
	}

	return nil
}

func writeInterleaved(w *wav.Writer, channels [][]float32) error {
	if len(channels) == 0 {
		return nil
	}
	n := len(channels[0])
	buf := make([]float32, n*len(channels))
	for i := range n {
		for c := range channels {
			buf[i*len(channels)+c] = channels[c][i]
		}
	}

	return w.WriteFloat32(buf)
}
