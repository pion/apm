// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RejectsBadArguments(t *testing.T) {
	for _, rate := range []int{8000, 44100} {
		_, err := New(rate, 1, 1)
		assert.ErrorIs(t, err, ErrUnsupportedSampleRate, "rate %d", rate)
	}
	_, err := New(16000, 0, 1)
	assert.ErrorIs(t, err, ErrInvalidChannels)
	_, err = New(16000, 1, 0)
	assert.ErrorIs(t, err, ErrInvalidChannels)
}

func TestCanceller_RejectsBadFrames(t *testing.T) {
	canceller, err := New(32000, 2, 1)
	require.NoError(t, err)

	assert.NoError(t, canceller.AnalyzeRender(newSplitBandsFrame(2, 2)))
	assert.ErrorIs(t, canceller.AnalyzeRender(newSplitBandsFrame(1, 2)), ErrFrameShape)
	assert.ErrorIs(t, canceller.AnalyzeRender(newSplitBandsFrame(2, 3)), ErrFrameShape)
	assert.ErrorIs(t, canceller.AnalyzeRender([][][]float32{{make([]float32, 80), nil}, {nil, nil}}), ErrFrameShape)

	assert.NoError(t, canceller.AnalyzeCapture([][]float32{make([]float32, 320)}))
	assert.ErrorIs(t, canceller.AnalyzeCapture([][]float32{make([]float32, 160)}), ErrFrameShape)
	assert.ErrorIs(t, canceller.AnalyzeCapture(make([][]float32, 2)), ErrFrameShape)

	assert.NoError(t, canceller.ProcessCapture(newSplitBandsFrame(1, 2), false))
	assert.ErrorIs(t, canceller.ProcessCapture(newSplitBandsFrame(2, 2), false), ErrFrameShape)
	assert.ErrorIs(t, canceller.ProcessCapture(newSplitBandsFrame(1, 1), false), ErrFrameShape)
}

// echoScenario feeds a Canceller with noise in every band of the render
// channels, and with a capture signal that is the mean of the render
// channels, delayed and scaled by 0.5.
type echoScenario struct {
	canceller       *Canceller
	rng             *rand.Rand
	render, capture [][][]float32
	fullBand        [][]float32
	delayLines      [][]float32
	position        int
}

func newEchoScenario(t *testing.T, sampleRateHz, numRender, numCapture, delaySamples int) *echoScenario {
	t.Helper()
	canceller, err := New(sampleRateHz, numRender, numCapture)
	require.NoError(t, err)
	numBands := numBandsForRate(sampleRateHz)
	scenario := &echoScenario{
		canceller:  canceller,
		rng:        rand.New(rand.NewPCG(1, uint64(sampleRateHz))), //nolint:gosec // Deterministic test signal.
		render:     newSplitBandsFrame(numRender, numBands),
		capture:    newSplitBandsFrame(numCapture, numBands),
		fullBand:   make([][]float32, numCapture),
		delayLines: make([][]float32, numBands),
	}
	for band := range scenario.delayLines {
		scenario.delayLines[band] = make([]float32, delaySamples)
	}
	for ch := range scenario.fullBand {
		scenario.fullBand[ch] = make([]float32, numBands*splitBandSize)
	}

	return scenario
}

// step processes one frame and returns the energies of the echo and of
// the output.
func (e *echoScenario) step(t *testing.T) (echoEnergy, outputEnergy float64) {
	t.Helper()
	for band, delayLine := range e.delayLines {
		for k := range splitBandSize {
			var mean float32
			for ch := range e.render {
				sample := float32(e.rng.NormFloat64() * 3000)
				e.render[ch][band][k] = sample
				mean += sample / float32(len(e.render))
			}
			position := (e.position + k) % len(delayLine)
			echo := delayLine[position]
			delayLine[position] = 0.5 * mean
			for ch := range e.capture {
				e.capture[ch][band][k] = echo
			}
			echoEnergy += float64(echo) * float64(echo)
		}
	}
	e.position += splitBandSize

	require.NoError(t, e.canceller.AnalyzeRender(e.render))
	require.NoError(t, e.canceller.AnalyzeCapture(e.fullBand))
	require.NoError(t, e.canceller.ProcessCapture(e.capture, false))
	for _, sample := range e.capture[0] {
		for _, value := range sample {
			outputEnergy += float64(value) * float64(value)
		}
	}

	return echoEnergy, outputEnergy
}

func TestCanceller_RemovesEcho(t *testing.T) {
	for _, tc := range []struct{ sampleRateHz, numRender, numCapture int }{
		{16000, 1, 1}, {32000, 1, 1}, {48000, 1, 1}, {16000, 2, 2}, {48000, 2, 2}, {48000, 2, 1},
	} {
		t.Run(fmt.Sprintf("%d_%d_%d", tc.sampleRateHz, tc.numRender, tc.numCapture), func(t *testing.T) {
			const delaySamples = 400
			scenario := newEchoScenario(t, tc.sampleRateHz, tc.numRender, tc.numCapture, delaySamples)
			var echoEnergy, outputEnergy float64
			for index := range 500 {
				echo, output := scenario.step(t)
				if index >= 400 {
					echoEnergy += echo
					outputEnergy += output
				}
			}
			assert.Greater(t, 10*math.Log10(echoEnergy/outputEnergy), 20.0)
			if tc.numRender > 1 {
				return
			}

			metrics := scenario.canceller.Metrics()
			assert.InDelta(t, 8, metrics.EchoReturnLoss, 3)
			assert.Greater(t, metrics.EchoReturnLossEnhancement, 3.0)
			assert.InDelta(t, delaySamples/16, metrics.DelayMs, 2*kBlockSizeMs)
		})
	}
}

func TestCanceller_ZeroAllocSteadyState(t *testing.T) {
	scenario := newEchoScenario(t, 48000, 2, 2, 100)
	// The canceller is set up again when it detects stereo content, after
	// 2 seconds.
	for range 300 {
		scenario.step(t)
	}
	assert.Zero(t, testing.AllocsPerRun(100, func() { scenario.step(t) }))
}
