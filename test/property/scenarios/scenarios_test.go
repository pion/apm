// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package scenarios

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/pion/apm/apm"
	"github.com/pion/apm/test/metrics"
	"github.com/pion/apm/test/property"
	"github.com/pion/apm/test/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rate            = 16000
	samplesPerFrame = 160
)

func newRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // Deterministic test signal.
}

func newProcessor(t *testing.T, cfg apm.Config) *apm.Processor {
	t.Helper()
	proc, err := apm.New(cfg)
	require.NoError(t, err)

	return proc
}

// process runs in through a 16 kHz mono pipeline and returns the output.
func process(t *testing.T, cfg apm.Config, in []float32) []float32 {
	t.Helper()
	pipe := property.NewPipeline(newProcessor(t, cfg), apm.Rate16k)
	out := make([]float32, len(in))
	_, err := pipe.ProcessStream(in, out)
	require.NoError(t, err)

	return out
}

// runEcho interleaves far and mic per frame through an AEC-enabled
// processor: the AEC needs render and capture to arrive in time order.
func runEcho(t *testing.T, far, mic []float32) []float32 {
	t.Helper()
	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.AEC.Enabled = true
	proc := newProcessor(t, cfg)

	out := make([]float32, len(mic))
	farFrame := apm.NewFrame(apm.Rate16k, 1)
	nearFrame := apm.NewFrame(apm.Rate16k, 1)
	for off := 0; off+samplesPerFrame <= len(mic); off += samplesPerFrame {
		copy(farFrame.Data[0], far[off:off+samplesPerFrame])
		copy(nearFrame.Data[0], mic[off:off+samplesPerFrame])
		require.NoError(t, proc.ProcessReverseStream(farFrame))
		require.NoError(t, proc.ProcessStream(nearFrame))
		copy(out[off:], nearFrame.Data[0])
	}

	return out
}

// echoPath returns far-end speech-shaped noise and its echo through an
// exponentially decaying room response.
func echoPath(dur int, rng *rand.Rand) (far, echo []float32) {
	far = synth.SpeechShapedNoise(dur, rate, rng)
	ir := synth.ExpDecayIR(160, rate, 0.15, rng)

	return far, synth.Convolve(far, ir)[:dur]
}

func TestAEC_ERLE(t *testing.T) {
	bounds := property.MustBounds(t, "aec_erle")

	const dur = rate * 6 // 6 s: long enough for the adaptive filter to converge
	far, echo := echoPath(dur, newRNG(1))
	mic := append([]float32(nil), echo...)
	out := runEcho(t, far, mic)

	erle := metrics.MaxConvergedERLE(mic, out, 160)
	t.Logf("AEC max-converged ERLE = %.2f dB", erle)
	if bounds.MinERLE != nil {
		assert.GreaterOrEqual(t, erle, *bounds.MinERLE, "ERLE dB")
	}
}

func TestAEC_DoubleTalk(t *testing.T) {
	bounds := property.MustBounds(t, "aec_doubletalk")

	const dur = rate * 6
	rng := newRNG(2)
	far, echo := echoPath(dur, rng)

	near := make([]float32, dur)
	mid0, mid1 := dur/4, 3*dur/4
	for i := mid0; i < mid1; i++ {
		near[i] = float32(rng.NormFloat64()) * 0.1
	}
	mic := make([]float32, dur)
	for i := range mic {
		mic[i] = echo[i] + near[i]
	}
	out := runEcho(t, far, mic)

	// Measure single-talk ERLE (first quarter: pre double-talk region).
	erle := metrics.ERLE(mic[:mid0], out[:mid0], 160, -50)
	t.Logf("single-talk ERLE = %.2f dB", erle)
	if bounds.MinERLE != nil {
		assert.GreaterOrEqual(t, erle, *bounds.MinERLE, "single-talk ERLE dB")
	}
}

const nsSkip = "synthetic speech-like fixture is too stationary for the upstream speech/noise discriminator; revisit with a real corpus"

func TestNS_SNRGain(t *testing.T) {
	t.Skip(nsSkip)
	bounds := property.MustBounds(t, "ns_snr_gain")

	const dur = rate * 3
	clean := speechLike(dur)
	noise := synth.SpeechShapedNoise(dur, rate, newRNG(3))
	noisy, err := synth.MixAtSNR(clean, noise, 5.0)
	require.NoError(t, err)

	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.NS = apm.NSConfig{Enabled: true, Level: apm.NSHigh}
	out := process(t, cfg, noisy)

	// Score on steady-state region after NS has learned the noise model.
	const warmup = rate
	inSNR := metrics.SNR(clean[warmup:], noisy[warmup:])
	outSNR := metrics.SNR(clean[warmup:], out[warmup:])
	gain := outSNR - inSNR
	t.Logf("input SNR = %.1f dB, output SNR = %.1f dB, gain = %.1f dB", inSNR, outSNR, gain)
	if bounds.MinSNRGain != nil {
		assert.GreaterOrEqual(t, gain, *bounds.MinSNRGain, "NS SNR gain dB")
	}
}

// speechLike approximates a speech envelope: three harmonics (200/400/600 Hz)
// gated by a 4 Hz amplitude modulation. Used as a stand-in for clean speech
// in tests when no real corpus is available.
func speechLike(n int) []float32 {
	out := make([]float32, n)
	for i := range n {
		sec := float64(i) / float64(rate)
		env := 0.5 + 0.5*sinTau(4*sec)
		carrier := sinTau(200*sec) + 0.7*sinTau(400*sec) + 0.4*sinTau(600*sec)
		out[i] = float32(env * carrier * 0.3)
	}

	return out
}

func sinTau(x float64) float64 {
	return math.Sin(2 * math.Pi * x)
}

func TestNS_SpeechDistortion(t *testing.T) {
	t.Skip(nsSkip)
	bounds := property.MustBounds(t, "ns_speech_distortion")

	const dur = rate * 3
	clean := speechLike(dur)

	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.NS = apm.NSConfig{Enabled: true, Level: apm.NSModerate}
	out := process(t, cfg, clean)

	// Steady-state region only.
	const warmup = rate
	lsd := metrics.LogSpectralDistance(clean[warmup:], out[warmup:], rate, 512, 256)
	cd := metrics.CepstralDistance(clean[warmup:], out[warmup:], rate, 512, 256, 13)
	t.Logf("LSD = %.2f dB, CD = %.2f dB", lsd, cd)
	if bounds.MaxLSD != nil {
		assert.LessOrEqual(t, lsd, *bounds.MaxLSD, "LSD dB")
	}
	if bounds.MaxCepstralCD != nil {
		assert.LessOrEqual(t, cd, *bounds.MaxCepstralCD, "CD dB")
	}
}

func TestAGC_TargetLevel(t *testing.T) {
	t.Skip("AGC target level control is not ported yet")
	bounds := property.MustBounds(t, "agc_target_level")

	const dur = rate * 2
	step := rate // step at 1 s
	in := make([]float32, dur)
	// Quiet first half (-30 dBFS), louder second half (-10 dBFS).
	quiet := synth.Sine(step, rate, 500, 0.0316) // -30 dBFS peak
	loud := synth.Sine(dur-step, rate, 500, 0.316)
	copy(in, quiet)
	copy(in[step:], loud)

	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.AGC = apm.AGCConfig{Enabled: true, TargetLevelDBFS: -10, CompressionGain: 9}
	out := process(t, cfg, in)

	at := metrics.AttackTime(out, rate, step, 160, 3, 1.0)
	if bounds.MaxAttackS != nil {
		assert.LessOrEqual(t, at, *bounds.MaxAttackS, "AGC attack s")
	}
}

func TestAGC_NoOvershoot(t *testing.T) {
	bounds := property.MustBounds(t, "agc_no_overshoot")

	const dur = rate * 2
	in := synth.WhiteNoise(dur, newRNG(5))
	synth.ScaleToDBFS(in, 0) // drive AGC hard

	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.AGC = apm.AGCConfig{Enabled: true, TargetLevelDBFS: -3, CompressionGain: 12}
	out := process(t, cfg, in)

	peak := metrics.PeakLevelDBFS(out)
	if bounds.MaxPeakDBFS != nil {
		assert.LessOrEqual(t, peak, *bounds.MaxPeakDBFS, "AGC output peak dBFS")
	}
}

// hpfAttenuation returns the HPF level change in dB for a tone at freq.
func hpfAttenuation(t *testing.T, freq float64) float64 {
	t.Helper()
	tone := synth.Sine(rate, rate, freq, 0.5)
	cfg := apm.DefaultConfig(apm.Rate16k, 1)
	cfg.HPF = apm.HPFConfig{Enabled: true}
	out := process(t, cfg, tone)

	return synth.LevelDBFS(out) - synth.LevelDBFS(tone)
}

func TestHPF_Corner(t *testing.T) {
	bounds := property.MustBounds(t, "hpf_corner")
	atten := hpfAttenuation(t, 80) // at corner
	if bounds.MinCornerDB != nil {
		assert.LessOrEqual(t, atten, *bounds.MinCornerDB, "HPF attenuation at corner dB")
	}
}

func TestHPF_Stopband(t *testing.T) {
	bounds := property.MustBounds(t, "hpf_stopband")
	atten := hpfAttenuation(t, 10) // well below corner
	if bounds.MinCornerDB != nil {
		assert.LessOrEqual(t, atten, *bounds.MinCornerDB, "HPF stopband attenuation dB")
	}
}

// TestHarness_PassthroughSmoke verifies the framework end to end (synth,
// pipeline, metric) with all modules disabled.
func TestHarness_PassthroughSmoke(t *testing.T) {
	const dur = rate
	in := synth.WhiteNoise(dur, newRNG(99))

	pipe := property.NewPipeline(newProcessor(t, apm.DefaultConfig(apm.Rate16k, 1)), apm.Rate16k)
	out := make([]float32, dur)
	n, err := pipe.ProcessStream(in, out)
	require.NoError(t, err)
	require.Equal(t, dur, n)
	// Passthrough keeps input equal to output, so SNR should be very high.
	assert.GreaterOrEqual(t, metrics.SNR(in, out), 60.0, "passthrough SNR dB")
}
