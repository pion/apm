// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// suppressionFilter mirrors SuppressionFilter: it applies the suppression
// gains to the capture signal and adds the comfort noise.
type suppressionFilter struct {
	numCaptureChannels int
	fft                *aec3Fft
	// eOutputOld holds, per band and channel, the second half of the
	// previous synthesis for the first band and the previous block for
	// the upper bands.
	eOutputOld [][][kFftLengthBy2]float32
}

func newSuppressionFilter(sampleRateHz, numCaptureChannels int) *suppressionFilter {
	filter := &suppressionFilter{
		numCaptureChannels: numCaptureChannels,
		fft:                newAec3Fft(),
		eOutputOld:         make([][][kFftLengthBy2]float32, numBandsForRate(sampleRateHz)),
	}
	for band := range filter.eOutputOld {
		filter.eOutputOld[band] = make([][kFftLengthBy2]float32, numCaptureChannels)
	}

	return filter
}

// applyGain mirrors ApplyGain. eLowestBand is the spectrum of the first
// band of the capture signal, and capture holds the upper bands and
// receives the output.
func (s *suppressionFilter) applyGain(
	comfortNoise, comfortNoiseHighBand []fftData,
	gain *[kFftLengthBy2Plus1]float32,
	highBandsGain float32,
	eLowestBand []fftData,
	capture *block,
) {
	// The comfort noise gain is sqrt(1-g^2), where g is the suppression
	// gain.
	var noiseGain [kFftLengthBy2Plus1]float32
	for i, value := range gain {
		noiseGain[i] = float32(math.Sqrt(float64(1 - value*value))) //nolint:gosec // G602 false positive on a range loop.
	}
	highBandsNoiseScaling := 0.4 * float32(math.Sqrt(float64(1-highBandsGain*highBandsGain)))

	for ch := range eLowestBand[:s.numCaptureChannels] {
		s.applyLowBandGain(ch, &comfortNoise[ch], gain, &noiseGain, &eLowestBand[ch], capture.view(0, ch))

		// Apply the suppression gain to the upper bands.
		for band := 1; band < capture.numBands; band++ {
			for i, sample := range capture.view(band, ch) {
				capture.view(band, ch)[i] = sample * highBandsGain
			}
		}

		// Add comfort noise to band 1.
		if capture.numBands > 1 {
			s.addHighBandNoise(&comfortNoiseHighBand[ch], highBandsNoiseScaling, capture.view(1, ch))
		}

		// Delay the upper bands to match the delay of the filter bank.
		for band := 1; band < capture.numBands; band++ {
			samples := capture.view(band, ch)
			old := &s.eOutputOld[band][ch]
			for i := range samples {
				samples[i], old[i] = old[i], samples[i]
			}
		}

		// Clamp the output of all bands.
		for band := range capture.numBands {
			for i, sample := range capture.view(band, ch) {
				capture.view(band, ch)[i] = clamp32(sample, -32768, 32767)
			}
		}
	}
}

// ifftNormalization scales the output of the inverse FFT.
const ifftNormalization = 2.0 / kFftLength

// applyLowBandGain applies the suppression gains to the first band of a
// channel, adds the comfort noise and synthesizes the output.
func (s *suppressionFilter) applyLowBandGain(
	ch int,
	comfortNoise *fftData,
	gain, noiseGain *[kFftLengthBy2Plus1]float32,
	eLowestBand *fftData,
	output []float32,
) {
	// Apply the suppression gains, then scale and add the comfort noise.
	var spectrum fftData
	for i := range spectrum.re {
		spectrum.re[i] = eLowestBand.re[i]*gain[i] + noiseGain[i]*comfortNoise.re[i]
		spectrum.im[i] = eLowestBand.im[i]*gain[i] + noiseGain[i]*comfortNoise.im[i]
	}

	// Synthesis filterbank.
	var extended [kFftLength]float32
	s.fft.inv(&spectrum, &extended)

	// Window and add the first half of extended with the second half of
	// extended from the previous block.
	old := &s.eOutputOld[0][ch]
	for i := range output {
		sample := old[i] * kSqrtHanning128[kFftLengthBy2+i]
		sample += extended[i] * kSqrtHanning128[i]
		output[i] = sample * ifftNormalization
	}

	// The second half of extended is stored for the succeeding frame.
	copy(old[:], extended[kFftLengthBy2:])
}

// addHighBandNoise adds the comfort noise of the upper bands to a band.
func (s *suppressionFilter) addHighBandNoise(comfortNoise *fftData, scaling float32, output []float32) {
	var timeDomainHighBandNoise [kFftLength]float32
	s.fft.inv(comfortNoise, &timeDomainHighBandNoise)

	gain := scaling * ifftNormalization
	for i := range output {
		output[i] += timeDomainHighBandNoise[i] * gain
	}
}
