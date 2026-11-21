// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// nearendDetectorScenarioConfig returns the configuration that replaces
// the initial one at step 2000 of the reference scenario.
func nearendDetectorScenarioConfig() suppressorConfig {
	config := defaultSuppressorConfig()
	config.dominantNearendDetection = dominantNearendDetectionConfig{
		enrThreshold:             0.5,
		enrExitThreshold:         3,
		snrThreshold:             10,
		holdDuration:             20,
		triggerThreshold:         5,
		useDuringInitialPhase:    false,
		useUnboundedEchoSpectrum: true,
	}
	config.subbandNearendDetection = subbandNearendDetectionConfig{
		nearendAverageBlocks: 5,
		subband1:             subbandRegion{low: 1, high: 4},
		subband2:             subbandRegion{low: 20, high: 40},
		nearendThreshold:     0.8,
		snrThreshold:         5,
	}

	return config
}

// nearendDetectorScenarioLevels returns the levels of the nearend, the
// echo and the noise of a channel, and the slope of the nearend spectrum.
func nearendDetectorScenarioLevels(index, ch int) (nearend, echo, noise, slope float32) {
	period := index / 30
	nearend, echo, noise, slope = 1e3, 1e2, 1e2, 1.03
	if period%3 == 0 {
		nearend = 1e6
	}
	if (period+ch)%4 == 0 {
		echo = 1e7
	}
	if period%7 == 0 {
		noise = 1e6
	}
	if (index/90)%2 == 0 {
		slope = 0.97
	}

	return nearend, echo, noise, slope
}

// runNearendDetectorScenario runs the reference scenario of the nearend
// detectors and compares the steps where the nearend state changes with
// the upstream detector at the pinned commit.
//
// The spectra are drawn at random. The level of the nearend, the echo and
// the noise changes every 30 steps: the nearend is strong in one period
// out of three, the echo in one out of four, shifted per channel, and the
// noise in one out of seven. The slope of the nearend spectrum changes
// every 90 steps. The initial state lasts 200 steps. The configuration is
// set again at step 1000. At step 2000 it is replaced by one with other
// thresholds, durations and subbands. With strongUnusedBands, the bands 0
// and 16, which the dominant nearend detector does not use, are much
// stronger.
func runNearendDetectorScenario(
	t *testing.T,
	detector nearendDetector,
	initialConfig suppressorConfig,
	numCaptureChannels int,
	strongUnusedBands bool,
	reference *referenceCursor,
) {
	t.Helper()
	random := newTestRandom(42)
	nearend := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	echo := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	noise := make([][kFftLengthBy2Plus1]float32, numCaptureChannels)
	previous := false
	require.Equal(t, []float32{0, 0}, reference.next("state"))

	for index := range 4000 {
		switch index {
		case 1000:
			detector.setConfig(initialConfig)
		case 2000:
			detector.setConfig(nearendDetectorScenarioConfig())
		}
		for ch := range numCaptureChannels {
			nearendLevel, echoLevel, noiseLevel, slope := nearendDetectorScenarioLevels(index, ch)
			tilt := float32(1)
			for k := range kFftLengthBy2Plus1 {
				drawNearend := random.randFloat()
				drawEcho := random.randFloat()
				drawNoise := random.randFloat()
				nearend[ch][k] = nearendLevel * tilt * (0.5 + drawNearend)
				echo[ch][k] = echoLevel * (0.5 + drawEcho)
				noise[ch][k] = noiseLevel * (0.5 + drawNoise)
				tilt *= slope
			}
			if strongUnusedBands {
				for _, spectrum := range []*[kFftLengthBy2Plus1]float32{&nearend[ch], &echo[ch], &noise[ch]} {
					spectrum[0] *= 1e6
					spectrum[16] *= 1e6
				}
			}
		}
		detector.update(nearend, echo, noise, index < 200 || inRange(index, 2100, 2400))
		if detector.isNearendState() != previous {
			previous = detector.isNearendState()
			require.Equal(t, []float32{float32(index), boolValue(previous)}, reference.next("state"))
		}
	}
	require.Empty(t, reference.entries)
}
