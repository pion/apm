// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "math"

// getNoiseFloorFactor matches GetNoiseFloorFactor: the noise floor value
// that matches a WGN input of noiseFloorDbfs.
func getNoiseFloorFactor(noiseFloorDbfs float32) float32 {
	// dbfsNormalization is 20*log10(32768).
	const dbfsNormalization = 90.30899869919436

	return 64 * float32(math.Pow(10, float64((dbfsNormalization+noiseFloorDbfs)*0.1)))
}

// sqrt2Sin matches kSqrt2Sin: a table of sqrt(2) * sin(2*pi*i/32).
func sqrt2Sin() [32]float32 {
	return [32]float32{
		+0.0000000, +0.2758994, +0.5411961, +0.7856950, +1.0000000,
		+1.1758756, +1.3065630, +1.3870398, +math.Sqrt2, +1.3870398,
		+1.3065630, +1.1758756, +1.0000000, +0.7856950, +0.5411961,
		+0.2758994, +0.0000000, -0.2758994, -0.5411961, -0.7856950,
		-1.0000000, -1.1758756, -1.3065630, -1.3870398, -math.Sqrt2,
		-1.3870398, -1.3065630, -1.1758756, -1.0000000, -0.7856950,
		-0.5411961, -0.2758994,
	}
}

// generateRandomSinTableIndices matches GenerateRandomSinTableIndices.
func generateRandomSinTableIndices(seed *uint32, reSinTableIndices, imSinTableIndices *[kFftLengthBy2 - 1]int) {
	const (
		seedMask    = 0x80000000 - 1
		imIndexMask = 32 - 1
	)
	for k := range reSinTableIndices {
		// Generate a random 31-bit integer.
		*seed = (*seed*69069 + 1) & seedMask

		// Convert to 5-bit indices.
		reSinTableIndices[k] = int(*seed >> 26)
		imSinTableIndices[k] = (reSinTableIndices[k] + 8) & imIndexMask
	}
}

// generateComfortNoise matches GenerateComfortNoise.
func generateComfortNoise(
	n2 *[kFftLengthBy2Plus1]float32,
	reSinTableIndices, imSinTableIndices *[kFftLengthBy2 - 1]int,
	lowerBandNoise, upperBandNoise *fftData,
) {
	const (
		oneByNumBands        = 1.0 / (kFftLengthBy2Plus1/2 + 1)
		fftLengthBy2Plus1By2 = kFftLengthBy2Plus1 / 2
	)
	table := sqrt2Sin()

	// Compute the square root spectrum.
	var noise [kFftLengthBy2Plus1]float32
	for k, value := range n2 {
		noise[k] = float32(math.Sqrt(float64(value))) //nolint:gosec // G602 false positive on a range loop.
	}

	// Compute the noise level for the upper bands.
	highBandNoiseLevel := sum(noise[fftLengthBy2Plus1By2:]) * oneByNumBands

	// The analysis and synthesis windowing cause a loss of power when
	// cross-fading the noise where the frames are completely uncorrelated,
	// as they are generated with a random phase, hence the factor sqrt(2).
	// This is not the case for the speech signal, where the input is
	// overlapping.
	lowerBandNoise.re[0], lowerBandNoise.re[kFftLengthBy2] = 0, 0
	upperBandNoise.re[0], upperBandNoise.re[kFftLengthBy2] = 0, 0
	for k := 1; k < kFftLengthBy2; k++ {
		// x = sqrt(2) * sin(a) and y = sqrt(2) * cos(a).
		x := table[reSinTableIndices[k-1]]
		y := table[imSinTableIndices[k-1]]

		// Form the low-frequency noise via spectral shaping.
		lowerBandNoise.re[k] = noise[k] * x
		lowerBandNoise.im[k] = noise[k] * y

		// Form the high-frequency noise via simple leveling.
		upperBandNoise.re[k] = highBandNoiseLevel * x
		upperBandNoise.im[k] = highBandNoiseLevel * y
	}
}

// comfortNoiseGenerator mirrors ComfortNoiseGenerator: it generates the
// comfort noise.
type comfortNoiseGenerator struct {
	seed       uint32
	noiseFloor float32
	// n2Initial is nil once the initial phase is over.
	n2Initial  [][kFftLengthBy2Plus1]float32
	y2Smoothed [][kFftLengthBy2Plus1]float32
	n2         [][kFftLengthBy2Plus1]float32
	n2Counter  int
}

func newComfortNoiseGenerator(config echoCanceller3Config, numCaptureChannels int) *comfortNoiseGenerator {
	generator := &comfortNoiseGenerator{
		seed:       42,
		noiseFloor: getNoiseFloorFactor(config.comfortNoise.noiseFloorDbfs),
		n2Initial:  make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		y2Smoothed: make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
		n2:         make([][kFftLengthBy2Plus1]float32, numCaptureChannels),
	}
	for ch := range generator.n2 {
		fill(generator.n2[ch][:], 1.0e6)
	}

	return generator
}

// compute mirrors Compute: it computes the comfort noise.
func (c *comfortNoiseGenerator) compute(
	saturatedCapture bool,
	captureSpectrum [][kFftLengthBy2Plus1]float32,
	lowerBandNoise, upperBandNoise []fftData,
) {
	if !saturatedCapture {
		c.updateNoiseEstimate(captureSpectrum)
	}

	// Choose the N2 estimate to use.
	n2 := c.n2
	if c.n2Initial != nil {
		n2 = c.n2Initial
	}

	var reSinTableIndices, imSinTableIndices [kFftLengthBy2 - 1]int
	generateRandomSinTableIndices(&c.seed, &reSinTableIndices, &imSinTableIndices)

	for ch := range c.n2 {
		generateComfortNoise(&n2[ch], &reSinTableIndices, &imSinTableIndices, &lowerBandNoise[ch], &upperBandNoise[ch])
	}
}

// updateNoiseEstimate updates the estimate of the background noise
// spectrum with the spectrum of the capture signal.
func (c *comfortNoiseGenerator) updateNoiseEstimate(y2 [][kFftLengthBy2Plus1]float32) {
	// Smooth Y2.
	for ch := range c.y2Smoothed {
		for k, smoothed := range c.y2Smoothed[ch] {
			c.y2Smoothed[ch][k] = smoothed + 0.1*(y2[ch][k]-smoothed)
		}
	}

	if c.n2Counter > 50 {
		c.updateNoiseFromSmoothedCapture()
	}

	if c.n2Initial != nil {
		c.n2Counter++
		if c.n2Counter == 1000 {
			c.n2Initial = nil
		} else {
			c.updateInitialNoiseEstimate()
		}
	}

	for ch := range c.n2 {
		for k := range c.n2[ch] {
			c.n2[ch][k] = max(c.n2[ch][k], c.noiseFloor)
		}
		if c.n2Initial != nil {
			for k := range c.n2Initial[ch] {
				c.n2Initial[ch][k] = max(c.n2Initial[ch][k], c.noiseFloor)
			}
		}
	}
}

// updateNoiseFromSmoothedCapture updates N2 from the smoothed Y2.
func (c *comfortNoiseGenerator) updateNoiseFromSmoothedCapture() {
	for ch := range c.n2 {
		for k, noise := range c.n2[ch] {
			smoothed := c.y2Smoothed[ch][k]
			if smoothed < noise {
				c.n2[ch][k] = (0.9*smoothed + 0.1*noise) * 1.0002
			} else {
				c.n2[ch][k] = noise * 1.0002
			}
		}
	}
}

// updateInitialNoiseEstimate computes the initial N2 from N2.
func (c *comfortNoiseGenerator) updateInitialNoiseEstimate() {
	for ch := range c.n2 {
		for k, noise := range c.n2[ch] {
			initial := c.n2Initial[ch][k]
			if noise > initial {
				c.n2Initial[ch][k] = initial + 0.001*(noise-initial)
			} else {
				c.n2Initial[ch][k] = noise
			}
		}
	}
}

// noiseSpectrum mirrors NoiseSpectrum: the estimate of the background
// noise spectrum.
func (c *comfortNoiseGenerator) noiseSpectrum() [][kFftLengthBy2Plus1]float32 { return c.n2 }
