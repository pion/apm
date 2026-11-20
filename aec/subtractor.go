// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// predictionError mirrors the upstream PredictionError helper:
// tmp = IFFT(echo); e = y - scale*tmp[N/2:]; s = scale*tmp[N/2:].
// scale = 1/kFftLengthBy2 = 2/N matches Ooura's IFFT semantics already
// applied inside aec3Fft.inv.
func predictionError(fft *aec3Fft, echo *fftData, y []float32, e, s *[kBlockSize]float32) {
	var tmp [kFftLength]float32
	fft.inv(echo, &tmp)
	const scale = 1.0 / float32(kFftLengthBy2)
	for k := range kBlockSize {
		v := tmp[kFftLengthBy2+k] * scale
		e[k] = y[k] - v
		s[k] = v
	}
}

// subtractor is a single-filter port of the upstream Subtractor: one
// AdaptiveFirFilter + one coarse-gain instance per capture channel,
// driving them per block. Upstream's coarse/refined dual-filter design
// and the misadjustment/leakage tracking are not yet ported.
type subtractor struct {
	fft     *aec3Fft
	filter  *adaptiveFirFilter
	gain    *coarseFilterUpdateGain
	rsa     *renderSignalAnalyzer
	impulse []float32
}

func newSubtractor(maxPartitions int) *subtractor {
	return &subtractor{
		fft:     newAec3Fft(),
		filter:  newAdaptiveFirFilter(maxPartitions, maxPartitions, 250, 1),
		gain:    newCoarseFilterUpdateGain(defaultCoarseFilterConfig(), 250),
		rsa:     newRenderSignalAnalyzer(),
		impulse: make([]float32, getTimeDomainLength(maxPartitions)),
	}
}

// process runs one 64-sample capture block through the subtractor.
// Until the dual coarse/refined filter design lands, the single filter
// populates both pairs of fields with identical values.
func (s *subtractor) process(rb *renderBuffer, capture []float32, out *subtractorOutput) {
	var S fftData
	s.filter.filter(rb, &S)
	predictionError(s.fft, &S, capture, &out.eRefined, &out.sRefined)
	out.eCoarse = out.eRefined
	out.sCoarse = out.sRefined

	// FFT of the error signal: Hanning-windowed, zero-padded, matching
	// upstream ZeroPaddedFft + kHanning.
	var errBlock [kFftLengthBy2]float32
	copy(errBlock[:], out.eRefined[:])
	s.fft.zeroPaddedFft(&errBlock, windowHanning, &out.ERefined)
	out.ERefined.spectrum(&out.E2Refined)
	out.E2Coarse = out.E2Refined

	// Per-block power summaries (matches Subtractor::Process  to
	// SubtractorOutput::ComputeMetrics).
	out.computeMetrics(capture)

	// Render power: sum of recent render spectra over the active filter
	// length (mirrors SpectralSum in Subtractor::Process).
	var X2 [kFftLengthBy2Plus1]float32
	rb.spectralSum(s.filter.sizePartitions(), &X2)

	var G fftData
	s.gain.compute(&X2, s.rsa, &out.ERefined, s.filter.sizePartitions(), false, &G)
	s.filter.adaptUpdateImpulse(rb, &G, &s.impulse)
}

// handleEchoPathChange resets the filter and gain.
func (s *subtractor) handleEchoPathChange() {
	s.filter.handleEchoPathChange()
	s.gain.handleEchoPathChange()
}
