// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// Helpers ported from adaptive_fir_filter.cc: the _C variants only.

// adaptPartitions implements H(t+1) = H(t) + conj(X(t)) * G(t), with H the
// partitions, X the render spectra and G the gain.
// Mirrors AdaptPartitions (the _C variant).
func adaptPartitions(rb *renderBuffer, gain *fftData, numPartitions int, partitions [][]fftData) {
	data := rb.getFftBuffer()
	idx := rb.position()
	numRenderChannels := len(data[idx])
	for p := range numPartitions {
		for ch := range numRenderChannels {
			render := &data[idx][ch]
			part := &partitions[p][ch]
			for k := range kFftLengthBy2Plus1 {
				part.re[k] += render.re[k]*gain.re[k] + render.im[k]*gain.im[k]
				part.im[k] += render.re[k]*gain.im[k] - render.im[k]*gain.re[k]
			}
		}
		if idx < len(data)-1 {
			idx++
		} else {
			idx = 0
		}
	}
}

// applyFilter computes the echo estimate as the sum over partitions and
// channels of H[p][ch] * X[idx+p][ch].
// Mirrors ApplyFilter (the _C variant).
func applyFilter(rb *renderBuffer, numPartitions int, partitions [][]fftData, echo *fftData) {
	echo.clear()
	data := rb.getFftBuffer()
	idx := rb.position()
	numRenderChannels := len(data[idx])
	for p := range numPartitions {
		for ch := range numRenderChannels {
			render := &data[idx][ch]
			part := &partitions[p][ch]
			for k := range kFftLengthBy2Plus1 {
				echo.re[k] += render.re[k]*part.re[k] - render.im[k]*part.im[k]
				echo.im[k] += render.re[k]*part.im[k] + render.im[k]*part.re[k]
			}
		}
		if idx < len(data)-1 {
			idx++
		} else {
			idx = 0
		}
	}
}

// adaptiveFirFilter mirrors AdaptiveFirFilter.
type adaptiveFirFilter struct {
	fft                           *aec3Fft
	numRenderChannels             int
	maxSizePartitions             int
	sizeChangeDurationBlocks      int
	oneBySizeChangeDurationBlocks float32
	currentSizePartitions         int
	targetSizePartitions          int
	oldTargetSizePartitions       int
	sizeChangeCounter             int
	H                             [][]fftData // [maxSizePartitions][numRenderChannels]
	partitionToConstrain          int
}

func newAdaptiveFirFilter(maxSizePartitions, initialSizePartitions, sizeChangeDurationBlocks, numRenderChannels int) *adaptiveFirFilter {
	filt := &adaptiveFirFilter{
		fft:                      newAec3Fft(),
		numRenderChannels:        numRenderChannels,
		maxSizePartitions:        maxSizePartitions,
		sizeChangeDurationBlocks: sizeChangeDurationBlocks,
		currentSizePartitions:    initialSizePartitions,
		targetSizePartitions:     initialSizePartitions,
		oldTargetSizePartitions:  initialSizePartitions,
	}
	filt.oneBySizeChangeDurationBlocks = 1.0 / float32(sizeChangeDurationBlocks)
	filt.H = make([][]fftData, maxSizePartitions)
	for p := range filt.H {
		filt.H[p] = make([]fftData, numRenderChannels)
	}
	filt.setSizePartitions(initialSizePartitions, true)

	return filt
}

// sizePartitions returns the current filter length in partitions.
func (f *adaptiveFirFilter) sizePartitions() int { return f.currentSizePartitions }

// handleEchoPathChange clears partitions beyond the current size.
func (f *adaptiveFirFilter) handleEchoPathChange() {
	zeroFilter(f.currentSizePartitions, f.maxSizePartitions, f.H)
}

// setSizePartitions sets a target filter length. With immediateEffect,
// the filter resizes now; otherwise it ramps over sizeChangeDurationBlocks.
func (f *adaptiveFirFilter) setSizePartitions(size int, immediateEffect bool) {
	if size > f.maxSizePartitions {
		size = f.maxSizePartitions
	}
	f.targetSizePartitions = size
	if immediateEffect {
		old := f.currentSizePartitions
		f.currentSizePartitions = f.targetSizePartitions
		f.oldTargetSizePartitions = f.targetSizePartitions
		zeroFilter(old, f.currentSizePartitions, f.H)
		if f.partitionToConstrain > f.currentSizePartitions-1 {
			f.partitionToConstrain = f.currentSizePartitions - 1
		}
		f.sizeChangeCounter = 0
	} else {
		f.sizeChangeCounter = f.sizeChangeDurationBlocks
	}
}

func (f *adaptiveFirFilter) updateSize() {
	oldSize := f.currentSizePartitions
	if f.sizeChangeCounter > 0 {
		f.sizeChangeCounter--
		changeFactor := float32(f.sizeChangeCounter) * f.oneBySizeChangeDurationBlocks
		from := float32(f.oldTargetSizePartitions)
		to := float32(f.targetSizePartitions)
		f.currentSizePartitions = int(from*changeFactor + to*(1-changeFactor))
		if f.partitionToConstrain > f.currentSizePartitions-1 {
			f.partitionToConstrain = f.currentSizePartitions - 1
		}
	} else {
		f.currentSizePartitions = f.targetSizePartitions
		f.oldTargetSizePartitions = f.targetSizePartitions
	}
	zeroFilter(oldSize, f.currentSizePartitions, f.H)
}

// filter computes the echo estimate from the current filter and the render buffer.
func (f *adaptiveFirFilter) filter(rb *renderBuffer, echo *fftData) {
	applyFilter(rb, f.currentSizePartitions, f.H, echo)
}

// adaptUpdateImpulse adapts the filter using gain and also updates an
// externally stored time-domain impulse response.
func (f *adaptiveFirFilter) adaptUpdateImpulse(rb *renderBuffer, gain *fftData, impulseResponse *[]float32) {
	f.updateSize()
	adaptPartitions(rb, gain, f.currentSizePartitions, f.H)
	f.constrainAndUpdateImpulseResponse(impulseResponse)
}

// constrainAndUpdateImpulseResponse forces the time-domain taps of one
// partition to be band-limited (first half kept, second half zeroed),
// rotating through partitions one per call, and stores the taps in the
// supplied impulse response.
func (f *adaptiveFirFilter) constrainAndUpdateImpulseResponse(impulseResponse *[]float32) {
	want := getTimeDomainLength(f.currentSizePartitions)
	if cap(*impulseResponse) < want {
		*impulseResponse = make([]float32, want)
	} else {
		*impulseResponse = (*impulseResponse)[:want]
	}
	start := f.partitionToConstrain * kFftLengthBy2
	partIR := (*impulseResponse)[start : start+kFftLengthBy2]
	clear(partIR)

	var taps [kFftLength]float32
	for ch := range f.numRenderChannels {
		f.fft.inv(&f.H[f.partitionToConstrain][ch], &taps)
		const kScale = 1.0 / float32(kFftLengthBy2)
		for i := range kFftLengthBy2 {
			taps[i] *= kScale
		}
		clear(taps[kFftLengthBy2:])
		mergeImpulseResponse(ch == 0, taps[:kFftLengthBy2], partIR)
		f.fft.fwd(&taps, &f.H[f.partitionToConstrain][ch])
	}

	if f.partitionToConstrain < f.currentSizePartitions-1 {
		f.partitionToConstrain++
	} else {
		f.partitionToConstrain = 0
	}
}

// mergeImpulseResponse copies the first channel's taps and keeps the
// largest magnitude tap across the other channels.
func mergeImpulseResponse(first bool, taps, dst []float32) {
	if first {
		copy(dst, taps)

		return
	}
	for k, v := range taps {
		if abs32(v) > abs32(dst[k]) {
			dst[k] = v
		}
	}
}

func zeroFilter(oldSize, newSize int, partitions [][]fftData) {
	for p := oldSize; p < newSize; p++ {
		for ch := range partitions[p] {
			partitions[p][ch].clear()
		}
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}

	return x
}
