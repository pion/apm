// SPDX-FileCopyrightText: 2020 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const (
	blocksSinceConvergedFilterInit     = 10000
	blocksSinceConsistentEstimateInit  = 10000
	initialTransparentStateProbability = 0.2
)

// transparentMode mirrors TransparentMode: it detects and toggles the
// transparent mode, which causes the suppressor to apply less
// suppression.
type transparentMode interface {
	// active mirrors Active: whether the transparent mode should be
	// active.
	active() bool
	// reset mirrors Reset: it resets the state of the detector.
	reset()
	// update mirrors Update: it updates the detection decision based on
	// new data.
	update(
		filterDelayBlocks int,
		anyFilterConsistent, anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged bool,
		activeRender, saturatedCapture bool,
	)
}

// newTransparentMode mirrors TransparentMode::Create. It returns nil when
// the transparent mode is disabled.
//
// Upstream creates the classifier of newTransparentModeHmm instead of the
// legacy one when the field trial WebRTC-Aec3TransparentModeHmm is
// enabled. Field trials are not ported.
func newTransparentMode(config echoCanceller3Config) transparentMode {
	if config.epStrength.boundedErl {
		return nil
	}

	return newLegacyTransparentMode(config)
}

// transparentModeHmm mirrors TransparentModeImpl: a classifier that
// toggles the transparent mode, which reduces the echo suppression when
// headsets are used.
type transparentModeHmm struct {
	transparencyActivated bool
	probTransparentState  float32
}

func newTransparentModeHmm() *transparentModeHmm {
	return &transparentModeHmm{probTransparentState: initialTransparentStateProbability}
}

func (t *transparentModeHmm) active() bool { return t.transparencyActivated }

func (t *transparentModeHmm) reset() {
	t.transparencyActivated = false
	t.probTransparentState = initialTransparentStateProbability
}

// update implements the classifier as a Hidden Markov Model with two
// hidden states: "normal" and "transparent". The estimated probabilities
// of the two states are updated by observing the filter convergence
// during active render. The filters are less likely to be reported as
// converged when there is no echo in the microphone signal.
//
// The constants were obtained upstream by observing the inputs under
// varying call scenarios, then hand tuned to prefer the normal state
// during uncertain regions, to avoid echo leaks.
func (t *transparentModeHmm) update(
	_ int, _, _, anyCoarseFilterConverged, _ bool, activeRender, _ bool,
) {
	// The model is only updated during active render.
	if !activeRender {
		return
	}

	const (
		// Probability of switching from one state to the other.
		probSwitch = 0.000001
		// Probability of observing converged filters in the states
		// "normal" and "transparent" during active render.
		convergedNormal      = 0.01
		convergedTransparent = 0.001
	)
	// Probability of transitioning to the transparent state from the
	// normal state and the transparent state respectively.
	probA := [2]float32{probSwitch, 1 - float32(probSwitch)}
	// Probability of the two observations, a filter that is not converged
	// or one that is, in the normal state and the transparent state
	// respectively.
	probB := [2][2]float32{
		{1 - float32(convergedNormal), convergedNormal},
		{1 - float32(convergedTransparent), convergedTransparent},
	}

	// Probability of the two states before the update.
	probTransparent := t.probTransparentState
	probNormal := 1 - probTransparent

	// Probability of transitioning to the transparent state.
	probTransitionTransparent := probNormal*probA[0] + probTransparent*probA[1]
	probTransitionNormal := 1 - probTransitionTransparent

	// Observed output.
	out := 0
	if anyCoarseFilterConverged {
		out = 1
	}

	// Joint probabilities of the observed output and the respective
	// states.
	probJointNormal := probTransitionNormal * probB[0][out]
	probJointTransparent := probTransitionTransparent * probB[1][out]

	// Conditional probability of the transparent state and the observed
	// output.
	t.probTransparentState = probJointTransparent / (probJointNormal + probJointTransparent)

	// The transparent mode is only activated when its state probability is
	// high. A dead zone between the thresholds avoids switching back and
	// forth.
	if t.probTransparentState > 0.95 {
		t.transparencyActivated = true
	} else if t.probTransparentState < 0.5 {
		t.transparencyActivated = false
	}
}

// legacyTransparentMode mirrors LegacyTransparentModeImpl: the legacy
// classifier for toggling the transparent mode.
type legacyTransparentMode struct {
	linearAndStableEchoPath         bool
	captureBlockCounter             int
	transparencyActivated           bool
	activeBlocksSinceSaneFilter     int
	saneFilterObserved              bool
	finiteErlRecentlyDetected       bool
	nonConvergedSequenceSize        int
	divergedSequenceSize            int
	activeNonConvergedSequenceSize  int
	numConvergedBlocks              int
	recentConvergenceDuringActivity bool
	strongNotSaturatedRenderBlocks  int
}

func newLegacyTransparentMode(config echoCanceller3Config) *legacyTransparentMode {
	return &legacyTransparentMode{
		linearAndStableEchoPath:     config.echoRemovalControl.linearAndStableEchoPath,
		activeBlocksSinceSaneFilter: blocksSinceConsistentEstimateInit,
		nonConvergedSequenceSize:    blocksSinceConvergedFilterInit,
	}
}

func (l *legacyTransparentMode) active() bool { return l.transparencyActivated }

func (l *legacyTransparentMode) reset() {
	l.nonConvergedSequenceSize = blocksSinceConvergedFilterInit
	l.divergedSequenceSize = 0
	l.strongNotSaturatedRenderBlocks = 0
	if l.linearAndStableEchoPath {
		l.recentConvergenceDuringActivity = false
	}
}

func (l *legacyTransparentMode) update(
	filterDelayBlocks int,
	anyFilterConsistent, anyFilterConverged, _, allFiltersDiverged bool,
	activeRender, saturatedCapture bool,
) {
	l.captureBlockCounter++
	if activeRender && !saturatedCapture {
		l.strongNotSaturatedRenderBlocks++
	}

	saneFilterRecentlySeen := l.updateSaneFilter(filterDelayBlocks, anyFilterConsistent, activeRender)
	l.updateConvergence(anyFilterConverged, activeRender)
	l.updateDivergence(allFiltersDiverged)

	if l.activeNonConvergedSequenceSize > 60*kNumBlocksPerSecond {
		l.finiteErlRecentlyDetected = false
	}
	if l.numConvergedBlocks > 50 {
		l.finiteErlRecentlyDetected = true
	}

	switch {
	case l.finiteErlRecentlyDetected:
		l.transparencyActivated = false
	case saneFilterRecentlySeen && l.recentConvergenceDuringActivity:
		l.transparencyActivated = false
	default:
		filterShouldHaveConverged := l.strongNotSaturatedRenderBlocks > 6*kNumBlocksPerSecond
		l.transparencyActivated = filterShouldHaveConverged
	}
}

// updateSaneFilter tells whether a sane filter was recently seen.
func (l *legacyTransparentMode) updateSaneFilter(
	filterDelayBlocks int, anyFilterConsistent, activeRender bool,
) bool {
	if anyFilterConsistent && filterDelayBlocks < 5 {
		l.saneFilterObserved = true
		l.activeBlocksSinceSaneFilter = 0
	} else if activeRender {
		l.activeBlocksSinceSaneFilter++
	}

	if !l.saneFilterObserved {
		return l.captureBlockCounter <= 5*kNumBlocksPerSecond
	}

	return l.activeBlocksSinceSaneFilter <= 30*kNumBlocksPerSecond
}

func (l *legacyTransparentMode) updateDivergence(allFiltersDiverged bool) {
	if !allFiltersDiverged {
		l.divergedSequenceSize = 0

		return
	}
	l.divergedSequenceSize++
	if l.divergedSequenceSize >= 60 {
		l.nonConvergedSequenceSize = blocksSinceConvergedFilterInit
	}
}

func (l *legacyTransparentMode) updateConvergence(anyFilterConverged, activeRender bool) {
	if anyFilterConverged {
		l.recentConvergenceDuringActivity = true
		l.activeNonConvergedSequenceSize = 0
		l.nonConvergedSequenceSize = 0
		l.numConvergedBlocks++

		return
	}

	l.nonConvergedSequenceSize++
	if l.nonConvergedSequenceSize > 20*kNumBlocksPerSecond {
		l.numConvergedBlocks = 0
	}

	if activeRender {
		l.activeNonConvergedSequenceSize++
		if l.activeNonConvergedSequenceSize > 60*kNumBlocksPerSecond {
			l.recentConvergenceDuringActivity = false
		}
	}
}
