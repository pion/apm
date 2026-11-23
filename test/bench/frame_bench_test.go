// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package bench

import (
	"math/rand/v2"
	"testing"

	"github.com/pion/apm/apm"
	"github.com/stretchr/testify/require"
)

func newProcessor(b *testing.B, rate apm.SampleRate, numCh int) *apm.Processor {
	b.Helper()
	proc, err := apm.New(apm.DefaultConfig(rate, numCh))
	require.NoError(b, err)

	return proc
}

// BenchmarkProcessStream_PassthroughMono16k measures the time and allocs
// to process one 10 ms mono frame at 16 kHz through the Processor.
// Target: <100 ns/op and 0 allocs/op for the eventual implemented modules.
func BenchmarkProcessStream_PassthroughMono16k(b *testing.B) {
	proc := newProcessor(b, apm.Rate16k, 1)
	frame := apm.NewFrame(apm.Rate16k, 1)
	// Warm up.
	require.NoError(b, proc.ProcessStream(frame))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = proc.ProcessStream(frame)
	}
}

func BenchmarkProcessStream_PassthroughStereo48k(b *testing.B) {
	proc := newProcessor(b, apm.Rate48k, 2)
	frame := apm.NewFrame(apm.Rate48k, 2)
	_ = proc.ProcessStream(frame)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = proc.ProcessStream(frame)
	}
}

func BenchmarkProcessReverseStream_Mono16k(b *testing.B) {
	proc := newProcessor(b, apm.Rate16k, 1)
	frame := apm.NewFrame(apm.Rate16k, 1)
	_ = proc.ProcessReverseStream(frame)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = proc.ProcessReverseStream(frame)
	}
}

// BenchmarkFullPipeline_PassthroughMono16k drives both reverse and near
// streams as a realistic workload would, end to end per frame.
func BenchmarkFullPipeline_PassthroughMono16k(b *testing.B) {
	proc := newProcessor(b, apm.Rate16k, 1)
	near := apm.NewFrame(apm.Rate16k, 1)
	far := apm.NewFrame(apm.Rate16k, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = proc.ProcessReverseStream(far)
		_ = proc.ProcessStream(near)
	}
}

// benchmarkEchoCanceller measures the time to process one 10 ms far-end
// frame and one near-end frame that holds its echo, with the echo
// canceller enabled.
func benchmarkEchoCanceller(b *testing.B, rate apm.SampleRate, numCh int) {
	b.Helper()
	cfg := apm.DefaultConfig(rate, numCh)
	cfg.AEC.Enabled = true
	proc, err := apm.New(cfg)
	require.NoError(b, err)

	rng := rand.New(rand.NewPCG(5, 5)) //nolint:gosec // Deterministic benchmark signal.
	far, near, echo := apm.NewFrame(rate, numCh), apm.NewFrame(rate, numCh), apm.NewFrame(rate, numCh)
	for ch := range far.Data {
		for i := range far.Data[ch] {
			far.Data[ch][i] = float32(rng.NormFloat64() * 0.1)
			echo.Data[ch][i] = 0.5 * far.Data[ch][i]
		}
	}
	process := func() {
		for ch := range near.Data {
			copy(near.Data[ch], echo.Data[ch])
		}
		_ = proc.ProcessReverseStream(far)
		_ = proc.ProcessStream(near)
	}
	// Let the canceller detect stereo content, which allocates.
	for range 300 {
		process()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		process()
	}
}

func BenchmarkFullPipeline_AECMono16k(b *testing.B) {
	benchmarkEchoCanceller(b, apm.Rate16k, 1)
}

func BenchmarkFullPipeline_AECStereo48k(b *testing.B) {
	benchmarkEchoCanceller(b, apm.Rate48k, 2)
}
