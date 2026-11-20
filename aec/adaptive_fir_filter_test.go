// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Port of AdaptiveFirFilterTest.FilterStatisticsAccess.
func TestAdaptiveFirFilter_FilterStatisticsAccess(t *testing.T) {
	filter := newAdaptiveFirFilter(9, 9, 250, 1)
	h2 := make([][kFftLengthBy2Plus1]float32, filter.maxFilterSizePartitions())

	var erl [kFftLengthBy2Plus1]float32
	computeErl(h2, &erl)
	h2 = filter.computeFrequencyResponse(h2)
	assert.Len(t, h2, 9)
}

// Port of AdaptiveFirFilterTest.FilterSize.
func TestAdaptiveFirFilter_FilterSize(t *testing.T) {
	for filterSize := 1; filterSize < 5; filterSize++ {
		filter := newAdaptiveFirFilter(filterSize, filterSize, 250, 1)
		assert.Equal(t, filterSize, filter.sizePartitions())
	}
}

// adaptiveFilterReferenceRelativeDelta bounds the difference to upstream
// relative to the largest value of a vector. The Go FFT rounds differently
// than the Ooura FFT used upstream, and the difference feeds back through
// the adaptation.
const adaptiveFilterReferenceRelativeDelta = 1e-3

func assertNear(t *testing.T, want, got []float32, name string) {
	t.Helper()
	var largest float32
	for _, value := range want {
		largest = max(largest, abs32(value))
	}
	assert.InDeltaSlice(t, want, got, float64(largest)*adaptiveFilterReferenceRelativeDelta, name)
}

// referenceCursor reads the lines of a reference file that end with a
// suffix, in order.
type referenceCursor struct {
	t       *testing.T
	entries []refdata.Entry
}

func newReferenceCursor(t *testing.T, path, suffix string) *referenceCursor {
	t.Helper()
	cursor := &referenceCursor{t: t}
	for _, entry := range refdata.Load(t, path) {
		if name, found := strings.CutSuffix(entry.Key, suffix); found {
			entry.Key = name
			cursor.entries = append(cursor.entries, entry)
		}
	}

	return cursor
}

// next returns the samples of the next line, which must have this name.
func (c *referenceCursor) next(name string) []float32 {
	c.t.Helper()
	require.NotEmpty(c.t, c.entries)
	entry := c.entries[0]
	c.entries = c.entries[1:]
	require.Equal(c.t, name, entry.Key)

	return entry.Samples
}

// filterAndAdaptScenario holds the state of the loop of
// AdaptiveFirFilterMultiChannel.FilterAndAdapt, without the high-pass
// filters and the AecState.
type filterAndAdaptScenario struct {
	numRenderChannels int
	filter            *adaptiveFirFilter
	h2                [][kFftLengthBy2Plus1]float32
	impulseResponse   []float32
	fft               *aec3Fft
	renderDelayBuffer *renderDelayBufferImpl
	gain              *coarseFilterUpdateGain
	random            *testRandom
	render            *block
	analyzer          *renderSignalAnalyzer
	delayBuffers      []*testDelayBuffer

	capture     []float32
	filterError [kBlockSize]float32
	echo        fftData
	gainData    fftData
	renderPower [kFftLengthBy2Plus1]float32
}

func newFilterAndAdaptScenario(numRenderChannels, delaySamples int) *filterAndAdaptScenario {
	const sampleRateHz = 48000
	config := defaultEchoCanceller3Config()
	filter := newAdaptiveFirFilter(config.filter.refined.lengthBlocks, config.filter.refined.lengthBlocks,
		config.filter.configChangeDurationBlocks, numRenderChannels)
	config.delay.defaultDelay = 1
	scenario := &filterAndAdaptScenario{
		numRenderChannels: numRenderChannels,
		filter:            filter,
		h2:                make([][kFftLengthBy2Plus1]float32, filter.maxFilterSizePartitions()),
		impulseResponse:   make([]float32, getTimeDomainLength(filter.maxFilterSizePartitions())),
		fft:               newAec3Fft(),
		renderDelayBuffer: newRenderDelayBufferImpl(config, sampleRateHz, numRenderChannels),
		gain:              newCoarseFilterUpdateGain(config.filter.coarse, config.filter.configChangeDurationBlocks),
		random:            newTestRandom(42),
		render:            newBlock(numBandsForRate(sampleRateHz), numRenderChannels),
		analyzer:          newRenderSignalAnalyzer(config),
		delayBuffers:      make([]*testDelayBuffer, numRenderChannels),
		capture:           make([]float32, kBlockSize),
	}
	for ch := range scenario.delayBuffers {
		scenario.delayBuffers[ch] = newTestDelayBuffer(delaySamples)
	}

	return scenario
}

// generate produces a render block and the capture block that holds its
// delayed echo and some noise.
func (s *filterAndAdaptScenario) generate() {
	channelCapture := make([]float32, kBlockSize)
	noise := make([]float32, kBlockSize)
	clear(s.capture)
	for ch := range s.numRenderChannels {
		randomizeSampleVector(s.random, s.render.view(0, ch))
		s.delayBuffers[ch].delay(s.render.view(0, ch), channelCapture)
		for k := range s.capture {
			s.capture[k] += channelCapture[k] / float32(s.numRenderChannels)
		}
	}
	randomizeSampleVector(s.random, noise)
	noiseScaling := float32(1.0) / 100 / float32(s.numRenderChannels)
	for k := range s.capture {
		s.capture[k] += noise[k] * noiseScaling
	}
}

// step runs one block: it cancels the echo, then adapts the filter.
func (s *filterAndAdaptScenario) step(index int) {
	const scale = 1.0 / float32(kFftLengthBy2)
	s.generate()

	s.renderDelayBuffer.insert(s.render)
	if index == 0 {
		s.renderDelayBuffer.reset()
	}
	s.renderDelayBuffer.prepareCaptureProcessing()
	renderBuffer := s.renderDelayBuffer.getRenderBuffer()
	s.analyzer.update(renderBuffer, 0, true)

	var errorData fftData
	var scratch [kFftLength]float32
	s.filter.filter(renderBuffer, &s.echo)
	s.fft.inv(&s.echo, &scratch)
	for k := range s.filterError {
		s.filterError[k] = min(max(s.capture[k]-scratch[k+kFftLengthBy2]*scale, -32768), 32767)
	}
	s.fft.zeroPaddedFft(&s.filterError, windowRectangular, &errorData)
	renderBuffer.spectralSum(s.filter.sizePartitions(), &s.renderPower)
	s.gain.compute(&s.renderPower, s.analyzer, &errorData, s.filter.sizePartitions(), false, &s.gainData)
	if index%2 == 0 {
		s.filter.adaptUpdateImpulse(renderBuffer, &s.gainData, &s.impulseResponse)
	} else {
		s.filter.adapt(renderBuffer, &s.gainData)
	}
	s.h2 = s.filter.computeFrequencyResponse(s.h2)
}

// TestAdaptiveFirFilter_MatchesUpstream compares the signals and the
// filter with the upstream AdaptiveFirFilter, CoarseFilterUpdateGain and
// RenderSignalAnalyzer at the pinned commit. Each line of the reference
// file is keyed by "name render_channels delay".
func TestAdaptiveFirFilter_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct{ numRenderChannels, delaySamples int }{{1, 150}, {2, 64}} {
		suffix := fmt.Sprintf(" %d %d", tc.numRenderChannels, tc.delaySamples)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/adaptive_fir_filter_upstream.txt", suffix)
			scenario := newFilterAndAdaptScenario(tc.numRenderChannels, tc.delaySamples)

			for index := range 1000 {
				scenario.step(index)
				if index != 20 && index != 999 {
					continue
				}
				require.Equal(t, []float32{float32(index)}, reference.next("step"))
				assertNear(t, reference.next("e"), scenario.filterError[:], "e")
				assert.Equal(t, reference.next("y"), scenario.capture)
				assertNear(t, reference.next("s_re"), scenario.echo.re[:], "s_re")
				assertNear(t, reference.next("s_im"), scenario.echo.im[:], "s_im")
				assertNear(t, reference.next("g_re"), scenario.gainData.re[:], "g_re")
				assertNear(t, reference.next("g_im"), scenario.gainData.im[:], "g_im")
				assertNear(t, reference.next("render_power"), scenario.renderPower[:], "render_power")
			}

			assertNear(t, reference.next("h"), scenario.impulseResponse, "h")
			for p := 0; p < len(scenario.h2); p += 4 {
				assertNear(t, reference.next(fmt.Sprintf("h2_%d", p)), scenario.h2[p][:], "h2")
			}
			part := &scenario.filter.getFilter()[2][tc.numRenderChannels-1]
			assertNear(t, reference.next("filter_re"), part.re[:], "filter_re")
			assertNear(t, reference.next("filter_im"), part.im[:], "filter_im")

			// Like the upstream test, verify that the filter performs well.
			ratio := reference.next("ratio")[0]
			assert.Greater(t, ratio, float32(1000))
			assert.InEpsilon(t, ratio, energy(scenario.capture)/energy(scenario.filterError[:]), 0.05)
		})
	}
}

func TestAdaptiveFirFilter_ScaleAndSetFilter(t *testing.T) {
	filter := newAdaptiveFirFilter(3, 2, 250, 1)
	source := [][]fftData{make([]fftData, 1), make([]fftData, 1), make([]fftData, 1)}
	for p := range source {
		fill(source[p][0].re[:], float32(p+1))
		fill(source[p][0].im[:], float32(-p-1))
	}

	// Only the current partitions are set.
	filter.setFilter(3, source)
	assert.Equal(t, source[1][0], filter.getFilter()[1][0])
	assert.Equal(t, fftData{}, filter.getFilter()[2][0])

	filter.scaleFilter(0.5)
	assert.InDelta(t, 1, filter.getFilter()[1][0].re[0], 0)
	assert.InDelta(t, -0.5, filter.getFilter()[0][0].im[kFftLengthBy2], 0)
}
