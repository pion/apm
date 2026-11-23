// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package apm

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/pion/apm/aec"
	"github.com/pion/apm/hpf"
	"github.com/pion/apm/internal/subband"
	"github.com/stretchr/testify/require"
)

// echoChain drives an echo canceller like upstream APM does for a stream
// with nothing else enabled. It is the reference of the Processor tests.
type echoChain struct {
	filter *hpf.Filter
	// postFilter is nil below 48 kHz.
	postFilter                      *postFilter
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
	chain.postFilter = newPostFilterIfNeeded(int(rate), numCapture)
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
func (c *echoChain) processCapture(t *testing.T, capture [][]float32, delayMs int, levelChange bool) {
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
	require.NoError(t, c.canceller.ProcessCapture(c.captureSplit, levelChange))
	if c.captureSplitter != nil {
		require.NoError(t, c.captureSplitter.Synthesis(c.captureBands, capture))
	}
	if c.postFilter != nil {
		c.postFilter.process(capture)
	}
}

// echoSignal generates the frames of a render signal and of a capture
// signal that holds its echo, delayed by 20 ms.
type echoSignal struct {
	rng     *rand.Rand
	history [2]*Frame
}

func newEchoSignal(seed uint64, rate SampleRate, numRender int) *echoSignal {
	return &echoSignal{
		rng:     rand.New(rand.NewPCG(seed, 0)), //nolint:gosec // Deterministic test signal.
		history: [2]*Frame{NewFrame(rate, numRender), NewFrame(rate, numRender)},
	}
}

// frames fills a render frame with noise and a capture frame with the
// echo and weak noise. The capture signal is saturated when loud is set.
func (e *echoSignal) frames(far, near *Frame, loud bool) {
	rng := e.rng
	for ch := range far.Data {
		for j := range far.Data[ch] {
			far.Data[ch][j] = float32(rng.NormFloat64() * 0.1)
		}
	}
	source := e.history[0]
	e.history[0], e.history[1] = e.history[1], far
	for ch := range near.Data {
		render := source.Data[ch%len(source.Data)]
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
// for every channel. At 48 kHz the post filter runs last.
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

			signal := newEchoSignal(29, tc.rate, tc.numRender)
			for i := range 120 {
				far, near := NewFrame(tc.rate, tc.numRender), NewFrame(tc.rate, tc.numCapture)
				signal.frames(far, near, i >= 60 && i < 70)

				chain.analyzeRender(t, cloneScaledUp(far.Data))
				want := cloneScaledUp(near.Data)
				chain.processCapture(t, want, -1, false)
				scaleDown(want)

				require.NoError(t, proc.ProcessReverseStream(far))
				require.NoError(t, proc.ProcessStream(near))
				require.Equal(t, want, near.Data, "frame %d", i)
			}
		})
	}
}

// processorEchoOutputs runs a Processor and an echoChain on the same
// 16 kHz frames and returns their outputs. The render frames 40 to 55 are
// skipped, which makes the echo canceller reset its render buffer.
// control is called on the Processor before every capture frame, and
// chainControl gives the stream delay of the chain, or -1 for none, and
// its level change flag.
func processorEchoOutputs(
	t *testing.T, numFrames int, control func(index int, proc *Processor), chainControl func(index int) (int, bool),
) (got, want [][]float32) {
	t.Helper()
	cfg := DefaultConfig(Rate16k, 1)
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)
	chain := newEchoChain(t, Rate16k, 1, 1)

	signal := newEchoSignal(37, Rate16k, 1)
	for i := range numFrames {
		far, near := NewFrame(Rate16k, 1), NewFrame(Rate16k, 1)
		signal.frames(far, near, false)
		reference := cloneScaledUp(near.Data)
		if i < 40 || i >= 56 {
			chain.analyzeRender(t, cloneScaledUp(far.Data))
			require.NoError(t, proc.ProcessReverseStream(far))
		}
		delayMs, levelChange := chainControl(i)
		chain.processCapture(t, reference, delayMs, levelChange)
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
	got, want := processorEchoOutputs(t, 150, setDelay, func(int) (int, bool) { return delayMs, false })
	require.Equal(t, want, got)

	// The delay changes the output.
	_, without := processorEchoOutputs(t, 150, setDelay, func(int) (int, bool) { return -1, false })
	require.NotEqual(t, without, got)
}

// Like upstream, the echo canceller is told of a change of the analog
// level and of the playout volume, but not of their first values. The
// changes come after the first 2.5 seconds, during which they have no
// effect on the echo canceller.
func TestProcessor_FlagsLevelChanges(t *testing.T) {
	control := func(index int, proc *Processor) {
		switch {
		case index >= 410:
			proc.SetStreamAnalogLevel(120)
		case index >= 340:
			proc.SetStreamAnalogLevel(100)
		}
		switch index {
		case 330:
			proc.SetPlayoutVolume(200)
		case 370:
			proc.SetPlayoutVolume(150)
		}
	}
	got, want := processorEchoOutputs(t, 450, control, func(index int) (int, bool) {
		return -1, index == 370 || index == 410
	})
	require.Equal(t, want, got)

	// Each change changes the output.
	for _, changeIndex := range []int{370, 410} {
		_, without := processorEchoOutputs(t, 450, control, func(index int) (int, bool) {
			return -1, index == changeIndex
		})
		require.False(t, reflect.DeepEqual(without, got), "change at frame %d", changeIndex)
	}
}
