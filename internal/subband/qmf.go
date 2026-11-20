// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

// QMF 2-band filter. Ported from
// common_audio/signal_processing/splitting_filter.c. Used for the 32 kHz
// path (one 320-sample frame splits into two 160-sample bands at 16 kHz
// each).

// QMFState holds the cascaded all-pass filter state for one channel.
type QMFState struct {
	AnalysisState1  [6]float32
	AnalysisState2  [6]float32
	SynthesisState1 [6]float32
	SynthesisState2 [6]float32
}

// Coefficients for the two cascade chains. Verbatim from upstream.
func allPassFilter1() [3]float32 { return [3]float32{0.0979309082, 0.5643005371, 0.8737335205} }
func allPassFilter2() [3]float32 { return [3]float32{0.32551574707, 0.74862670898, 0.96145629882} }

// allPassQMF applies three cascaded first-order all-pass filters in
// place. Mirrors WebRtcSpl_AllPassQMF.
func allPassQMF(inData, outData []float32, coeffs [3]float32, state []float32) {
	n := len(inData)

	// First cascade
	diff := inData[0] - state[1]
	outData[0] = state[0] + coeffs[0]*diff
	for k := 1; k < n; k++ {
		diff = inData[k] - outData[k-1]
		outData[k] = inData[k-1] + coeffs[0]*diff
	}
	state[0] = inData[n-1]
	state[1] = outData[n-1]

	// Second cascade: writes back into inData as a scratch.
	diff = outData[0] - state[3]
	inData[0] = state[2] + coeffs[1]*diff
	for k := 1; k < n; k++ {
		diff = outData[k] - inData[k-1]
		inData[k] = outData[k-1] + coeffs[1]*diff
	}
	state[2] = outData[n-1]
	state[3] = inData[n-1]

	// Third cascade: final output back to outData.
	diff = inData[0] - state[5]
	outData[0] = state[4] + coeffs[2]*diff
	for k := 1; k < n; k++ {
		diff = inData[k] - outData[k-1]
		outData[k] = inData[k-1] + coeffs[2]*diff
	}
	state[4] = inData[n-1]
	state[5] = outData[n-1]
}

// AnalysisQMF splits one 320-sample frame into low/high 160-sample bands.
func AnalysisQMF(in, lowBand, highBand []float32, state *QMFState) {
	if len(in)%2 != 0 {
		panic("subband: AnalysisQMF input length must be even") //nolint:forbidigo // Invariant violated by the caller.
	}
	bandLen := len(in) / 2
	if len(lowBand) != bandLen || len(highBand) != bandLen {
		panic("subband: AnalysisQMF band length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}

	halfIn1 := make([]float32, bandLen)
	halfIn2 := make([]float32, bandLen)
	filter1 := make([]float32, bandLen)
	filter2 := make([]float32, bandLen)

	for i, k := 0, 0; i < bandLen; i, k = i+1, k+2 {
		halfIn2[i] = in[k]
		halfIn1[i] = in[k+1]
	}

	allPassQMF(halfIn1, filter1, allPassFilter1(), state.AnalysisState1[:])
	allPassQMF(halfIn2, filter2, allPassFilter2(), state.AnalysisState2[:])

	for i := range bandLen {
		lowBand[i] = (filter1[i] + filter2[i]) * 0.5
		highBand[i] = (filter1[i] - filter2[i]) * 0.5
	}
}

// SynthesisQMF merges low/high bands back into a single 2N-sample frame.
func SynthesisQMF(lowBand, highBand, out []float32, state *QMFState) {
	bandLen := len(lowBand)
	if len(highBand) != bandLen {
		panic("subband: SynthesisQMF band length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}
	if len(out) != 2*bandLen {
		panic("subband: SynthesisQMF output length mismatch") //nolint:forbidigo // Invariant violated by the caller.
	}

	halfIn1 := make([]float32, bandLen)
	halfIn2 := make([]float32, bandLen)
	filter1 := make([]float32, bandLen)
	filter2 := make([]float32, bandLen)

	for i := range bandLen {
		halfIn1[i] = lowBand[i] + highBand[i]
		halfIn2[i] = lowBand[i] - highBand[i]
	}

	allPassQMF(halfIn1, filter1, allPassFilter2(), state.SynthesisState1[:])
	allPassQMF(halfIn2, filter2, allPassFilter1(), state.SynthesisState2[:])

	for i, k := 0, 0; i < bandLen; i++ {
		out[k] = clipS16(filter2[i])
		k++
		out[k] = clipS16(filter1[i])
		k++
	}
}

func clipS16(v float32) float32 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}

	return v
}
