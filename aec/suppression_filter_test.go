// SPDX-FileCopyrightText: 2017 The WebRTC project authors
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

// produceSinusoid ports ProduceSinusoid: it writes a sinusoid of the
// given frequency to the first band and zeroes the others.
func produceSinusoid(sampleRateHz int, sinusoidalFrequencyHz float32, sampleCounter *int, x *block) {
	for j := range kBlockSize {
		k := float32(*sampleCounter + j)
		for channel := range x.numChannels {
			x.view(0, channel)[j] = 32767 *
				float32(math.Sin(float64(2*math.Pi*sinusoidalFrequencyHz*k/float32(sampleRateHz))))
		}
	}
	*sampleCounter += kBlockSize

	for band := 1; band < x.numBands; band++ {
		for channel := range x.numChannels {
			fill(x.view(band, channel), 0)
		}
	}
}

// suppressionFilterTest holds the state of the upstream SuppressionFilter
// tests: a filter for one channel at 48 kHz without comfort noise.
type suppressionFilterTest struct {
	filter       *suppressionFilter
	comfortNoise []fftData
	highBands    []fftData
	captureOld   [kFftLengthBy2]float32
	fft          *aec3Fft
	gain         [kFftLengthBy2Plus1]float32
	capture      *block
}

func newSuppressionFilterTest(numChannels int) *suppressionFilterTest {
	test := &suppressionFilterTest{
		filter:       newSuppressionFilter(48000, 1),
		comfortNoise: make([]fftData, 1),
		highBands:    make([]fftData, 1),
		fft:          newAec3Fft(),
		capture:      newBlock(3, numChannels),
	}
	fill(test.gain[:], 1)

	return test
}

// apply computes the spectrum of the first band and applies the gain.
func (s *suppressionFilterTest) apply() {
	spectra := make([]fftData, 1)
	capture := (*[kBlockSize]float32)(s.capture.view(0, 0))
	s.fft.paddedFft(capture, &s.captureOld, windowSqrtHanning, &spectra[0])
	s.captureOld = *capture

	s.filter.applyGain(s.comfortNoise, s.highBands, &s.gain, 1, spectra, s.capture)
}

// Port of SuppressionFilter.ComfortNoiseInUnityGain: no comfort noise is
// added when the gain is 1.
func TestSuppressionFilter_ComfortNoiseInUnityGain(t *testing.T) {
	test := newSuppressionFilterTest(kBlockSize)
	for _, noise := range []*fftData{&test.comfortNoise[0], &test.highBands[0]} {
		fill(noise.re[:], 1)
		fill(noise.im[:], 1)
	}

	test.apply()

	for _, sample := range test.capture.data {
		assert.Zero(t, sample)
	}
}

// runSinusoid filters 100 blocks of a sinusoid and returns the energy of
// the first band before and after.
func (s *suppressionFilterTest) runSinusoid(frequencyHz float32) (input, output float32) {
	sampleCounter := 0
	for range 100 {
		produceSinusoid(16000, frequencyHz, &sampleCounter, s.capture)
		input += energy(s.capture.view(0, 0))
		s.apply()
		output += energy(s.capture.view(0, 0))
	}

	return input, output
}

// Port of SuppressionFilter.SignalSuppression: the suppressor is able to
// suppress a signal.
func TestSuppressionFilter_SignalSuppression(t *testing.T) {
	test := newSuppressionFilterTest(1)
	fill(test.gain[10:], 0)

	input, output := test.runSinusoid(16000 * 40 / kFftLengthBy2 / 2)
	assert.Less(t, output, input/1000)
}

// Port of SuppressionFilter.SignalTransparency: the suppressor is able to
// pass through a desired signal while suppressing some frequencies.
func TestSuppressionFilter_SignalTransparency(t *testing.T) {
	test := newSuppressionFilterTest(1)
	fill(test.gain[30:], 0)

	input, output := test.runSinusoid(16000 * 10 / kFftLengthBy2 / 2)
	assert.Less(t, 0.9*input, output)
}

// Port of SuppressionFilter.Delay: the delay of the suppressor.
func TestSuppressionFilter_Delay(t *testing.T) {
	test := newSuppressionFilterTest(1)

	for blockIndex := range 100 {
		for band := range test.capture.numBands {
			for sample := range kBlockSize {
				test.capture.view(band, 0)[sample] = float32(blockIndex*kBlockSize + sample)
			}
		}

		test.apply()
		if blockIndex <= 2 {
			continue
		}
		for band := range test.capture.numBands {
			for sample, value := range test.capture.view(band, 0) {
				assert.InDelta(t, blockIndex*kBlockSize+sample-kBlockSize, value, 0.01)
			}
		}
	}
}

// TestSuppressionFilter_MatchesUpstream compares the output with the
// upstream SuppressionFilter at the pinned commit. Random gains are
// applied to 40 blocks of noise, with random comfort noise. The noise is
// beyond full scale in blocks 20 to 24. Each line of the reference file
// is keyed by "name sample_rate capture_channels" and holds the block,
// the channel and the band, then the samples.
func TestSuppressionFilter_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct{ sampleRateHz, numChannels int }{{48000, 2}, {16000, 1}} {
		suffix := fmt.Sprintf(" %d %d", tc.sampleRateHz, tc.numChannels)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/suppression_filter_upstream.txt", suffix)
			numBands := numBandsForRate(tc.sampleRateHz)
			filter := newSuppressionFilter(tc.sampleRateHz, tc.numChannels)
			fft := newAec3Fft()
			random := newTestRandom(42)
			comfortNoise := make([]fftData, tc.numChannels)
			highBands := make([]fftData, tc.numChannels)
			spectra := make([]fftData, tc.numChannels)
			captureOld := make([][kFftLengthBy2]float32, tc.numChannels)
			var gain [kFftLengthBy2Plus1]float32
			capture := newBlock(numBands, tc.numChannels)

			for index := range 40 {
				amplitude := float32(20000)
				if inRange(index, 20, 25) {
					amplitude = 60000
				}
				for k := range gain {
					gain[k] = random.randFloat()
				}
				highBandsGain := random.randFloat()
				for ch := range tc.numChannels {
					for band := range numBands {
						randomizeSampleVectorAmplitude(random, capture.view(band, ch), amplitude)
					}
					for _, noise := range []*fftData{&comfortNoise[ch], &highBands[ch]} {
						randomizeSampleVectorAmplitude(random, noise.re[:], 3000)
						randomizeSampleVectorAmplitude(random, noise.im[:], 3000)
					}
					samples := (*[kBlockSize]float32)(capture.view(0, ch))
					fft.paddedFft(samples, &captureOld[ch], windowSqrtHanning, &spectra[ch])
					captureOld[ch] = *samples
				}
				filter.applyGain(comfortNoise, highBands, &gain, highBandsGain, spectra, capture)

				for ch := range tc.numChannels {
					for band := range numBands {
						want := reference.next("output")
						require.Equal(t, []float32{float32(index), float32(ch), float32(band)}, want[:3])
						require.InDeltaSlice(t, want[3:], capture.view(band, ch), 0.05, "block %d", index)
					}
				}
			}
			require.Empty(t, reference.entries)
		})
	}
}
