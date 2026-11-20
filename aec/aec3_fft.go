// SPDX-FileCopyrightText: 2016-2022 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import "github.com/pion/apm/internal/dsp/fft"

// Aec3 uses a 128-point real FFT. We build a single plan once per
// canceller and reuse it across forward/inverse calls.

type windowKind int

const (
	windowRectangular windowKind = iota
	windowHanning
	windowSqrtHanning
)

//nolint:gochecknoglobals // Constant windows from upstream.
var (
	// Hanning window of length kFftLengthBy2 (64), used by ZeroPaddedFft.
	// Values copied verbatim from upstream aec3_fft.cc kHanning64.
	kHanning64 = [kFftLengthBy2]float32{
		0, 0.00248461, 0.00991376, 0.0222136, 0.03926189,
		0.06088921, 0.08688061, 0.11697778, 0.15088159, 0.1882551,
		0.22872687, 0.27189467, 0.31732949, 0.36457977, 0.41317591,
		0.46263495, 0.51246535, 0.56217185, 0.61126047, 0.65924333,
		0.70564355, 0.75, 0.79187184, 0.83084292, 0.86652594,
		0.89856625, 0.92664544, 0.95048443, 0.96984631, 0.98453864,
		0.99441541, 0.99937846, 0.99937846, 0.99441541, 0.98453864,
		0.96984631, 0.95048443, 0.92664544, 0.89856625, 0.86652594,
		0.83084292, 0.79187184, 0.75, 0.70564355, 0.65924333,
		0.61126047, 0.56217185, 0.51246535, 0.46263495, 0.41317591,
		0.36457977, 0.31732949, 0.27189467, 0.22872687, 0.1882551,
		0.15088159, 0.11697778, 0.08688061, 0.06088921, 0.03926189,
		0.0222136, 0.00991376, 0.00248461, 0,
	}

	// SqrtHanning window of length kFftLength (128), used by PaddedFft.
	// Verbatim from upstream aec3_fft.cc kSqrtHanning128.
	kSqrtHanning128 = [kFftLength]float32{
		0.00000000000000, 0.02454122852291, 0.04906767432742, 0.07356456359967,
		0.09801714032956, 0.12241067519922, 0.14673047445536, 0.17096188876030,
		0.19509032201613, 0.21910124015687, 0.24298017990326, 0.26671275747490,
		0.29028467725446, 0.31368174039889, 0.33688985339222, 0.35989503653499,
		0.38268343236509, 0.40524131400499, 0.42755509343028, 0.44961132965461,
		0.47139673682600, 0.49289819222978, 0.51410274419322, 0.53499761988710,
		0.55557023301960, 0.57580819141785, 0.59569930449243, 0.61523159058063,
		0.63439328416365, 0.65317284295378, 0.67155895484702, 0.68954054473707,
		0.70710678118655, 0.72424708295147, 0.74095112535496, 0.75720884650648,
		0.77301045336274, 0.78834642762661, 0.80320753148064, 0.81758481315158,
		0.83146961230255, 0.84485356524971, 0.85772861000027, 0.87008699110871,
		0.88192126434835, 0.89322430119552, 0.90398929312344, 0.91420975570353,
		0.92387953251129, 0.93299279883474, 0.94154406518302, 0.94952818059304,
		0.95694033573221, 0.96377606579544, 0.97003125319454, 0.97570213003853,
		0.98078528040323, 0.98527764238894, 0.98917650996478, 0.99247953459871,
		0.99518472667220, 0.99729045667869, 0.99879545620517, 0.99969881869620,
		1.00000000000000, 0.99969881869620, 0.99879545620517, 0.99729045667869,
		0.99518472667220, 0.99247953459871, 0.98917650996478, 0.98527764238894,
		0.98078528040323, 0.97570213003853, 0.97003125319454, 0.96377606579544,
		0.95694033573221, 0.94952818059304, 0.94154406518302, 0.93299279883474,
		0.92387953251129, 0.91420975570353, 0.90398929312344, 0.89322430119552,
		0.88192126434835, 0.87008699110871, 0.85772861000027, 0.84485356524971,
		0.83146961230255, 0.81758481315158, 0.80320753148064, 0.78834642762661,
		0.77301045336274, 0.75720884650648, 0.74095112535496, 0.72424708295147,
		0.70710678118655, 0.68954054473707, 0.67155895484702, 0.65317284295378,
		0.63439328416365, 0.61523159058063, 0.59569930449243, 0.57580819141785,
		0.55557023301960, 0.53499761988710, 0.51410274419322, 0.49289819222978,
		0.47139673682600, 0.44961132965461, 0.42755509343028, 0.40524131400499,
		0.38268343236509, 0.35989503653499, 0.33688985339222, 0.31368174039889,
		0.29028467725446, 0.26671275747490, 0.24298017990326, 0.21910124015687,
		0.19509032201613, 0.17096188876030, 0.14673047445536, 0.12241067519922,
		0.09801714032956, 0.07356456359967, 0.04906767432742, 0.02454122852291,
	}
)

// aec3Fft wraps a dsp/fft plan and exposes the upstream Aec3Fft API.
type aec3Fft struct {
	plan *fft.Plan
}

func newAec3Fft() *aec3Fft {
	return &aec3Fft{plan: fft.New(kFftLength)}
}

// fwd computes the forward FFT of a 128-sample time array and unpacks
// into a fftData. Mirrors Aec3Fft::Fft.
func (a *aec3Fft) fwd(x *[kFftLength]float32, out *fftData) {
	a.plan.Forward(x[:], out.re[:], out.im[:])
	out.im[0] = 0
	out.im[kFftLengthBy2] = 0
}

// inv computes the inverse FFT into a 128-sample time array. Mirrors
// Aec3Fft::Ifft which leaves the result unnormalized (Ooura convention,
// equal to (N/2)*true_inverse). Callers downstream typically multiply by
// 1/kFftLengthBy2 (= 2/N) to recover the true inverse: preserving that
// convention lets upstream code translate verbatim.
func (a *aec3Fft) inv(in *fftData, x *[kFftLength]float32) {
	a.plan.Inverse(in.re[:], in.im[:], x[:])
	const scale = float32(kFftLengthBy2)
	for i := range x {
		x[i] *= scale
	}
}

// zeroPaddedFft puts a 64-sample x at the end of a 128-sample buffer,
// applies the chosen window, and FFTs. Mirrors Aec3Fft::ZeroPaddedFft.
func (a *aec3Fft) zeroPaddedFft(x *[kFftLengthBy2]float32, w windowKind, out *fftData) {
	var buf [kFftLength]float32
	switch w {
	case windowRectangular:
		copy(buf[kFftLengthBy2:], x[:])
	case windowHanning:
		for i := range kFftLengthBy2 {
			buf[kFftLengthBy2+i] = x[i] * kHanning64[i]
		}
	default:
		panic("aec3_fft: ZeroPaddedFft sqrt-hanning not supported") //nolint:forbidigo // Unsupported window is a programming error.
	}
	a.fwd(&buf, out)
}

// paddedFft concatenates x_old (64) and x (64) into a 128-sample buffer
// (optionally windowed by sqrt-Hanning) and FFTs. Mirrors Aec3Fft::PaddedFft.
func (a *aec3Fft) paddedFft(x, xOld *[kFftLengthBy2]float32, w windowKind, out *fftData) {
	var buf [kFftLength]float32
	switch w {
	case windowRectangular:
		copy(buf[:kFftLengthBy2], xOld[:])
		copy(buf[kFftLengthBy2:], x[:])
	case windowSqrtHanning:
		for i := range kFftLengthBy2 {
			buf[i] = xOld[i] * kSqrtHanning128[i]
		}
		for i := range kFftLengthBy2 {
			buf[kFftLengthBy2+i] = x[i] * kSqrtHanning128[kFftLengthBy2+i]
		}
	default:
		panic("aec3_fft: PaddedFft hanning not supported") //nolint:forbidigo // Unsupported window is a programming error.
	}
	a.fwd(&buf, out)
}
