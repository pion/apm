// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runBasicSetupAndAPICallTest ports RunBasicSetupAndApiCallTest, without
// the echo leakage status.
func runBasicSetupAndAPICallTest(t *testing.T, sampleRateHz, numIterations int) {
	t.Helper()
	processor := newBlockProcessor(defaultEchoCanceller3Config(), sampleRateHz, 1, 1)
	blk := newBlock(numBandsForRate(sampleRateHz), 1)
	fill(blk.data, 1000)
	assert.NotPanics(t, func() {
		for range numIterations {
			processor.bufferRender(blk)
			processor.processCapture(false, false, nil, blk)
		}
	})
}

// Port of BlockProcessor.BasicSetupAndApiCalls: the basic usage of the
// block processor.
func TestBlockProcessor_BasicSetupAndApiCalls(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		runBasicSetupAndAPICallTest(t, rate, 1)
	}
}

// Port of BlockProcessor.TestLongerCall.
func TestBlockProcessor_TestLongerCall(t *testing.T) {
	runBasicSetupAndAPICallTest(t, 16000, 20*kNumBlocksPerSecond)
}

// blockProcessorScenario holds the state of the upstream reference
// scenario of the block processor.
//
// The signals are those of the echo remover scenario. Every block, the
// render signal is buffered, then the capture signal is processed. The
// render signal is not buffered in blocks 0 to 4 and 1120 to 1149, and 152
// more blocks of noise are buffered at block 1250, the last of which
// overruns the render buffer. With external, an external delay estimator
// is used, and the delay of the audio buffer is set at block 50.
type blockProcessorScenario struct {
	signals      *echoRemoverScenario
	processor    *blockProcessor
	extra        *block
	external     bool
	delaySamples int
}

func newBlockProcessorScenario(
	sampleRateHz, numRender, numCapture, delaySamples int, external bool,
) *blockProcessorScenario {
	config := defaultEchoCanceller3Config()
	config.delay.useExternalDelayEstimator = external
	signals := newEchoRemoverScenario(sampleRateHz, numRender, numCapture, delaySamples, false)

	return &blockProcessorScenario{
		signals:      signals,
		processor:    newBlockProcessor(config, sampleRateHz, numRender, numCapture),
		extra:        newBlock(signals.numBands, numRender),
		external:     external,
		delaySamples: delaySamples,
	}
}

// step processes a block and stores the energies in the signals.
func (b *blockProcessorScenario) step(index int) {
	signals := b.signals
	last := signals.numCapture - 1
	signals.generate(index)
	signals.inputEnergy = energy(signals.capture.view(0, last))

	if index >= 5 && !inRange(index, 1120, 1150) {
		b.processor.bufferRender(signals.render)
	}
	if index == 1250 {
		for range 152 {
			randomizeSampleVectorAmplitude(signals.random, b.extra.data, 10000)
			b.processor.bufferRender(b.extra)
		}
	}
	if b.external && index == 50 {
		b.processor.setAudioBufferDelay(b.delaySamples * 1000 / 16000)
	}
	b.processor.setCaptureOutputUsage(!inRange(index, 1300, 1320))
	b.processor.processCapture(index == 600, inRange(index, 820, 830), signals.linearOutput, signals.capture)

	signals.outputEnergy = energy(signals.capture.view(0, last))
	signals.upperEnergy = 0
	for band := 1; band < signals.numBands; band++ {
		signals.upperEnergy += energy(signals.capture.view(band, last))
	}
	signals.linearEnergy = energy(signals.linearOutput.view(0, last))
}

// TestBlockProcessor_MatchesUpstream compares the energy of the output of
// every block, in dB, and the metrics of every 25th block with the
// upstream BlockProcessor at the pinned commit, like
// TestEchoRemover_MatchesUpstream does.
//
// Each line of the reference file is keyed by
// "name sample_rate render_channels capture_channels delay", plus
// "external" for the external delay estimator.
func TestBlockProcessor_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		sampleRateHz, numRender, numCapture, delaySamples int
		external                                          bool
	}{{48000, 1, 1, 640, false}, {16000, 2, 2, 1500, false}, {32000, 1, 1, 320, true}} {
		suffix := fmt.Sprintf(" %d %d %d %d", tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples)
		if tc.external {
			suffix += " external"
		}
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/block_processor_upstream.txt", suffix)
			metricsReference := reference.filter("metrics")
			scenario := newBlockProcessorScenario(tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples,
				tc.external)
			signals := scenario.signals
			var output, upper, linear, erl, erle levelDeviations

			for index := range echoRemoverScenarioBlocks {
				scenario.step(index)

				// The block index, then the energies of the input, the
				// output, its upper bands and the output of the linear
				// filter.
				want := reference.next("energies")
				require.Equal(t, float32(index), want[0])
				require.InEpsilon(t, want[1], signals.inputEnergy, 1e-5, "block %d", index)
				output.add(want[2], signals.outputEnergy)
				upper.add(want[3], signals.upperEnergy)
				linear.add(want[4], signals.linearEnergy)

				if index%25 == 24 {
					wantMetrics := metricsReference.next("metrics")
					gotErl, gotErle, delayMs := scenario.processor.getMetrics()
					erl = append(erl, math.Abs(gotErl-float64(wantMetrics[1])))
					erle = append(erle, math.Abs(gotErle-float64(wantMetrics[2])))
					if !fusesMultiplyAdd() {
						require.Equal(t, int(wantMetrics[3]), delayMs, "block %d", index)
					}
				}
			}
			output.assertBelow(t, "output", 0.01, 0.1, 2, math.Inf(1))
			if tc.sampleRateHz > 16000 {
				upper.assertBelow(t, "upper bands", 0.01, 0.1, 2, math.Inf(1))
			}
			linear.assertBelow(t, "linear output", 0.01, 0.1, 3, math.Inf(1))
			erl.assertBelow(t, "ERL", 0.001, 0.01, 0.01, 0.01)
			erle.assertBelow(t, "ERLE", 0.001, 0.1, 1, math.Inf(1))
			require.Empty(t, reference.entries)
			require.Empty(t, metricsReference.entries)
		})
	}
}
