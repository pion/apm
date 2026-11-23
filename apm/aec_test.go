// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package apm

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/pion/apm/aec"
	"github.com/pion/apm/hpf"
	"github.com/pion/apm/internal/subband"
	"github.com/stretchr/testify/require"
)

// echoChain drives an echo canceller like upstream APM does for a stream
// with nothing else enabled. It is the reference of the Processor tests.
type echoChain struct {
	filter                          *hpf.Filter
	canceller                       *aec.Canceller
	renderSplitter, captureSplitter *subband.SplittingFilter
	renderBands, captureBands       [][][]float32
	renderSplit, captureSplit       [][][]float32
}

func newEchoChain(t *testing.T, rate SampleRate, numRender, numCapture int) *echoChain {
	t.Helper()
	var chain echoChain
	var err error
	chain.filter, err = hpf.New(int(rate), numCapture)
	require.NoError(t, err)
	chain.canceller, err = aec.New(int(rate), numRender, numCapture)
	require.NoError(t, err)
	chain.renderSplitter, chain.renderBands, err = newSplitter(rate, numRender)
	require.NoError(t, err)
	chain.captureSplitter, chain.captureBands, err = newSplitter(rate, numCapture)
	require.NoError(t, err)
	chain.renderSplit = splitBandViews(chain.renderBands, numRender)
	chain.captureSplit = splitBandViews(chain.captureBands, numCapture)

	return &chain
}

// analyzeRender takes a render frame on the int16 scale.
func (c *echoChain) analyzeRender(t *testing.T, render [][]float32) {
	t.Helper()
	if c.renderSplitter == nil {
		setSingleBand(c.renderSplit, render)
	} else {
		require.NoError(t, c.renderSplitter.Analysis(render, c.renderBands))
	}
	require.NoError(t, c.canceller.AnalyzeRender(c.renderSplit))
}

// processCapture processes a capture frame on the int16 scale in place.
// The stream delay is set when delayMs is not negative.
func (c *echoChain) processCapture(t *testing.T, capture [][]float32, delayMs int) {
	t.Helper()
	require.NoError(t, c.filter.Process(capture))
	require.NoError(t, c.canceller.AnalyzeCapture(capture))
	if c.captureSplitter == nil {
		setSingleBand(c.captureSplit, capture)
	} else {
		require.NoError(t, c.captureSplitter.Analysis(capture, c.captureBands))
	}
	if delayMs >= 0 {
		c.canceller.SetAudioBufferDelay(delayMs)
	}
	require.NoError(t, c.canceller.ProcessCapture(c.captureSplit, false))
	if c.captureSplitter != nil {
		require.NoError(t, c.captureSplitter.Synthesis(c.captureBands, capture))
	}
}

// echoFrames fills a render frame with noise and a capture frame with its
// echo and weak noise. The capture signal is saturated when loud is set.
func echoFrames(rng *rand.Rand, far, near *Frame, loud bool) {
	for ch := range far.Data {
		for j := range far.Data[ch] {
			far.Data[ch][j] = float32(rng.NormFloat64() * 0.1)
		}
	}
	for ch := range near.Data {
		render := far.Data[ch%len(far.Data)]
		for j := range near.Data[ch] {
			sample := 0.5*render[j] + float32(rng.NormFloat64()*0.01)
			if loud {
				sample = max(-1, min(1, 20*sample))
			}
			near.Data[ch][j] = sample
		}
	}
}

func cloneScaledUp(data [][]float32) [][]float32 {
	clone := make([][]float32, len(data))
	for ch := range data {
		clone[ch] = append([]float32(nil), data[ch]...)
	}
	scaleUp(clone)

	return clone
}

// The Processor drives the echo canceller like upstream APM: the render
// signal is analyzed split in bands, the capture signal is high-pass
// filtered and analyzed on the full band, then processed split in bands,
// for every channel.
func TestProcessor_DrivesEchoCanceller(t *testing.T) {
	for _, tc := range []struct {
		rate                  SampleRate
		numRender, numCapture int
	}{{Rate16k, 1, 1}, {Rate32k, 2, 2}, {Rate48k, 2, 1}, {Rate48k, 1, 2}} {
		t.Run(fmt.Sprintf("%d_%d_%d", tc.rate, tc.numRender, tc.numCapture), func(t *testing.T) {
			cfg := DefaultConfig(tc.rate, tc.numCapture)
			cfg.ReverseNumChannels = tc.numRender
			cfg.MaxProcessingRate = tc.rate
			cfg.AEC.Enabled = true
			proc, err := New(cfg)
			require.NoError(t, err)
			chain := newEchoChain(t, tc.rate, tc.numRender, tc.numCapture)

			rng := rand.New(rand.NewPCG(29, 0)) //nolint:gosec // Deterministic test signal.
			for i := range 120 {
				far, near := NewFrame(tc.rate, tc.numRender), NewFrame(tc.rate, tc.numCapture)
				echoFrames(rng, far, near, i >= 60 && i < 70)

				chain.analyzeRender(t, cloneScaledUp(far.Data))
				want := cloneScaledUp(near.Data)
				chain.processCapture(t, want, -1)
				scaleDown(want)

				require.NoError(t, proc.ProcessReverseStream(far))
				require.NoError(t, proc.ProcessStream(near))
				require.Equal(t, want, near.Data, "frame %d", i)
			}
		})
	}
}

// processorEchoOutputs runs a Processor and an echoChain on the same 16
// kHz frames and returns their outputs. The render frames 40 to 55 are
// skipped, which makes the echo canceller reset its render buffer.
// control is called on the Processor before every capture frame, and
// chainDelayMs gives the stream delay of the chain, or -1 for none.
func processorEchoOutputs(
	t *testing.T, control func(index int, proc *Processor), chainDelayMs func(index int) int,
) (got, want [][]float32) {
	t.Helper()
	cfg := DefaultConfig(Rate16k, 1)
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)
	chain := newEchoChain(t, Rate16k, 1, 1)

	rng := rand.New(rand.NewPCG(37, 0)) //nolint:gosec // Deterministic test signal.
	for i := range 150 {
		far, near := NewFrame(Rate16k, 1), NewFrame(Rate16k, 1)
		echoFrames(rng, far, near, false)
		reference := cloneScaledUp(near.Data)
		if i < 40 || i >= 56 {
			chain.analyzeRender(t, cloneScaledUp(far.Data))
			require.NoError(t, proc.ProcessReverseStream(far))
		}
		chain.processCapture(t, reference, chainDelayMs(i))
		scaleDown(reference)
		control(i, proc)
		require.NoError(t, proc.ProcessStream(near))
		got = append(got, near.Data[0])
		want = append(want, reference[0])
	}

	return got, want
}

// Like upstream, the stream delay is passed to the echo canceller before
// the capture frame is processed.
func TestProcessor_PassesStreamDelay(t *testing.T) {
	const delayMs = 100
	setDelay := func(_ int, proc *Processor) { require.NoError(t, proc.SetStreamDelayMS(delayMs)) }
	got, want := processorEchoOutputs(t, setDelay, func(int) int { return delayMs })
	require.Equal(t, want, got)

	// The delay changes the output.
	_, without := processorEchoOutputs(t, setDelay, func(int) int { return -1 })
	require.NotEqual(t, without, got)
}
