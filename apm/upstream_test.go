// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package apm

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upstreamNoise is the generator of the upstream harness: a linear
// congruential generator of samples in [-1, 1).
type upstreamNoise uint32

func (n *upstreamNoise) next() float32 {
	*n = *n*1664525 + 1013904223

	return float32(int32(*n)) * (1.0 / 2147483648.0) //nolint:gosec // The conversion wraps on purpose.
}

// upstreamScenario is the scenario of the upstream harness, on frames of
// 10 ms. Every frame, the render frame is analyzed, then the capture
// frame is processed.
//
// The render signal is noise, independent in every channel, active in the
// first 32 frames of every 40 frames from frame 20 on. The echo is the
// mean of the render channels, delayed by 12.5 ms and scaled by 0.5, plus
// the same delayed by 37 more samples and scaled by 0.25. Every capture
// channel is the echo plus its own noise, which is weak except in frames
// 76 to 83 and 280 to 303. The capture signal saturates in frames 44, 45
// and 330 to 333.
//
// The stream delay is set to 40 ms before every capture frame. The analog
// level is set before every capture frame, to 100 then to 120 from frame
// 250 on. The playout volume is set at frames 300 and 400. The render
// frame is not analyzed in frames 450 to 461.
type upstreamScenario struct {
	proc        *Processor
	noise       upstreamNoise
	far, near   *Frame
	history     []float32
	delay       int
	secondDelay int
	// inputEnergy is the energy of the first capture channel, and
	// outputEnergies the energy of every output channel.
	inputEnergy    float32
	outputEnergies []float32
}

const upstreamScenarioFrames = 700

func newUpstreamScenario(t *testing.T, cfg Config) *upstreamScenario {
	t.Helper()
	proc, err := New(cfg)
	require.NoError(t, err)
	delay := int(cfg.SampleRate) / 80
	secondDelay := delay + 37

	return &upstreamScenario{
		proc:           proc,
		noise:          42,
		far:            NewFrame(cfg.SampleRate, proc.Config().ReverseNumChannels),
		near:           NewFrame(cfg.SampleRate, cfg.NumChannels),
		history:        make([]float32, secondDelay+cfg.SampleRate.SamplesPerFrame()),
		delay:          delay,
		secondDelay:    secondDelay,
		outputEnergies: make([]float32, cfg.NumChannels),
	}
}

func frameEnergy(samples []float32) float32 {
	var energy float32
	for _, sample := range samples {
		energy += float32(sample * sample)
	}

	return energy
}

// generate fills the render and the capture frames of a frame index.
func (s *upstreamScenario) generate(index int) {
	s.generateRender(index)

	nearendAmplitude := float32(0.001)
	if (index >= 76 && index < 84) || (index >= 280 && index < 304) {
		nearendAmplitude = 0.1
	}
	saturated := (index >= 44 && index < 46) || (index >= 330 && index < 334)
	for _, channel := range s.near.Data {
		s.generateCapture(channel, nearendAmplitude, saturated)
	}
	s.inputEnergy = frameEnergy(s.near.Data[0])
}

// generateRender fills the render frame and appends the mean of its
// channels to the history.
func (s *upstreamScenario) generateRender(index int) {
	var renderAmplitude float32
	if index >= 20 && index%40 < 32 {
		renderAmplitude = 0.1
	}
	for _, channel := range s.far.Data {
		for k := range channel {
			channel[k] = renderAmplitude * s.noise.next()
		}
	}
	frameLength := s.far.NumSamples()
	copy(s.history, s.history[frameLength:])
	for k := range frameLength {
		var mean float32
		for _, channel := range s.far.Data {
			mean += channel[k]
		}
		s.history[s.secondDelay+k] = mean / float32(len(s.far.Data))
	}
}

// generateCapture fills a capture channel with the echo and its noise.
// The conversions keep the compiler from fusing the operations.
func (s *upstreamScenario) generateCapture(channel []float32, nearendAmplitude float32, saturated bool) {
	for k := range channel {
		direct := float32(0.5 * s.history[s.secondDelay+k-s.delay])
		reflection := float32(0.25 * s.history[k])
		nearend := float32(nearendAmplitude * s.noise.next())
		sample := direct + reflection
		sample += nearend
		if saturated {
			sample = max(-1, min(1, 20*sample))
		}
		channel[k] = sample
	}
}

// step generates and processes the frames of a frame index.
func (s *upstreamScenario) step(t *testing.T, index int) {
	t.Helper()
	s.generate(index)
	switch index {
	case 300:
		s.proc.SetPlayoutVolume(200)
	case 400:
		s.proc.SetPlayoutVolume(150)
	}
	if index < 450 || index >= 462 {
		require.NoError(t, s.proc.ProcessReverseStream(s.far))
	}
	require.NoError(t, s.proc.SetStreamDelayMS(40))
	if index < 250 {
		s.proc.SetStreamAnalogLevel(100)
	} else {
		s.proc.SetStreamAnalogLevel(120)
	}
	require.NoError(t, s.proc.ProcessStream(s.near))
	for ch, channel := range s.near.Data {
		s.outputEnergies[ch] = frameEnergy(channel)
	}
}

// fusesMultiplyAdd tells whether the Go compiler fuses a multiplication
// and an addition into one operation on this architecture, which changes
// the rounding of the floating-point computations.
func fusesMultiplyAdd() bool {
	switch runtime.GOARCH {
	case "arm64", "ppc64", "ppc64le", "riscv64", "s390x", "loong64":
		return true
	default:
		return false
	}
}

// TestProcessor_MatchesUpstream compares the energy of every output
// frame, in dB, with the upstream AudioProcessing at the pinned commit,
// for a stream with the echo canceller, at 16, 32 and 48 kHz, in mono and
// in stereo. The "ns" scenarios also enable the noise suppressor. The
// upstream stream formats are set before the first frame, and its other
// settings are the defaults.
//
// The output of the echo canceller depends on the rounding of the
// floating-point computations, which feeds back through the adaptation
// of its filters. Upstream itself, with one capture sample of the second
// frame moved to the next float, or built with fused multiply-add, gives
// energies that differ from frame 47 on in some of the scenarios: by up
// to 0.05 dB for the median frame, 0.3 dB for 90% of the frames and
// 1.7 dB for 99% of them. The first 46 frames, which hold the start of the
// echo and a saturation of the capture signal, are therefore compared
// frame by frame, and the whole run is compared in distribution. The
// mono 16 kHz scenario, that the moved sample leaves unchanged upstream,
// is compared frame by frame over the whole run. No frame is compared
// one by one on the architectures where Go fuses multiply-add.
//
// Each line of the reference file is keyed by "sample_rate
// render_channels capture_channels max_processing_rate", plus "ns", and
// holds the frame index, the energy of the first capture channel and the
// energy of every output channel.
func TestProcessor_MatchesUpstream(t *testing.T) {
	const (
		startFrames = 46
		silence     = 1e-12
	)
	reference := map[string][][]float32{}
	for _, entry := range refdata.Load(t, "testdata/upstream.txt") {
		reference[entry.Key] = append(reference[entry.Key], entry.Samples)
	}

	for _, tc := range []struct {
		rate, maxRate         SampleRate
		numRender, numCapture int
		ns                    bool
		// exactFrames is the number of frames compared frame by frame.
		exactFrames int
	}{
		{Rate16k, Rate32k, 1, 1, false, upstreamScenarioFrames},
		{Rate16k, Rate32k, 2, 2, false, startFrames},
		{Rate32k, Rate32k, 1, 1, false, startFrames},
		{Rate32k, Rate32k, 2, 2, false, startFrames},
		{Rate48k, Rate48k, 1, 1, false, startFrames},
		{Rate48k, Rate48k, 2, 2, false, startFrames},
		{Rate48k, Rate32k, 2, 1, false, startFrames},
		{Rate48k, Rate32k, 1, 1, false, startFrames},
		{Rate16k, Rate32k, 1, 2, true, startFrames},
		{Rate48k, Rate48k, 1, 1, true, startFrames},
	} {
		key := fmt.Sprintf("%d %d %d %d", tc.rate, tc.numRender, tc.numCapture, tc.maxRate)
		if tc.ns {
			key += " ns"
		}
		t.Run(key, func(t *testing.T) {
			frames := reference[key]
			require.Len(t, frames, upstreamScenarioFrames)
			cfg := DefaultConfig(tc.rate, tc.numCapture)
			cfg.ReverseNumChannels = tc.numRender
			cfg.MaxProcessingRate = tc.maxRate
			cfg.AEC.Enabled = true
			cfg.NS.Enabled = tc.ns
			scenario := newUpstreamScenario(t, cfg)

			var deviations []float64
			for index, want := range frames {
				scenario.step(t, index)
				require.Equal(t, float32(index), want[0])
				require.InEpsilon(t, want[1], scenario.inputEnergy, 1e-5, "frame %d", index)
				for ch, energy := range scenario.outputEnergies {
					// The first frame of the noise suppressor is silent.
					if energy < silence && want[2+ch] < silence {
						continue
					}
					deviation := math.Abs(10 * math.Log10(float64(energy)/float64(want[2+ch])))
					if index < tc.exactFrames && !fusesMultiplyAdd() {
						require.LessOrEqual(t, deviation, 0.01, "frame %d, channel %d", index, ch)
					}
					deviations = append(deviations, deviation)
				}
			}
			slices.Sort(deviations)
			assert.LessOrEqual(t, deviations[len(deviations)/2], 0.15, "median")
			assert.LessOrEqual(t, deviations[len(deviations)*9/10], 0.8, "90th percentile")
			assert.LessOrEqual(t, deviations[len(deviations)*99/100], 3.5, "99th percentile")
		})
	}
}
