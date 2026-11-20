// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RejectsBadRate(t *testing.T) {
	_, err := New(DefaultConfig(), 8000, 1)
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
	_, err = New(DefaultConfig(), 48000, 1) // Multi-band is not ported.
	assert.ErrorIs(t, err, ErrUnsupportedSampleRate)
}

// TestCanceller_ConvergesOnFixedEchoPath drives the canceller with a known
// far-end signal convolved with a fixed IR. After enough frames the
// filter should converge and reduce echo by a meaningful margin.
func TestCanceller_ConvergesOnFixedEchoPath(t *testing.T) {
	canceller, err := New(DefaultConfig(), 16000, 1)
	require.NoError(t, err)
	rng := rand.New(rand.NewPCG(1, 1)) //nolint:gosec // Deterministic test signal.

	const seconds = 5
	const frames = seconds * 100 // 100 frames/s @ 10 ms each
	const samplesPerFrame = kFrameSize

	// IR: exponentially-decaying random response, 256 samples (~16 ms),
	// well within the 13-block (832-sample) filter.
	const irLen = 256
	ir := make([]float32, irLen)
	decay := math.Pow(10, -3.0/(0.05*16000)) // RT60 ≈ 50 ms
	env := 1.0
	for i := range ir {
		ir[i] = float32(rng.NormFloat64() * env * 0.1)
		env *= decay
	}
	ir[0] = 1 // direct path

	// Generate the full far-end signal first so the echo can be
	// precomputed. AEC3 operates on float samples at int16 scale (the
	// noise gates in the gain update assume this range).
	const scale = 32768.0
	total := frames * samplesPerFrame
	far := make([]float32, total)
	for i := range far {
		far[i] = float32(rng.NormFloat64() * 0.3 * scale)
	}
	echo := convolve(far, ir)
	mic := echo[:total]

	out := make([]float32, total)
	farFrame := make([]float32, samplesPerFrame)
	micFrame := make([]float32, samplesPerFrame)
	for f := range frames {
		off := f * samplesPerFrame
		copy(farFrame, far[off:off+samplesPerFrame])
		copy(micFrame, mic[off:off+samplesPerFrame])
		require.NoError(t, canceller.AnalyzeRender([][]float32{farFrame}))
		require.NoError(t, canceller.ProcessCapture([][]float32{micFrame}))
		copy(out[off:], micFrame)
	}

	// Examine the converged tail (last 1.5 s).
	tailStart := max(total-int(seconds*16000)*3/10, 0)
	erle := computeERLE(mic[tailStart:], out[tailStart:], 160)
	t.Logf("converged ERLE = %.2f dB (frames=%d, IR length=%d)", erle, frames, irLen)
	assert.GreaterOrEqual(t, erle, 5.0, "expected meaningful ERLE on fixed echo path")
}

func convolve(x, h []float32) []float32 {
	y := make([]float32, len(x)+len(h)-1)
	for i, xi := range x {
		if xi == 0 {
			continue
		}
		for j, hj := range h {
			y[i+j] += xi * hj
		}
	}

	return y
}

func computeERLE(mic, residual []float32, frameLen int) float64 {
	if frameLen <= 0 || len(mic) < frameLen {
		return math.NaN()
	}
	var pm, pr float64
	for i := 0; i+frameLen <= len(mic); i += frameLen {
		var fm, fr float64
		for j := range frameLen {
			m := float64(mic[i+j])
			r := float64(residual[i+j])
			fm += m * m
			fr += r * r
		}
		pm += fm
		pr += fr
	}
	if pr <= 0 {
		return math.Inf(+1)
	}
	if pm <= 0 {
		return math.Inf(-1)
	}

	return 10 * math.Log10(pm/pr)
}

func TestCanceller_ZeroAllocSteadyState(t *testing.T) {
	canceller, err := New(DefaultConfig(), 16000, 1)
	require.NoError(t, err)

	far := [][]float32{make([]float32, kFrameSize)}
	near := [][]float32{make([]float32, kFrameSize)}
	process := func() {
		_ = canceller.AnalyzeRender(far)
		_ = canceller.ProcessCapture(near)
	}
	for range 10 {
		process()
	}
	assert.Zero(t, testing.AllocsPerRun(100, process))
}

// With no far-end signal the canceller only delays the capture signal by
// the block framer's one block.
func TestCanceller_DelaysCaptureByOneBlock(t *testing.T) {
	canceller, err := New(DefaultConfig(), 16000, 1)
	require.NoError(t, err)

	const numFrames = 10
	far := [][]float32{make([]float32, kFrameSize)}
	input := make([]float32, numFrames*kFrameSize)
	for i := range input {
		input[i] = float32(i%100) - 50
	}
	output := make([]float32, 0, len(input))
	for i := range numFrames {
		near := [][]float32{append([]float32(nil), input[i*kFrameSize:(i+1)*kFrameSize]...)}
		require.NoError(t, canceller.AnalyzeRender(far))
		require.NoError(t, canceller.ProcessCapture(near))
		output = append(output, near[0]...)
	}

	assert.Equal(t, make([]float32, kBlockSize), output[:kBlockSize])
	assert.Equal(t, input[:len(input)-kBlockSize], output[kBlockSize:])
}
