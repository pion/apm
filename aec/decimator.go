// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "github.com/pion/apm/internal/dsp/biquad"

// lowPassFilterDs4 is signal.ellip(6, 1, 40, 1800/8000, 'lowpass',
// output='sos').
func lowPassFilterDs4() []biquad.Coefficients {
	return []biquad.Coefficients{
		{B: [3]float32{0.0180919877, 0.00320961363, 0.0180919877}, A: [2]float32{-1.5183195, 0.633165865}},
		{B: [3]float32{1.0, -1.24550459, 1.0}, A: [2]float32{-1.49784254, 0.853586692}},
		{B: [3]float32{1.0, -1.4221681, 1.0}, A: [2]float32{-1.49791282, 0.969572384}},
	}
}

// bandPassFilterDs8 is signal.cheby1(1, 6, [1000/8000, 2000/8000],
// 'bandpass', output='sos') repeated 5 times.
func bandPassFilterDs8() []biquad.Coefficients {
	section := biquad.Coefficients{
		B: [3]float32{0.103304783, 0.0, -0.103304783},
		A: [2]float32{-1.520363, 0.793390435},
	}

	return []biquad.Coefficients{section, section, section, section, section}
}

// highPassFilter is signal.butter(2, 1000/8000.0, 'highpass',
// output='sos').
func highPassFilter() []biquad.Coefficients {
	return []biquad.Coefficients{
		{B: [3]float32{0.757076375, -1.51415275, 0.757076375}, A: [2]float32{-1.45424359, 0.574061915}},
	}
}

// decimator mirrors Decimator: it downsamples a block by a factor of 4
// or 8 for the delay estimation.
type decimator struct {
	downSamplingFactor   int
	antiAliasingFilter   *biquad.Cascade
	noiseReductionFilter *biquad.Cascade
}

// newDecimator constructs a decimator. downSamplingFactor must be 4 or 8.
func newDecimator(downSamplingFactor int) *decimator {
	if downSamplingFactor == 4 {
		return &decimator{
			downSamplingFactor:   downSamplingFactor,
			antiAliasingFilter:   biquad.NewCascade(lowPassFilterDs4()),
			noiseReductionFilter: biquad.NewCascade(highPassFilter()),
		}
	}

	// At a factor of 8 the band-pass filter does both jobs: the noise
	// reduction filter passes the signal through.
	return &decimator{
		downSamplingFactor:   downSamplingFactor,
		antiAliasingFilter:   biquad.NewCascade(bandPassFilterDs8()),
		noiseReductionFilter: biquad.NewCascade(nil),
	}
}

// decimate downsamples one kBlockSize block of in into out, which holds
// kBlockSize/downSamplingFactor samples.
func (d *decimator) decimate(in, out []float32) {
	var x [kBlockSize]float32
	copy(x[:], in)

	// Limit the frequency content of the signal to avoid aliasing.
	d.antiAliasingFilter.Process(x[:])

	// Reduce the impact of near-end noise.
	d.noiseReductionFilter.Process(x[:])

	// Downsample the signal.
	for j := range out {
		out[j] = x[j*d.downSamplingFactor]
	}
}
