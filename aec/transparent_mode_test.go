// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transparentModePhase is a number of steps of a reference script, with
// the probabilities of the inputs. The detector is reset after the last
// step when reset is set.
type transparentModePhase struct {
	length          int
	consistent      float32
	delay           int
	converged       float32
	coarseConverged float32
	diverged        float32
	active          float32
	saturated       float32
	reset           bool
}

func transparentModeConverged(length int) transparentModePhase {
	return transparentModePhase{length: length, delay: 2, converged: 1, coarseConverged: 1, active: 1}
}

func transparentModeNotConverged(length int) transparentModePhase {
	return transparentModePhase{length: length, delay: 2, active: 1}
}

func transparentModeDiverged(length int) transparentModePhase {
	return transparentModePhase{length: length, delay: 2, diverged: 1, active: 1}
}

func transparentModeConsistent(length int) transparentModePhase {
	return transparentModePhase{length: length, consistent: 1, delay: 2, active: 1}
}

// transparentModeScripts returns the scripts of the upstream reference.
func transparentModeScripts() [][]transparentModePhase {
	// Active render without convergence, long enough for the filter to be
	// expected to have converged.
	noConvergence := transparentModeNotConverged(1600)
	reset := transparentModePhase{delay: 2, reset: true}

	return [][]transparentModePhase{
		// 0: random inputs.
		{
			{4000, 0, 2, 0, 0, 0, 1, 0, false},            // Active render, nothing converges.
			{4000, 0, 2, 0.9, 0.9, 0, 1, 0, true},         // The filters converge.
			{4000, 0, 2, 0, 0, 0, 0.3, 0, false},          // Little render, no convergence.
			{16000, 0, 2, 0, 0, 0, 1, 0, false},           // Active render, no convergence.
			{4000, 0.5, 2, 0.002, 0.002, 0, 1, 0, false},  // A consistent filter, rare convergence.
			{4000, 0.5, 7, 0, 0, 0, 1, 0, false},          // A consistent filter with a long delay.
			{4000, 0, 2, 0.01, 0.01, 0.99, 1, 0.5, false}, // Diverged filters, saturation.
			{4000, 0, 2, 0, 0, 0, 1, 1, false},            // Saturated capture.
			{4000, 0, 2, 0, 0, 0, 1, 0, true},
			{12000, 0, 2, 0, 0, 0, 1, 0, false},
			{4000, 0.1, 2, 0.03, 0.005, 0, 0.8, 0, false}, // Occasional convergence.
			{4000, 0, 2, 0, 0.004, 0, 1, 0, false},
			{4000, 0, 2, 0.5, 0.5, 0, 0, 0, false}, // Convergence without render.
			{8000, 0, 2, 0, 0, 0, 1, 0, false},
		},
		// 1: two bursts of convergence, close enough to add up.
		{
			noConvergence, transparentModeConverged(30), transparentModeNotConverged(3000),
			transparentModeConverged(30), transparentModeNotConverged(100),
		},
		// 2: two bursts of convergence, too far apart to add up.
		{
			noConvergence, transparentModeConverged(30), transparentModeNotConverged(5500),
			transparentModeConverged(30), transparentModeNotConverged(100),
		},
		// 3: the filters diverge between two bursts of convergence.
		{
			noConvergence, transparentModeConverged(40), transparentModeDiverged(70),
			transparentModeConverged(40), transparentModeNotConverged(100),
		},
		// 4: the filters do not diverge for long enough.
		{
			noConvergence, transparentModeConverged(40), transparentModeNotConverged(1), transparentModeDiverged(59),
			transparentModeNotConverged(1), transparentModeConverged(40), transparentModeNotConverged(100),
		},
		// 5: saturated capture.
		{{length: 2000, delay: 2, active: 1, saturated: 1}, noConvergence},
		// 6: a reset after a consistent filter and convergence.
		{transparentModeConsistent(2000), transparentModeConverged(20), reset, transparentModeConsistent(3000)},
		// 7: a reset between two bursts of convergence.
		{
			noConvergence, transparentModeConverged(40), reset, transparentModeNotConverged(1),
			transparentModeConverged(40), noConvergence,
		},
		// 8: a consistent filter and convergence, no render, then render
		// without a consistent filter.
		{
			transparentModeConsistent(2000), transparentModeConverged(20),
			{length: 8000, delay: 2},
			transparentModeNotConverged(7600),
		},
		// 9: convergence, then a long time without.
		{noConvergence, transparentModeConverged(60), transparentModeNotConverged(15100)},
		// 10: the filters diverge for just long enough.
		{
			noConvergence, transparentModeConverged(40), transparentModeDiverged(60), transparentModeNotConverged(1),
			transparentModeConverged(40), transparentModeNotConverged(100),
		},
	}
}

// runTransparentModeScript runs a script and compares the steps where
// the transparent mode is toggled with the reference.
func runTransparentModeScript(
	t *testing.T, mode transparentMode, script []transparentModePhase, reference *referenceCursor,
) {
	t.Helper()
	random := newTestRandom(42)
	draw := func(probability float32) bool { return random.randFloat() < probability }
	previous := false
	index := 0
	require.Equal(t, []float32{0, 0}, reference.next("active"))

	for _, phase := range script {
		for range phase.length {
			consistent := draw(phase.consistent)
			converged := draw(phase.converged)
			coarseConverged := draw(phase.coarseConverged)
			diverged := draw(phase.diverged)
			active := draw(phase.active)
			saturated := draw(phase.saturated)
			mode.update(phase.delay, consistent, converged, coarseConverged, diverged, active, saturated)
			if mode.active() != previous {
				previous = mode.active()
				require.Equal(t, []float32{float32(index), boolValue(previous)}, reference.next("active"))
			}
			index++
		}
		if phase.reset {
			mode.reset()
		}
	}
	require.Equal(t, []float32{float32(index), boolValue(mode.active())}, reference.next("end"))
	require.Empty(t, reference.entries)
}

// TestTransparentMode_MatchesUpstream compares the steps where the
// transparent mode is toggled with the upstream TransparentMode at the
// pinned commit, for the legacy classifier, with and without a linear and
// stable echo path, and for the HMM classifier. Each line of the
// reference file is keyed by "name classifier script".
func TestTransparentMode_MatchesUpstream(t *testing.T) {
	stableConfig := defaultEchoCanceller3Config()
	stableConfig.echoRemovalControl.linearAndStableEchoPath = true
	for name, create := range map[string]func() transparentMode{
		"legacy": func() transparentMode { return newTransparentMode(defaultEchoCanceller3Config()) },
		"stable": func() transparentMode { return newTransparentMode(stableConfig) },
		"hmm":    func() transparentMode { return newTransparentModeHmm() },
	} {
		for index, script := range transparentModeScripts() {
			suffix := fmt.Sprintf(" %s %d", name, index)
			t.Run(suffix[1:], func(t *testing.T) {
				reference := newReferenceCursor(t, "testdata/transparent_mode_upstream.txt", suffix)
				runTransparentModeScript(t, create(), script, reference)
			})
		}
	}
}

// The transparent mode is disabled when the ERL is bounded.
func TestTransparentMode_BoundedErl(t *testing.T) {
	config := defaultEchoCanceller3Config()
	config.epStrength.boundedErl = true
	assert.Nil(t, newTransparentMode(config))
}
