// SPDX-FileCopyrightText: 2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stereoTestFrame returns a frame of one band and two channels, where
// the first channel holds 100 and the second one the given value.
func stereoTestFrame(right float32) [][][]float32 {
	frame := [][][]float32{{make([]float32, 160), make([]float32, 160)}}
	fill(frame[0][0], 100)
	fill(frame[0][1], right)

	return frame
}

// Ports of MultiChannelContentDetector.HandlingOfMono,
// HandlingOfMonoAndDetectionOff and InitialDetectionOfStereo.
func TestMultiChannelContentDetector_InitialState(t *testing.T) {
	assert.False(t, newMultiChannelContentDetector(true, 1, 0, 0, 0).isProperMultiChannelContentDetected())
	assert.False(t, newMultiChannelContentDetector(false, 1, 0, 0, 0).isProperMultiChannelContentDetected())
	assert.False(t, newMultiChannelContentDetector(true, 2, 0, 0, 0).isProperMultiChannelContentDetected())
}

// Port of MultiChannelContentDetector.HandlingOfDetectionOff.
func TestMultiChannelContentDetector_HandlingOfDetectionOff(t *testing.T) {
	detector := newMultiChannelContentDetector(false, 2, 0, 0, 0)
	assert.True(t, detector.isProperMultiChannelContentDetected())

	frame := stereoTestFrame(101)
	assert.False(t, detector.updateDetection(frame))
	assert.True(t, detector.isProperMultiChannelContentDetected())
	assert.False(t, detector.updateDetection(frame))
}

// Ports of MultiChannelContentDetector.DetectionWhenFakeStereo,
// DetectionWhenStereo, DetectionWithStereoBelowThreshold and
// DetectionWithStereoAboveThreshold.
func TestMultiChannelContentDetector_Detection(t *testing.T) {
	const threshold = 1.0
	for _, tc := range []struct {
		name               string
		detectionThreshold float32
		right              float32
		stereo             bool
	}{
		{"FakeStereo", 0, 100, false},
		{"Stereo", 0, 101, true},
		{"StereoBelowThreshold", threshold, 100 + threshold, false},
		{"StereoAboveThreshold", threshold, 100 + threshold + 0.1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detector := newMultiChannelContentDetector(true, 2, tc.detectionThreshold, 0, 0)
			frame := stereoTestFrame(tc.right)

			assert.Equal(t, tc.stereo, detector.updateDetection(frame))
			assert.Equal(t, tc.stereo, detector.isProperMultiChannelContentDetected())
			assert.False(t, detector.updateDetection(frame))
		})
	}
}

// Port of MultiChannelContentDetector.DetectionWhenStereoAfterAWhile.
func TestMultiChannelContentDetector_DetectionWhenStereoAfterAWhile(t *testing.T) {
	detector := newMultiChannelContentDetector(true, 2, 0, 0, 0)

	frame := stereoTestFrame(100)
	assert.False(t, detector.updateDetection(frame))
	assert.False(t, detector.isProperMultiChannelContentDetected())
	assert.False(t, detector.updateDetection(frame))

	frame = stereoTestFrame(101)
	assert.True(t, detector.updateDetection(frame))
	assert.True(t, detector.isProperMultiChannelContentDetected())
	assert.False(t, detector.updateDetection(frame))
}

// Port of
// MultiChannelContentDetectorTimeoutBehavior.TimeOutBehaviorForNonTrueStereo.
func TestMultiChannelContentDetector_TimeOutBehaviorForNonTrueStereo(t *testing.T) {
	trueStereoFrame := stereoTestFrame(101)
	fakeStereoFrame := stereoTestFrame(100)
	for _, detectStereoContent := range []bool{false, true} {
		for _, timeoutThresholdSeconds := range []int{0, 1, 10} {
			timeoutThresholdFrames := timeoutThresholdSeconds * kNumFramesPerSecond
			timesOut := detectStereoContent && timeoutThresholdFrames > 0
			detector := newMultiChannelContentDetector(detectStereoContent, 2, 0, timeoutThresholdSeconds, 0)

			// Pass fake stereo frames and verify the content detection.
			for range 10 {
				assert.False(t, detector.updateDetection(fakeStereoFrame))
				assert.Equal(t, !detectStereoContent, detector.isProperMultiChannelContentDetected())
			}

			// Pass a true stereo frame and verify that it is properly
			// detected.
			assert.Equal(t, detectStereoContent, detector.updateDetection(trueStereoFrame))
			assert.True(t, detector.isProperMultiChannelContentDetected())

			// Pass fake stereo frames until any timeouts are about to occur.
			for range timeoutThresholdFrames - 1 {
				assert.False(t, detector.updateDetection(fakeStereoFrame))
				assert.True(t, detector.isProperMultiChannelContentDetected())
			}

			// Pass a fake stereo frame and verify that any timeouts properly
			// occur.
			assert.Equal(t, timesOut, detector.updateDetection(fakeStereoFrame))
			assert.Equal(t, !timesOut, detector.isProperMultiChannelContentDetected())

			// Pass fake stereo frames and verify the behavior after any
			// timeout.
			for range 10 {
				assert.False(t, detector.updateDetection(fakeStereoFrame))
				assert.Equal(t, !timesOut, detector.isProperMultiChannelContentDetected())
			}
		}
	}
}

// Port of
// MultiChannelContentDetectorHysteresisBehavior.PeriodBeforeStereoDetectionIsTriggered.
// Upstream truncates the hysteresis of its parameters, 0, 0.1 and 0.2
// seconds, to an integer number of seconds, so that it only tests a
// hysteresis of 0.
func TestMultiChannelContentDetector_PeriodBeforeStereoDetectionIsTriggered(t *testing.T) {
	trueStereoFrame := stereoTestFrame(101)
	fakeStereoFrame := stereoTestFrame(100)
	for _, detectStereoContent := range []bool{false, true} {
		detector := newMultiChannelContentDetector(detectStereoContent, 2, 0, 0, 0)

		// Pass fake stereo frames and verify the content detection.
		for range 10 {
			assert.False(t, detector.updateDetection(fakeStereoFrame))
			assert.Equal(t, !detectStereoContent, detector.isProperMultiChannelContentDetected())
			assert.False(t, detector.isTemporaryMultiChannelContentDetected())
		}

		// Pass two true stereo frames and verify that they are properly
		// detected.
		for k := range 2 {
			assert.Equal(t, detectStereoContent && k == 0, detector.updateDetection(trueStereoFrame))
			assert.True(t, detector.isProperMultiChannelContentDetected())
			assert.False(t, detector.isTemporaryMultiChannelContentDetected())
		}
	}
}

// drawMultiChannelScenarioFrame draws a frame of the reference scenario
// of the multi-channel content detector.
func drawMultiChannelScenarioFrame(index int, random *testRandom, frame [][][]float32) {
	difference, bandWithDifference := float32(0), 1
	switch {
	case inRange(index, 50, 55), inRange(index, 100, 111), inRange(index, 150, 400):
		difference = 1
	case inRange(index, 600, 700):
		difference = 0.4
	case index >= 900 && index%150 == 0:
		difference, bandWithDifference = 1, 0
	}
	for band := range frame {
		for k := range 160 {
			sample := 1000 * random.randFloat()
			frame[band][0][k] = sample
			frame[band][1][k] = sample
			if band == bandWithDifference && k == 77 {
				frame[band][1][k] += difference
			}
			frame[band][2][k] = sample + 100
		}
	}
}

// TestMultiChannelContentDetector_MatchesUpstream compares the frames
// where the state changes with the upstream MultiChannelContentDetector
// at the pinned commit, with a detection threshold of 0.5, a timeout of 2
// seconds and a hysteresis of 0.1 seconds.
//
// The frames have two bands and three channels. The third channel differs
// from the first one in every frame. The second channel differs from the
// first one by 1 in the second band of the frames 50 to 54, 100 to 110
// and 150 to 399, by 0.4 in the frames 600 to 699 and by 1 in the first
// band of every 150th frame from frame 900 on.
func TestMultiChannelContentDetector_MatchesUpstream(t *testing.T) {
	reference := newReferenceCursor(t, "testdata/multi_channel_content_detector_upstream.txt", "")
	detector := newMultiChannelContentDetector(true, 3, 0.5, 2, 0.1)
	random := newTestRandom(42)
	frame := make([][][]float32, 2)
	for band := range frame {
		frame[band] = [][]float32{make([]float32, 160), make([]float32, 160), make([]float32, 160)}
	}
	previousProper, previousTemporary := false, false
	require.Equal(t, []float32{0, 0, 0, 0}, reference.next("state"))

	for index := range 1500 {
		drawMultiChannelScenarioFrame(index, random, frame)

		changed := detector.updateDetection(frame)
		proper := detector.isProperMultiChannelContentDetected()
		temporary := detector.isTemporaryMultiChannelContentDetected()
		if changed || proper != previousProper || temporary != previousTemporary {
			got := []float32{float32(index), boolValue(changed), boolValue(proper), boolValue(temporary)}
			require.Equal(t, reference.next("state"), got, fmt.Sprintf("frame %d", index))
		}
		previousProper, previousTemporary = proper, temporary
	}
	require.Empty(t, reference.entries)
}
