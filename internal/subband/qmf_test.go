// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQMF_RoundTrip_Sine uses a single tone so a single integer alignment
// lag can compensate the filter's group delay; checks the canonical
// "QMF reconstructs cleanly" property.
func TestQMF_RoundTrip_Sine(t *testing.T) {
	var st QMFState
	const N = 320 * 50
	in := make([]float32, N)
	for i := range in {
		in[i] = float32(math.Sin(2*math.Pi*float64(i)*440/16000)) * 5000
	}
	out := make([]float32, N)
	lo := make([]float32, 160)
	hi := make([]float32, 160)
	for off := 0; off < N; off += 320 {
		AnalysisQMF(in[off:off+320], lo, hi, &st)
		SynthesisQMF(lo, hi, out[off:off+320], &st)
	}
	best := alignedSNR(in, out, 32, 320)
	t.Logf("QMF sine round-trip SNR = %.1f dB", best)
	assert.GreaterOrEqual(t, best, 20.0, "QMF sine round-trip SNR in dB")
}

// TestQMF_RoundTrip_EnergyPreserved checks that broadband noise is
// reconstructed with the right overall energy. The QMF has frequency-
// dependent group delay so an integer-lag SNR is lower than the sine
// case; instead we measure the energy ratio.
func TestQMF_RoundTrip_EnergyPreserved(t *testing.T) {
	var st QMFState
	rng := rand.New(rand.NewPCG(2, 2)) //nolint:gosec // Deterministic test signal.
	const blocks = 200
	in := make([]float32, blocks*320)
	for i := range in {
		in[i] = float32(rng.NormFloat64() * 1000)
	}
	out := make([]float32, len(in))
	lo := make([]float32, 160)
	hi := make([]float32, 160)
	for b := range blocks {
		off := b * 320
		AnalysisQMF(in[off:off+320], lo, hi, &st)
		SynthesisQMF(lo, hi, out[off:off+320], &st)
	}
	const skip = 320 * 5
	var inE, outE float64
	for i := skip; i < len(in); i++ {
		inE += float64(in[i]) * float64(in[i])
		outE += float64(out[i]) * float64(out[i])
	}
	ratioDB := 10 * math.Log10(outE/inE)
	t.Logf("QMF noise energy ratio (out/in) = %.2f dB", ratioDB)
	assert.LessOrEqual(t, math.Abs(ratioDB), 3.0, "QMF energy ratio in dB")
}

func alignedSNR(in, out []float32, maxLag, skip int) float64 {
	best := math.Inf(-1)
	for lag := 0; lag <= maxLag; lag++ {
		var sig, err float64
		for i := skip; i < len(in)-lag; i++ {
			s := float64(in[i])
			d := s - float64(out[i+lag])
			sig += s * s
			err += d * d
		}
		if err <= 0 {
			continue
		}
		snr := 10 * math.Log10(sig/err)
		if snr > best {
			best = snr
		}
	}

	return best
}
