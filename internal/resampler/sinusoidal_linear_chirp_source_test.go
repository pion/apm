// SPDX-FileCopyrightText: 2013 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package resampler

import "math"

const chirpMinFrequency = 5

// sinusoidalLinearChirpSource mirrors SinusoidalLinearChirpSource: a sine
// whose frequency rises linearly from 5 Hz to maxFrequency over
// totalSamples, after delaySamples of silence.
type sinusoidalLinearChirpSource struct {
	sampleRate   int
	totalSamples int
	maxFrequency float64
	k            float64
	currentIndex int
	delaySamples float64
}

func newSinusoidalLinearChirpSource(
	sampleRate, samples int, maxFrequency, delaySamples float64,
) *sinusoidalLinearChirpSource {
	duration := float64(samples) / float64(sampleRate)

	return &sinusoidalLinearChirpSource{
		sampleRate:   sampleRate,
		totalSamples: samples,
		maxFrequency: maxFrequency,
		k:            (maxFrequency - chirpMinFrequency) / duration,
		delaySamples: delaySamples,
	}
}

// run mirrors Run: it fills destination with the next samples.
func (s *sinusoidalLinearChirpSource) run(destination []float32) {
	for i := range destination {
		switch {
		case s.frequency(s.currentIndex) > 0.5*float64(s.sampleRate):
			destination[i] = 0
		case float64(s.currentIndex) < s.delaySamples:
			destination[i] = 0
		default:
			t := (float64(s.currentIndex) - s.delaySamples) / float64(s.sampleRate)
			destination[i] = float32(math.Sin(2 * math.Pi * (chirpMinFrequency*t + (s.k/2)*t*t)))
		}
		s.currentIndex++
	}
}

// frequency mirrors Frequency: the frequency at a position.
func (s *sinusoidalLinearChirpSource) frequency(position int) float64 {
	return chirpMinFrequency + (float64(position)-s.delaySamples)*
		(s.maxFrequency-chirpMinFrequency)/float64(s.totalSamples)
}

// resamplingErrors returns the RMS error, the largest error at low
// frequencies and the largest error at high frequencies of resampled
// against pure, in dBFS, like the upstream Resample tests. quantization is
// subtracted from the largest errors.
func resamplingErrors(
	resampled, pure []float32, pureSource *sinusoidalLinearChirpSource, minimumRate int, quantization float64,
) (rmsError, lowFreqMaxError, highFreqMaxError float64) {
	// The ranges of the Nyquist frequency of the lowest rate which are
	// referred to as low and high.
	const lowFrequencyNyquistRange = 0.7
	const highFrequencyNyquistRange = 0.9
	lowFrequencyRange := lowFrequencyNyquistRange * 0.5 * float64(minimumRate)
	highFrequencyRange := highFrequencyNyquistRange * 0.5 * float64(minimumRate)

	var sumOfSquares float64
	for i := range resampled {
		err := math.Abs(float64(resampled[i] - pure[i]))
		switch frequency := pureSource.frequency(i); {
		case frequency < lowFrequencyRange:
			lowFreqMaxError = max(lowFreqMaxError, err)
		case frequency < highFrequencyRange:
			highFreqMaxError = max(highFreqMaxError, err)
		}
		sumOfSquares += err * err
	}

	dbfs := func(x float64) float64 { return 20 * math.Log10(x) }

	return dbfs(math.Sqrt(sumOfSquares / float64(len(resampled)))),
		dbfs(lowFreqMaxError - quantization), dbfs(highFreqMaxError - quantization)
}
