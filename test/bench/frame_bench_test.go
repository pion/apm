// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package bench

import (
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
