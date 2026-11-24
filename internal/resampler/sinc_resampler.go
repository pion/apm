// SPDX-FileCopyrightText: 2013 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

// Package resampler ports the windowed-sinc resampler WebRTC uses to
// convert streams to and from its native processing rates, from
// common_audio/resampler.
package resampler

import "math"

const (
	// kernelSize is the number of taps of each windowed-sinc kernel.
	kernelSize = 32
	// kernelOffsetCount is the number of sub-sample kernel offsets that
	// are interpolated between.
	kernelOffsetCount  = 32
	kernelStorageSize  = kernelSize * (kernelOffsetCount + 1)
	sincScaleReduction = 0.9
)

// readFunc provides the next frames of input: it must fill destination
// completely.
type readFunc func(destination []float32)

// sincResampler mirrors SincResampler. It pulls requestFrames of input
// at a time through read and produces output at 1/ioSampleRateRatio
// times the input rate.
//
// The input buffer is divided in regions r0 to r4, kept as offsets:
//
//	|----------------|-----------------------------------------|----------------|
//	                                   requestFrames
//	                  <--------------------------------------------------------->
//	                                r0 (during first load)
//	 kernelSize / 2   kernelSize / 2          kernelSize / 2   kernelSize / 2
//	<---------------> <--------------->       <---------------> <--------------->
//	       r1                r2                      r3                r4
//	                           blockSize == r4 - r2
//	                  <--------------------------------------->
//
// On the second load r0 slides to the right by kernelSize / 2, and r3,
// r4 and blockSize follow. The regions then stay constant until flush.
type sincResampler struct {
	ioSampleRateRatio float64
	virtualSourceIdx  float64
	bufferPrimed      bool
	read              readFunc
	requestFrames     int
	blockSize         int
	kernelStorage     [kernelStorageSize]float32
	inputBuffer       []float32
	r0, r3, r4        int
}

const (
	r1 = 0
	r2 = kernelSize / 2
)

// sincScaleFactor is the normalized cutoff frequency of the low-pass
// filter. The windowed sinc does not cut off right away, so the cutoff is
// moved slightly downward to avoid aliasing at the very high end.
func sincScaleFactor(ioRatio float64) float64 {
	scale := 1.0
	if ioRatio > 1 {
		scale = 1 / ioRatio
	}

	return scale * sincScaleReduction
}

func newSincResampler(ioSampleRateRatio float64, requestFrames int, read readFunc) *sincResampler {
	resampler := &sincResampler{
		ioSampleRateRatio: ioSampleRateRatio,
		read:              read,
		requestFrames:     requestFrames,
		inputBuffer:       make([]float32, requestFrames+kernelSize),
	}
	resampler.flush()
	resampler.initializeKernel()

	return resampler
}

// updateRegions sets up the regions of the input buffer. On the second
// load r0 slides to the right by kernelSize / 2.
func (r *sincResampler) updateRegions(secondLoad bool) {
	r.r0 = kernelSize / 2
	if secondLoad {
		r.r0 = kernelSize
	}
	r.r3 = r.r0 + r.requestFrames - kernelSize
	r.r4 = r.r0 + r.requestFrames - kernelSize/2
	r.blockSize = r.r4 - r2
}

// initializeKernel generates the windowed-sinc kernels for sub-sample
// offsets from 0 to 1. The float32 and float64 steps follow upstream.
func (r *sincResampler) initializeKernel() {
	// Blackman window parameters.
	const (
		alpha = 0.16
		a0    = 0.5 * (1.0 - alpha)
		a1    = 0.5
		a2    = 0.5 * alpha
	)

	scale := sincScaleFactor(r.ioSampleRateRatio)
	for offsetIdx := 0; offsetIdx <= kernelOffsetCount; offsetIdx++ {
		subsampleOffset := float32(offsetIdx) / kernelOffsetCount

		for i := range kernelSize {
			preSinc := float32(math.Pi * float64(float32(i-kernelSize/2)-subsampleOffset))

			// Blackman window, matching the offset of the sinc.
			x := float64((float32(i) - subsampleOffset) / kernelSize)
			window := float64(float32(a0 - a1*math.Cos(2*math.Pi*x) + a2*math.Cos(4*math.Pi*x)))

			sinc := scale
			if preSinc != 0 {
				sinc = math.Sin(scale*float64(preSinc)) / float64(preSinc)
			}
			r.kernelStorage[i+offsetIdx*kernelSize] = float32(window * sinc)
		}
	}
}

// resample writes len(destination) frames of output, reading input as
// needed.
func (r *sincResampler) resample(destination []float32) {
	remainingFrames := len(destination)
	next := 0

	// Step (1): prime the input buffer at the start of the input stream.
	if !r.bufferPrimed && remainingFrames > 0 {
		r.read(r.inputBuffer[r.r0 : r.r0+r.requestFrames])
		r.bufferPrimed = true
	}

	// Step (2): resample.
	ratio := r.ioSampleRateRatio
	for remainingFrames > 0 {
		// The count may be negative if the last call ended on an iteration
		// that put virtualSourceIdx over the limit.
		for i := int(math.Ceil((float64(r.blockSize) - r.virtualSourceIdx) / ratio)); i > 0; i-- {
			// virtualSourceIdx lies between two kernel offsets.
			sourceIdx := int(r.virtualSourceIdx)
			subsampleRemainder := r.virtualSourceIdx - float64(sourceIdx)

			virtualOffsetIdx := subsampleRemainder * kernelOffsetCount
			offsetIdx := int(virtualOffsetIdx)

			// The two kernels which straddle virtualSourceIdx.
			k1 := r.kernelStorage[offsetIdx*kernelSize:]
			k2 := k1[kernelSize:]
			input := r.inputBuffer[r1+sourceIdx:]

			destination[next] = convolve(input, k1, k2, virtualOffsetIdx-float64(offsetIdx))
			next++

			r.virtualSourceIdx += ratio

			remainingFrames--
			if remainingFrames == 0 {
				return
			}
		}

		// Wrap back around to the start.
		r.virtualSourceIdx -= float64(r.blockSize)

		// Step (3): copy r3, r4 to r1, r2. This wraps the last input
		// frames back to the start of the buffer.
		copy(r.inputBuffer[r1:r1+kernelSize], r.inputBuffer[r.r3:r.r3+kernelSize])

		// Step (4): reinitialize the regions if necessary.
		if r.r0 == r2 {
			r.updateRegions(true)
		}

		// Step (5): refresh the buffer with more input.
		r.read(r.inputBuffer[r.r0 : r.r0+r.requestFrames])
	}
}

// chunkSize is the number of output frames one input request yields.
func (r *sincResampler) chunkSize() int {
	return int(float64(r.blockSize) / r.ioSampleRateRatio)
}

// flush drops the buffered input and restarts the stream.
func (r *sincResampler) flush() {
	r.virtualSourceIdx = 0
	r.bufferPrimed = false
	clear(r.inputBuffer)
	r.updateRegions(false)
}

// convolve mirrors Convolve_C: it generates one output sample by linear
// interpolation between the convolutions with two kernels.
func convolve(input, k1, k2 []float32, kernelInterpolationFactor float64) float32 {
	var sum1, sum2 float32
	for i := range kernelSize {
		sum1 += input[i] * k1[i]
		sum2 += input[i] * k2[i]
	}

	return float32((1-kernelInterpolationFactor)*float64(sum1) + kernelInterpolationFactor*float64(sum2))
}
