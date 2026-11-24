// SPDX-FileCopyrightText: 2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// dominantNearendDetector mirrors DominantNearendDetector: it flags the
// nearend state when the nearend is sufficiently stronger than the echo
// and the noise.
type dominantNearendDetector struct {
	config          dominantNearendDetectionConfig
	nearendState    bool
	triggerCounters []int
	holdCounters    []int
}

func newDominantNearendDetector(config dominantNearendDetectionConfig, numCaptureChannels int) *dominantNearendDetector {
	return &dominantNearendDetector{
		config:          config,
		triggerCounters: make([]int, numCaptureChannels),
		holdCounters:    make([]int, numCaptureChannels),
	}
}

func (d *dominantNearendDetector) isNearendState() bool { return d.nearendState }

func (d *dominantNearendDetector) update(
	nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum [][kFftLengthBy2Plus1]float32,
	initialState bool,
) {
	d.nearendState = false

	for ch := range d.triggerCounters {
		neSum := sum(nearendSpectrum[ch][1:16])
		echoSum := sum(residualEchoSpectrum[ch][1:16])
		noiseSum := sum(comfortNoiseSpectrum[ch][1:16])

		// Detect strong active nearend if the nearend is sufficiently
		// stronger than the echo and the nearend noise.
		if (!initialState || d.config.useDuringInitialPhase) &&
			echoSum < d.config.enrThreshold*neSum && neSum > d.config.snrThreshold*noiseSum {
			d.triggerCounters[ch]++
			if d.triggerCounters[ch] >= d.config.triggerThreshold {
				// After a period of strong active nearend activity, flag
				// the nearend mode.
				d.holdCounters[ch] = d.config.holdDuration
				d.triggerCounters[ch] = d.config.triggerThreshold
			}
		} else {
			// Forget previously detected strong active nearend activity.
			d.triggerCounters[ch] = max(0, d.triggerCounters[ch]-1)
		}

		// Exit the nearend state early at strong echo.
		if echoSum > d.config.enrExitThreshold*neSum && echoSum > d.config.snrThreshold*noiseSum {
			d.holdCounters[ch] = 0
		}

		// Remain in any nearend mode for a certain duration.
		d.holdCounters[ch] = max(0, d.holdCounters[ch]-1)
		d.nearendState = d.nearendState || d.holdCounters[ch] > 0
	}
}

func (d *dominantNearendDetector) setConfig(config suppressorConfig) {
	d.config = config.dominantNearendDetection
}
