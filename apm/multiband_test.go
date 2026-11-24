// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/pion/apm/aec"
	"github.com/pion/apm/hpf"
	"github.com/pion/apm/internal/resampler"
	"github.com/pion/apm/internal/subband"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessor_SplitBand runs the full pipeline at 32 and 48 kHz and
// verifies the inner pipeline executes without errors.
func TestProcessor_SplitBand(t *testing.T) {
	cfg32 := DefaultConfig(Rate32k, 1)
	cfg32.HPF.Enabled = true
	cfg32.NS = NSConfig{Enabled: true, Level: NSModerate}

	cfg48 := DefaultConfig(Rate48k, 1)
	cfg48.MaxProcessingRate = Rate48k
	cfg48.HPF.Enabled = true
	cfg48.NS = NSConfig{Enabled: true, Level: NSModerate}
	cfg48.AGC = AGCConfig{Enabled: true, FixedDigital: AGCFixedDigital{GainDB: 6}}

	for seed, cfg := range []Config{cfg32, cfg48} {
		proc, err := New(cfg)
		require.NoError(t, err)
		rng := rand.New(rand.NewPCG(uint64(seed+1), uint64(seed+1))) //nolint:gosec // Deterministic test signal.
		for i := range 200 {
			f := NewFrame(cfg.SampleRate, 1)
			for j := range f.Data[0] {
				f.Data[0][j] = float32(rng.NormFloat64() * 0.1)
			}
			require.NoError(t, proc.ProcessStream(f), "rate %d frame %d", cfg.SampleRate, i)
		}
	}
}

// The echo canceller works on all the bands: it removes an echo that is
// mostly above 8 kHz.
func TestProcessor_48kHz_AEC(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.MaxProcessingRate = Rate48k
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	const frames = 600
	const delay = 240
	total := frames * Rate48k.SamplesPerFrame()

	rng := rand.New(rand.NewPCG(3, 3)) //nolint:gosec // Deterministic test signal.
	far := make([]float32, total)
	for i := range far {
		far[i] = float32(rng.NormFloat64() * 0.1)
	}
	mic := make([]float32, total)
	for i := delay; i < total; i++ {
		mic[i] = 0.5 * far[i-delay]
	}
	out := runEchoPath(t, proc, far, mic)

	tailStart := total - int(Rate48k)
	var pm, pr float64
	for i := tailStart; i < total; i++ {
		pm += float64(mic[i]) * float64(mic[i])
		pr += float64(out[i]) * float64(out[i])
	}
	require.Positive(t, pr, "output had zero power")
	assert.GreaterOrEqual(t, 10*math.Log10(pm/pr), 30.0)
}

// runEchoPath feeds far and mic through proc frame by frame at 48 kHz and
// returns the processed capture signal.
func runEchoPath(t *testing.T, proc *Processor, far, mic []float32) []float32 {
	t.Helper()
	spf := Rate48k.SamplesPerFrame()
	out := make([]float32, len(mic))
	for off := 0; off+spf <= len(mic); off += spf {
		ff := NewFrame(Rate48k, 1)
		nf := NewFrame(Rate48k, 1)
		copy(ff.Data[0], far[off:off+spf])
		copy(nf.Data[0], mic[off:off+spf])
		require.NoError(t, proc.ProcessReverseStream(ff))
		require.NoError(t, proc.ProcessStream(nf))
		copy(out[off:], nf.Data[0])
	}

	return out
}

// The gain controller runs on the full band, so it also amplifies a tone
// that sits in the upper bands.
func TestProcessor_AGCAppliesGainToUpperBands(t *testing.T) {
	for _, tc := range []struct {
		rate   SampleRate
		toneHz float64
	}{{Rate32k, 12000}, {Rate48k, 20000}} {
		cfg := DefaultConfig(tc.rate, 1)
		cfg.AGC = AGCConfig{Enabled: true, FixedDigital: AGCFixedDigital{GainDB: 6}}
		proc, err := New(cfg)
		require.NoError(t, err)

		var inEnergy, outEnergy float64
		spf := tc.rate.SamplesPerFrame()
		for i := range 50 {
			frame := NewFrame(tc.rate, 1)
			for j := range frame.Data[0] {
				phase := 2 * math.Pi * tc.toneHz * float64(i*spf+j) / float64(tc.rate)
				frame.Data[0][j] = float32(0.01 * math.Sin(phase))
			}
			in := append([]float32(nil), frame.Data[0]...)
			require.NoError(t, proc.ProcessStream(frame))
			if i < 10 {
				continue
			}
			for j := range in {
				inEnergy += float64(in[j]) * float64(in[j])
				outEnergy += float64(frame.Data[0][j]) * float64(frame.Data[0][j])
			}
		}
		gainDB := 10 * math.Log10(outEnergy/inEnergy)
		assert.InDelta(t, 6.0, gainDB, 1.0, "rate %d", tc.rate)
	}
}

// The high-pass filter runs on the full band with the coefficients of the
// stream rate, before the signal is split into bands.
func TestProcessor_HPFFiltersFullBand(t *testing.T) {
	for _, tc := range []struct {
		rate     SampleRate
		numBands int
	}{{Rate32k, 2}, {Rate48k, 3}} {
		cfg := DefaultConfig(tc.rate, 1)
		cfg.MaxProcessingRate = Rate48k
		cfg.HPF.Enabled = true
		proc, err := New(cfg)
		require.NoError(t, err)

		filter, err := hpf.New(int(tc.rate), 1)
		require.NoError(t, err)
		splitter, err := subband.NewSplittingFilter(1, tc.numBands)
		require.NoError(t, err)
		bands := allocBandBuffers(tc.numBands, 1)

		rng := rand.New(rand.NewPCG(7, 0)) //nolint:gosec // Deterministic test signal.
		for i := range 20 {
			frame := NewFrame(tc.rate, 1)
			want := [][]float32{make([]float32, tc.rate.SamplesPerFrame())}
			for j := range frame.Data[0] {
				frame.Data[0][j] = float32(rng.NormFloat64() * 0.1)
				want[0][j] = frame.Data[0][j] * apmScale
			}
			require.NoError(t, filter.Process(want))
			require.NoError(t, splitter.Analysis(want, bands))
			require.NoError(t, splitter.Synthesis(bands, want))
			scaleDown(want)

			require.NoError(t, proc.ProcessStream(frame))
			require.Equal(t, want[0], frame.Data[0], "rate %d, frame %d", tc.rate, i)
		}
	}
}

// Without a module that works on the bands the signal is not split, so
// it is not delayed or filtered by the filter bank.
func TestProcessor_SkipsBandSplitWhenUnused(t *testing.T) {
	for _, rate := range []SampleRate{Rate32k, Rate48k} {
		cfg := DefaultConfig(rate, 1)
		cfg.AGC = AGCConfig{Enabled: true}
		proc, err := New(cfg)
		require.NoError(t, err)

		rng := rand.New(rand.NewPCG(3, 0)) //nolint:gosec // Deterministic test signal.
		for i := range 5 {
			frame := NewFrame(rate, 1)
			for j := range frame.Data[0] {
				frame.Data[0][j] = float32(rng.NormFloat64() * 0.01)
			}
			want := append([]float32(nil), frame.Data[0]...)

			require.NoError(t, proc.ProcessStream(frame))
			require.Equal(t, want, frame.Data[0], "rate %d, frame %d", rate, i)
		}
	}
}

// The noise suppressor also attenuates stationary content above the
// lowest band: here a steady tone on top of white noise.
func TestProcessor_NSSuppressesUpperBands(t *testing.T) {
	for _, tc := range []struct {
		rate   SampleRate
		toneHz float64
	}{{Rate32k, 12000}, {Rate48k, 20000}} {
		cfg := DefaultConfig(tc.rate, 1)
		cfg.MaxProcessingRate = Rate48k
		cfg.NS = NSConfig{Enabled: true, Level: NSHigh}
		proc, err := New(cfg)
		require.NoError(t, err)

		rng := rand.New(rand.NewPCG(5, 0)) //nolint:gosec // Deterministic test signal.
		var inEnergy, outEnergy float64
		spf := tc.rate.SamplesPerFrame()
		for i := range 200 {
			frame := NewFrame(tc.rate, 1)
			tone := make([]float64, spf)
			for j := range frame.Data[0] {
				tone[j] = 0.01 * math.Sin(2*math.Pi*tc.toneHz*float64(i*spf+j)/float64(tc.rate))
				frame.Data[0][j] = float32(tone[j] + rng.NormFloat64()*0.01)
			}
			require.NoError(t, proc.ProcessStream(frame))
			if i < 100 {
				continue
			}
			// Without upper band suppression the tone alone keeps the
			// output at or above its own energy.
			for j := range tone {
				inEnergy += tone[j] * tone[j]
				outEnergy += float64(frame.Data[0][j]) * float64(frame.Data[0][j])
			}
		}
		gainDB := 10 * math.Log10(outEnergy/inEnergy)
		assert.Less(t, gainDB, -6.0, "rate %d: output energy over tone energy", tc.rate)
	}
}

// Like upstream, a render stream at another rate is resampled to the
// capture rate before it is split into bands for the echo canceller.
func TestProcessor_ResamplesRenderToCaptureRate(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.MaxProcessingRate = Rate48k
	cfg.ReverseSampleRate = Rate16k
	cfg.AEC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	renderResampler, err := resampler.NewPushSincResampler(160, 480)
	require.NoError(t, err)
	renderSplitter, err := subband.NewSplittingFilter(1, 3)
	require.NoError(t, err)
	captureSplitter, err := subband.NewSplittingFilter(1, 3)
	require.NoError(t, err)
	filter, err := hpf.New(48000, 1)
	require.NoError(t, err)
	canceller, err := aec.New(48000, 1, 1)
	require.NoError(t, err)
	post := newPostFilterIfNeeded(48000, 1)
	renderBands, captureBands := allocBandBuffers(3, 1), allocBandBuffers(3, 1)
	renderSplit, captureSplit := splitBandViews(renderBands, 1), splitBandViews(captureBands, 1)

	rng := rand.New(rand.NewPCG(13, 0)) //nolint:gosec // Deterministic test signal.
	for i := range 30 {
		far, near := NewFrame(Rate16k, 1), NewFrame(Rate48k, 1)
		for j := range far.Data[0] {
			far.Data[0][j] = float32(rng.NormFloat64() * 0.1)
		}
		for j := range near.Data[0] {
			near.Data[0][j] = float32(rng.NormFloat64() * 0.1)
		}

		render := [][]float32{make([]float32, 480)}
		require.NoError(t, renderResampler.Resample(far.Data[0], render[0]))
		scaleUp(render)
		require.NoError(t, renderSplitter.Analysis(render, renderBands))
		require.NoError(t, canceller.AnalyzeRender(renderSplit))

		want := [][]float32{append([]float32(nil), near.Data[0]...)}
		scaleUp(want)
		require.NoError(t, filter.Process(want))
		require.NoError(t, canceller.AnalyzeCapture(want))
		require.NoError(t, captureSplitter.Analysis(want, captureBands))
		require.NoError(t, canceller.ProcessCapture(captureSplit, false))
		require.NoError(t, captureSplitter.Synthesis(captureBands, want))
		post.process(want)
		scaleDown(want)

		require.NoError(t, proc.ProcessReverseStream(far))
		require.NoError(t, proc.ProcessStream(near))
		require.Equal(t, want[0], near.Data[0], "frame %d", i)
	}
}

// By default the band modules run at 32 kHz at most, like upstream: a
// 48 kHz stream is resampled to 32 kHz for them and back. Like upstream,
// the high-pass filter then keeps the coefficients of 48 kHz.
func TestProcessor_DefaultCaps48kHzProcessingAt32kHz(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.HPF.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	down, err := resampler.NewPushSincResampler(480, 320)
	require.NoError(t, err)
	up, err := resampler.NewPushSincResampler(320, 480)
	require.NoError(t, err)
	filter, err := hpf.New(48000, 1)
	require.NoError(t, err)
	splitter, err := subband.NewSplittingFilter(1, 2)
	require.NoError(t, err)
	bands := allocBandBuffers(2, 1)

	rng := rand.New(rand.NewPCG(23, 0)) //nolint:gosec // Deterministic test signal.
	for i := range 20 {
		near := NewFrame(Rate48k, 1)
		for j := range near.Data[0] {
			near.Data[0][j] = float32(rng.NormFloat64() * 0.1)
		}

		buffer := [][]float32{make([]float32, 320)}
		require.NoError(t, down.Resample(near.Data[0], buffer[0]))
		scaleUp(buffer)
		require.NoError(t, filter.Process(buffer))
		require.NoError(t, splitter.Analysis(buffer, bands))
		require.NoError(t, splitter.Synthesis(bands, buffer))
		scaleDown(buffer)
		want := make([]float32, 480)
		require.NoError(t, up.Resample(buffer[0], want))

		require.NoError(t, proc.ProcessStream(near))
		require.Equal(t, want, near.Data[0], "frame %d", i)
	}
}

// Without a band module the cap does not apply: the gain controller
// runs at the stream rate.
func TestProcessor_FullBandModulesIgnoreProcessingCap(t *testing.T) {
	cfg := DefaultConfig(Rate48k, 1)
	cfg.AGC.Enabled = true
	proc, err := New(cfg)
	require.NoError(t, err)

	assert.Equal(t, Rate32k, proc.Config().MaxProcessingRate)
	assert.Equal(t, Rate48k, proc.procRate)
}
