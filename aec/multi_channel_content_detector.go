// SPDX-FileCopyrightText: 2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

const kNumFramesPerSecond = 100

// hasStereoContent matches HasStereoContent: it compares the left and the
// right channels of the render frame, which is indexed by band, channel
// and sample, to determine whether the signal is a proper stereo signal.
// The threshold allows for differences introduced by hardware drivers.
func hasStereoContent(frame [][][]float32, detectionThreshold float32) bool {
	if len(frame[0]) < 2 {
		return false
	}

	for band := range frame {
		for k, left := range frame[band][0] {
			if abs32(left-frame[band][1][k]) > detectionThreshold {
				return true
			}
		}
	}

	return false
}

// multiChannelContentDetector mirrors MultiChannelContentDetector: it
// analyzes audio content to determine whether it is proper multichannel,
// or only upmixed mono. The metrics that upstream logs are not ported.
type multiChannelContentDetector struct {
	detectStereoContent bool
	detectionThreshold  float32
	// detectionTimeoutThresholdFrames is 0 when there is no timeout: once
	// multichannel is detected, the detector remains in that state.
	detectionTimeoutThresholdFrames       int
	stereoDetectionHysteresisFrames       int
	persistentMultichannelContentDetected bool
	temporaryMultichannelContentDetected  bool
	framesSinceStereoDetectedLast         int
	consecutiveFramesWithStereo           int
}

func newMultiChannelContentDetector(
	detectStereoContent bool,
	numRenderInputChannels int,
	detectionThreshold float32,
	stereoDetectionTimeoutThresholdSeconds int,
	stereoDetectionHysteresisSeconds float32,
) *multiChannelContentDetector {
	return &multiChannelContentDetector{
		detectStereoContent:                   detectStereoContent,
		detectionThreshold:                    detectionThreshold,
		detectionTimeoutThresholdFrames:       max(0, stereoDetectionTimeoutThresholdSeconds) * kNumFramesPerSecond,
		stereoDetectionHysteresisFrames:       int(stereoDetectionHysteresisSeconds * kNumFramesPerSecond),
		persistentMultichannelContentDetected: !detectStereoContent && numRenderInputChannels > 1,
	}
}

// updateDetection mirrors UpdateDetection: it compares the left and the
// right channels of the render frame to determine whether the signal is a
// proper multichannel signal. It returns whether a change in the proper
// multichannel content was detected.
func (m *multiChannelContentDetector) updateDetection(frame [][][]float32) bool {
	if !m.detectStereoContent {
		return false
	}

	previousPersistentMultichannelContentDetected := m.persistentMultichannelContentDetected
	stereoDetectedInFrame := hasStereoContent(frame, m.detectionThreshold)

	if stereoDetectedInFrame {
		m.consecutiveFramesWithStereo++
		m.framesSinceStereoDetectedLast = 0
	} else {
		m.consecutiveFramesWithStereo = 0
		m.framesSinceStereoDetectedLast++
	}

	// Detect persistent multichannel content.
	if m.consecutiveFramesWithStereo > m.stereoDetectionHysteresisFrames {
		m.persistentMultichannelContentDetected = true
	}
	if m.detectionTimeoutThresholdFrames > 0 &&
		m.framesSinceStereoDetectedLast >= m.detectionTimeoutThresholdFrames {
		m.persistentMultichannelContentDetected = false
	}

	// Detect temporary multichannel content.
	m.temporaryMultichannelContentDetected = !m.persistentMultichannelContentDetected && stereoDetectedInFrame

	return previousPersistentMultichannelContentDetected != m.persistentMultichannelContentDetected
}

// isProperMultiChannelContentDetected mirrors
// IsProperMultiChannelContentDetected.
func (m *multiChannelContentDetector) isProperMultiChannelContentDetected() bool {
	return m.persistentMultichannelContentDetected
}

// isTemporaryMultiChannelContentDetected mirrors
// IsTemporaryMultiChannelContentDetected.
func (m *multiChannelContentDetector) isTemporaryMultiChannelContentDetected() bool {
	return m.temporaryMultichannelContentDetected
}
