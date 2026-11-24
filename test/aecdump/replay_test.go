// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aecdump

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pion/apm/apm"
	"github.com/pion/apm/internal/wav"
	webrtcproto "github.com/pion/apm/third_party/webrtc-proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplay_PassthroughRoundTrip writes a synthetic 1-second .aecdump,
// replays it through a passthrough Processor, and verifies the output
// WAV has the right shape and samples. This is the smoke test for the
// whole .aecdump to Processor to WAV path, independent of any module.
func TestReplay_PassthroughRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "synthetic.aecdump")
	outPath := filepath.Join(dir, "out.wav")

	const rate = 16000
	const frames = 100 // 1 second of 10 ms frames
	const samplesPerFrame = 160
	require.NoError(t, writeSyntheticDump(dumpPath, rate, frames))
	require.NoError(t, runReplay(dumpPath, outPath))

	channels, gotRate, err := wav.ReadAll(outPath)
	require.NoError(t, err)
	require.Equal(t, rate, gotRate)
	require.Len(t, channels, 1)
	require.Len(t, channels[0], frames*samplesPerFrame)
	// Passthrough preserves samples; float32 round-trips exactly through WAV.
	for i := range 5 {
		idx := i * samplesPerFrame
		assert.Equal(t, float32(idx%2000)/2000, channels[0][idx], "sample %d", idx)
	}
}

func writeSyntheticDump(path string, rate, frames int) error {
	file, err := os.Create(path) //nolint:gosec // Test temp dir path.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := webrtcproto.WriteEvent(file, &webrtcproto.Event{
		Type: webrtcproto.EventInit,
		Init: &webrtcproto.Init{
			SampleRate:         int32(rate), //nolint:gosec // Small test constant.
			NumInputChannels:   1,
			NumOutputChannels:  1,
			NumReverseChannels: 1,
			ReverseSampleRate:  int32(rate), //nolint:gosec // Small test constant.
			OutputSampleRate:   int32(rate), //nolint:gosec // Small test constant.
		},
	}); err != nil {
		return err
	}
	per := rate * 10 / 1000 // samples per 10 ms frame
	for i := range frames {
		near := make([]float32, per)
		far := make([]float32, per)
		for j := range near {
			v := float32((i*per+j)%2000) / 2000
			near[j] = v
			far[j] = v * 0.5
		}
		if err := webrtcproto.WriteEvent(file, &webrtcproto.Event{
			Type:          webrtcproto.EventReverseStream,
			ReverseStream: &webrtcproto.ReverseStream{Channels: [][]float32{far}},
		}); err != nil {
			return err
		}
		if err := webrtcproto.WriteEvent(file, &webrtcproto.Event{
			Type:   webrtcproto.EventStream,
			Stream: &webrtcproto.Stream{DelayMS: 50, InputChannels: [][]float32{near}},
		}); err != nil {
			return err
		}
	}

	return nil
}

// replayer is a thin in-process equivalent of cmd/aecdump-replay, so tests
// don't have to exec the built binary.
type replayer struct {
	outPath   string
	processor *apm.Processor
	writer    *wav.Writer
	nearFrame *apm.Frame
	farFrame  *apm.Frame
}

func runReplay(dumpPath, outPath string) error {
	src, err := os.Open(dumpPath) //nolint:gosec // Test temp dir path.
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	rep := &replayer{outPath: outPath}
	defer func() {
		if rep.writer != nil {
			_ = rep.writer.Close()
		}
	}()
	reader := webrtcproto.NewReader(src)
	for {
		ev, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := rep.handle(ev); err != nil {
			return err
		}
	}
}

func (r *replayer) handle(ev *webrtcproto.Event) error {
	switch ev.Type { //nolint:exhaustive // Other events are not replayed.
	case webrtcproto.EventInit:
		rate := apm.SampleRate(ev.Init.SampleRate)
		numCh := int(ev.Init.NumInputChannels)
		proc, err := apm.New(apm.DefaultConfig(rate, numCh))
		if err != nil {
			return err
		}
		r.processor = proc
		r.nearFrame = apm.NewFrame(rate, numCh)
		r.farFrame = apm.NewFrame(apm.SampleRate(ev.Init.ReverseSampleRate), int(ev.Init.NumReverseChannels))
		r.writer, err = wav.CreateWriter(r.outPath, int(rate), numCh, wav.Float32)

		return err
	case webrtcproto.EventReverseStream:
		for c, ch := range ev.ReverseStream.Channels {
			copy(r.farFrame.Data[c], ch)
		}

		return r.processor.ProcessReverseStream(r.farFrame)
	case webrtcproto.EventStream:
		for c, ch := range ev.Stream.InputChannels {
			copy(r.nearFrame.Data[c], ch)
		}
		if err := r.processor.ProcessStream(r.nearFrame); err != nil {
			return err
		}

		return r.writer.WriteFloat32(interleave(r.nearFrame))
	default:
		return nil
	}
}

func interleave(frame *apm.Frame) []float32 {
	n := frame.NumSamples()
	numCh := frame.NumChannels()
	buf := make([]float32, n*numCh)
	for i := range n {
		for c := range numCh {
			buf[i*numCh+c] = frame.Data[c][i]
		}
	}

	return buf
}
