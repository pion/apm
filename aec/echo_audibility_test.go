// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// echoAudibilityInserts returns the number of render blocks of a step of
// the reference scenario: two every 50th step, none every 60th step.
func echoAudibilityInserts(index int) int {
	switch {
	case index%50 == 7:
		return 2
	case index%60 == 9:
		return 0
	default:
		return 1
	}
}

// echoAudibilityAmplitude returns the amplitude of the render signal of
// a step of the reference scenario.
func echoAudibilityAmplitude(index int) float32 {
	switch {
	case index < 30:
		return 0
	case index < 50:
		return 2e-4
	default:
		return 1e-2 * stationarityScenarioLevel(index, 0, false)
	}
}

// echoAudibilityInsertRender inserts the render blocks of a step of the
// reference scenario.
func echoAudibilityInsertRender(
	index int, random *testRandom, render *block, renderDelayBuffer *renderDelayBufferImpl,
) {
	for range echoAudibilityInserts(index) {
		for ch := range render.numChannels {
			channel := render.view(0, ch)
			randomizeSampleVector(random, channel)
			for k := range channel {
				channel[k] *= echoAudibilityAmplitude(index)
			}
		}
		renderDelayBuffer.insert(render)
	}
	if index == 0 {
		renderDelayBuffer.reset()
	}
	renderDelayBuffer.prepareCaptureProcessing()
}

// TestEchoAudibility_MatchesUpstream compares the stationarity of the
// block and the residual echo scaling of every band with the upstream
// EchoAudibility at the pinned commit, with and without the use of the
// render stationarity at init, and for an instance that sees an external
// delay from step 20 on.
//
// The render signal is zero in steps 0 to 29, noise below the detection
// level in steps 30 to 49, then noise with the level of the stationarity
// scenario. Two render blocks are inserted every 50th step, none every
// 60th step. An external delay is seen from step 200 on, and the delay is
// 3 blocks, then 20 blocks from step 1500 on. The filter has had time to
// converge from step 400 on. Each line of the reference file is keyed by
// "name render_channels".
func TestEchoAudibility_MatchesUpstream(t *testing.T) {
	for _, numChannels := range []int{1, 2} {
		t.Run(fmt.Sprint(numChannels), func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/echo_audibility_upstream.txt", fmt.Sprintf(" %d", numChannels))
			referenceAtInit := reference.filter("audibility_at_init")
			referenceEarlyDelay := reference.filter("audibility_early_delay")
			random := newTestRandom(42)
			renderDelayBuffer := newRenderDelayBufferImpl(defaultEchoCanceller3Config(), 48000, numChannels)
			audibilities := []*echoAudibility{newEchoAudibility(false), newEchoAudibility(true), newEchoAudibility(false)}
			changes := make([]flagChanges, len(audibilities))
			render := newBlock(3, numChannels)
			var reverb, scaling [kFftLengthBy2Plus1]float32

			for index := range stationarityScenarioSteps {
				echoAudibilityInsertRender(index, random, render, renderDelayBuffer)
				for k := range reverb {
					reverb[k] = 100 * random.randFloat() //nolint:gosec // G602 false positive on a range loop.
				}
				delayBlocks := 3
				if index >= 1500 {
					delayBlocks = 20
				}

				for i, audibility := range audibilities {
					// The third instance sees an external delay before any
					// render signal.
					externalDelaySeen := index >= 200 || (i == 2 && index >= 20)
					audibility.update(renderDelayBuffer.getRenderBuffer(), &reverb, delayBlocks, externalDelaySeen)
					audibility.getResidualEchoScaling(index >= 400, scaling[:])
					flags := append([]float32{boolValue(audibility.isBlockStationary())}, scaling[:]...)
					changes[i].add(index, flags)
				}
			}
			changes[0].assertMatches(t, reference, "audibility")
			changes[1].assertMatches(t, referenceAtInit, "audibility_at_init")
			require.Empty(t, reference.entries)
			require.Empty(t, referenceAtInit.entries)
			changes[2].assertMatches(t, referenceEarlyDelay, "audibility_early_delay")
			require.Empty(t, referenceEarlyDelay.entries)
		})
	}
}
