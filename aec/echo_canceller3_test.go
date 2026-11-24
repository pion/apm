// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

import (
	"fmt"
	"math"
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const echoCanceller3TestFrames = 20

// populateInputBands ports PopulateInputFrame for a frame that is split
// in bands: linearly increasing sample values for each band, with an
// offset per band, to allow a simple bit exactness verification.
func populateInputBands(frameIndex int, bands [][]float32, offset int) {
	for band, samples := range bands {
		for i := range samples {
			value := frameIndex*len(samples) + i + offset
			samples[i] = 0
			if value > 0 {
				samples[i] = float32(5000*band + value)
			}
		}
	}
}

// captureTransportVerificationProcessor ports
// CaptureTransportVerificationProcessor: it leaves the capture data as it
// is.
type captureTransportVerificationProcessor struct{}

func (captureTransportVerificationProcessor) processCapture(bool, bool, *block, *block) {}
func (captureTransportVerificationProcessor) bufferRender(*block)                       {}
func (captureTransportVerificationProcessor) getMetrics() (float64, float64, int)       { return 0, 0, 0 }
func (captureTransportVerificationProcessor) setAudioBufferDelay(int)                   {}
func (captureTransportVerificationProcessor) setCaptureOutputUsage(bool)                {}

// renderTransportVerificationProcessor ports
// RenderTransportVerificationProcessor: it replaces the capture data by
// the render data that it received.
type renderTransportVerificationProcessor struct {
	captureTransportVerificationProcessor
	receivedRenderBlocks []*block
}

func (r *renderTransportVerificationProcessor) processCapture(_, _ bool, _, captureBlock *block) {
	renderBlock := r.receivedRenderBlocks[0]
	r.receivedRenderBlocks = r.receivedRenderBlocks[1:]
	captureBlock.swap(renderBlock)
}

func (r *renderTransportVerificationProcessor) bufferRender(renderBlock *block) {
	received := newBlock(renderBlock.numBands, renderBlock.numChannels)
	copy(received.data, renderBlock.data)
	r.receivedRenderBlocks = append(r.receivedRenderBlocks, received)
}

// echoCanceller3Tester ports EchoCanceller3Tester: a mono echo canceller
// with a capture and a render frame.
type echoCanceller3Tester struct {
	canceller *echoCanceller3
	capture   [][][]float32
	render    [][][]float32
}

func newEchoCanceller3Tester(t *testing.T, sampleRateHz int, processor blockProcessing) *echoCanceller3Tester {
	t.Helper()
	canceller, err := newEchoCanceller3(defaultEchoCanceller3Config(), nil, sampleRateHz, 1, 1)
	require.NoError(t, err)
	canceller.blockProcessor = processor
	numBands := numBandsForRate(sampleRateHz)

	return &echoCanceller3Tester{
		canceller: canceller,
		capture:   newSplitBandsFrame(1, numBands),
		render:    newSplitBandsFrame(1, numBands),
	}
}

// assertDelayedBy64 ports VerifyOutputFrameBitexactness for two signals:
// the output is the reference, delayed by one block.
func assertDelayedBy64(t *testing.T, reference, output []float32) {
	t.Helper()
	for k := kBlockSize; k < len(output); k++ {
		require.Equal(t, reference[k-kBlockSize], output[k], "sample %d", k) //nolint:testifylint // Exact upstream check.
	}
}

// Port of EchoCanceller3Buffering.CaptureBitexactness: the capture data
// is properly received by the block processor and the processor data is
// properly passed to the output.
func TestEchoCanceller3_CaptureBitexactness(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		tester := newEchoCanceller3Tester(t, rate, captureTransportVerificationProcessor{})
		reference := newSplitBandsFrame(1, numBandsForRate(rate))

		for frameIndex := range echoCanceller3TestFrames {
			populateInputBands(frameIndex, tester.capture[0], 0)
			populateInputBands(frameIndex, tester.render[0], 0)
			tester.canceller.analyzeRender(tester.render)
			tester.canceller.processCapture(tester.capture, nil, false)

			populateInputBands(frameIndex, reference[0], -kBlockSize)
			require.Equal(t, reference, tester.capture, "rate %d, frame %d", rate, frameIndex)
		}
	}
}

// Port of EchoCanceller3Buffering.RenderBitexactness: the render data is
// properly received by the block processor.
func TestEchoCanceller3_RenderBitexactness(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		tester := newEchoCanceller3Tester(t, rate, &renderTransportVerificationProcessor{})
		var renderInput, captureOutput []float32

		for frameIndex := range echoCanceller3TestFrames {
			populateInputBands(frameIndex, tester.capture[0], 100)
			populateInputBands(frameIndex, tester.render[0], 0)
			renderInput = append(renderInput, tester.render[0][0]...)
			tester.canceller.analyzeRender(tester.render)
			tester.canceller.processCapture(tester.capture, nil, false)
			captureOutput = append(captureOutput, tester.capture[0][0]...)
		}
		assertDelayedBy64(t, renderInput, captureOutput)
	}
}

// Port of EchoCanceller3Buffering.RenderSwapQueue: the render queue
// handles jitter in the capture and the render calls.
func TestEchoCanceller3_RenderSwapQueue(t *testing.T) {
	tester := newEchoCanceller3Tester(t, 16000, &renderTransportVerificationProcessor{})
	var renderInput, captureOutput []float32

	for frameIndex := range kRenderTransferQueueSizeFrames {
		populateInputBands(frameIndex, tester.render[0], 0)
		renderInput = append(renderInput, tester.render[0][0]...)
		tester.canceller.analyzeRender(tester.render)
	}
	for frameIndex := range kRenderTransferQueueSizeFrames {
		populateInputBands(frameIndex, tester.capture[0], 0)
		tester.canceller.processCapture(tester.capture, nil, false)
		captureOutput = append(captureOutput, tester.capture[0][0]...)
	}
	assertDelayedBy64(t, renderInput, captureOutput)
}

// Port of EchoCanceller3Buffering.RenderSwapQueueOverrunReturnValue: an
// overrun of the render queue is handled.
func TestEchoCanceller3_RenderSwapQueueOverrun(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		canceller, err := newEchoCanceller3(defaultEchoCanceller3Config(), nil, rate, 1, 1)
		require.NoError(t, err)
		render := newSplitBandsFrame(1, numBandsForRate(rate))
		assert.NotPanics(t, func() {
			for range 2 {
				for frameIndex := range 30 {
					populateInputBands(frameIndex, render[0], 0)
					canceller.analyzeRender(render)
				}
			}
		})
	}
}

const (
	stereoTestBlocksForMonoConfig     = 1
	stereoTestBlocksForSurroundConfig = 2
)

// stereoDetectionTest holds an echo canceller with a mono and a
// multichannel config that differ in the length of the coarse filter,
// like the upstream stereo detection tests.
type stereoDetectionTest struct {
	canceller *echoCanceller3
	frame     [][][]float32
}

func newStereoDetectionTest(t *testing.T, monoConfig echoCanceller3Config, numChannels int) *stereoDetectionTest {
	t.Helper()
	multichannelConfig := monoConfig
	monoConfig.filter.coarseInitial.lengthBlocks = stereoTestBlocksForMonoConfig
	multichannelConfig.filter.coarseInitial.lengthBlocks = stereoTestBlocksForSurroundConfig
	canceller, err := newEchoCanceller3(monoConfig, &multichannelConfig, 16000, numChannels, numChannels)
	require.NoError(t, err)

	return &stereoDetectionTest{canceller: canceller, frame: newSplitBandsFrame(numChannels, 1)}
}

// run ports RunAecInStereo and RunAecInSMono: it processes a frame with
// the given value in each channel.
func (s *stereoDetectionTest) run(values ...float32) {
	for ch, value := range values {
		fill(s.frame[ch][0], value)
	}
	s.canceller.analyzeRender(s.frame)
	fullBand := make([][]float32, len(s.frame))
	for ch := range fullBand {
		fullBand[ch] = s.frame[ch][0]
	}
	s.canceller.analyzeCapture(fullBand)
	s.canceller.processCapture(s.frame, nil, false)
}

// assertStereo checks whether the stereo render processing is active,
// and that the matching config is.
func (s *stereoDetectionTest) assertStereo(t *testing.T, stereo bool) {
	t.Helper()
	wantBlocks := stereoTestBlocksForMonoConfig
	if stereo {
		wantBlocks = stereoTestBlocksForSurroundConfig
	}
	assert.Equal(t, stereo, s.canceller.multichannelContentDetector.isProperMultiChannelContentDetected())
	assert.Equal(t, wantBlocks, s.canceller.configSelector.activeConfig().filter.coarseInitial.lengthBlocks)
}

// recordingProcessor stands for the StrictMock block processor of the
// messaging tests: it records the flags of every capture call and counts
// the render calls.
type recordingProcessor struct {
	captureTransportVerificationProcessor
	echoPathGainChanges, saturations []bool
	renderCalls                      int
}

func (r *recordingProcessor) processCapture(echoPathGainChange, captureSignalSaturation bool, _, _ *block) {
	r.echoPathGainChanges = append(r.echoPathGainChanges, echoPathGainChange)
	r.saturations = append(r.saturations, captureSignalSaturation)
}

func (r *recordingProcessor) bufferRender(*block) { r.renderCalls++ }

// expectedFlags returns numBlocks flags, the first numSet of which are set.
func expectedFlags(numBlocks, numSet int) []bool {
	flags := make([]bool, numBlocks)
	for i := range numSet {
		flags[i] = true
	}

	return flags
}

const (
	messagingBlocksPerFrame = splitBandSize / kBlockSize
	messagingBlocks         = echoCanceller3TestFrames * splitBandSize / kBlockSize
)

// Port of EchoCanceller3Messaging.CaptureSaturation: saturation is passed
// to the block processor for the blocks of the saturated frame only.
func TestEchoCanceller3_CaptureSaturation(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		for _, tc := range []struct {
			name      string
			saturated float32
			numSet    int
		}{
			{"none", 0, 0},
			{"one negative", -32768, messagingBlocksPerFrame},
			{"one positive", 32767, messagingBlocksPerFrame},
		} {
			processor := &recordingProcessor{}
			tester := newEchoCanceller3Tester(t, rate, processor)
			fullBand := [][]float32{make([]float32, rate/100)}

			for frameIndex := range echoCanceller3TestFrames {
				clear(fullBand[0])
				if frameIndex == 0 {
					fullBand[0][10] = tc.saturated
				}
				tester.canceller.analyzeCapture(fullBand)
				populateInputBands(frameIndex, tester.capture[0], 0)
				populateInputBands(frameIndex, tester.render[0], 0)
				tester.canceller.analyzeRender(tester.render)
				tester.canceller.processCapture(tester.capture, nil, false)
			}

			name := fmt.Sprintf("rate %d, %s", rate, tc.name)
			assert.Equal(t, messagingBlocks, processor.renderCalls, name)
			assert.Equal(t, expectedFlags(messagingBlocks, tc.numSet), processor.saturations, name)
		}
	}
}

// Port of EchoCanceller3Messaging.EchoPathChange: echo path changes are
// passed to the block processor for the blocks of the flagged frames.
func TestEchoCanceller3_EchoPathChange(t *testing.T) {
	for _, rate := range []int{16000, 32000, 48000} {
		for _, tc := range []struct {
			name   string
			change func(frameIndex int) bool
			numSet int
		}{
			{"none", func(int) bool { return false }, 0},
			{"one sticky", func(int) bool { return true }, messagingBlocks},
			{"one non-sticky", func(frameIndex int) bool { return frameIndex == 0 }, messagingBlocksPerFrame},
		} {
			processor := &recordingProcessor{}
			tester := newEchoCanceller3Tester(t, rate, processor)

			for frameIndex := range echoCanceller3TestFrames {
				populateInputBands(frameIndex, tester.capture[0], 0)
				populateInputBands(frameIndex, tester.render[0], 0)
				tester.canceller.analyzeRender(tester.render)
				tester.canceller.processCapture(tester.capture, nil, tc.change(frameIndex))
			}

			name := fmt.Sprintf("rate %d, %s", rate, tc.name)
			assert.Equal(t, messagingBlocks, processor.renderCalls, name)
			assert.Equal(t, expectedFlags(messagingBlocks, tc.numSet), processor.echoPathGainChanges, name)
		}
	}
}

// Port of EchoCanceller3.DetectionOfProperStereo.
func TestEchoCanceller3_DetectionOfProperStereo(t *testing.T) {
	config := defaultEchoCanceller3Config()
	config.multiChannel.detectStereoContent = true
	config.multiChannel.stereoDetectionThreshold = 0
	config.multiChannel.stereoDetectionHysteresisSeconds = 0
	test := newStereoDetectionTest(t, config, 2)
	test.assertStereo(t, false)

	test.run(100, 100)
	test.assertStereo(t, false)

	test.run(100, 101)
	test.assertStereo(t, true)
}

// Port of EchoCanceller3.DetectionOfProperStereoUsingThreshold.
func TestEchoCanceller3_DetectionOfProperStereoUsingThreshold(t *testing.T) {
	const stereoDetectionThreshold = 2.0
	config := defaultEchoCanceller3Config()
	config.multiChannel.detectStereoContent = true
	config.multiChannel.stereoDetectionThreshold = stereoDetectionThreshold
	config.multiChannel.stereoDetectionHysteresisSeconds = 0
	test := newStereoDetectionTest(t, config, 2)
	test.assertStereo(t, false)

	test.run(100, 100+stereoDetectionThreshold-1)
	test.assertStereo(t, false)

	test.run(100, 100+stereoDetectionThreshold+10)
	test.assertStereo(t, true)
}

// Port of EchoCanceller3.DetectionOfProperStereoUsingHysteresis.
func TestEchoCanceller3_DetectionOfProperStereoUsingHysteresis(t *testing.T) {
	config := defaultEchoCanceller3Config()
	config.multiChannel.detectStereoContent = true
	config.multiChannel.stereoDetectionHysteresisSeconds = 0.5
	test := newStereoDetectionTest(t, config, 2)
	test.assertStereo(t, false)

	test.run(100, 100)
	test.assertStereo(t, false)

	for range int(kNumFramesPerSecond * config.multiChannel.stereoDetectionHysteresisSeconds) {
		test.run(100, 101)
		test.assertStereo(t, false)
	}

	test.run(100, 101)
	test.assertStereo(t, true)
}

// Port of EchoCanceller3.StereoContentDetectionForMonoSignals.
func TestEchoCanceller3_StereoContentDetectionForMonoSignals(t *testing.T) {
	for _, detectStereoContent := range []bool{false, true} {
		config := defaultEchoCanceller3Config()
		config.multiChannel.detectStereoContent = detectStereoContent
		test := newStereoDetectionTest(t, config, 1)
		test.assertStereo(t, false)

		test.run(100)
		test.assertStereo(t, false)
	}
}

const (
	// echoCanceller3ModeStereo has a hysteresis of the stereo detection
	// of 0.2 seconds.
	echoCanceller3ModeStereo = "stereo"
	// echoCanceller3ModeCustom filters the echo reference with a
	// high-pass filter, delays the capture by 100 samples, exports the
	// output of the linear filter and has a multichannel config with a
	// shorter filter.
	echoCanceller3ModeCustom = "custom"

	echoCanceller3ScenarioFrames = 600
)

// echoCanceller3Scenario holds the state of the upstream reference
// scenario of the echo canceller, on frames of 10 ms. Every frame, the
// render frame is analyzed, then the capture frame is analyzed and
// processed.
//
// The render signal is noise in every band, active in the first 32
// frames of every 40 frames from frame 40 on. Its channels hold the same
// signal until frame 200, then different ones. The capture signal is the
// delayed render signal of the same band, downmixed and scaled by 0.6,
// plus weak noise, and a strong nearend signal in frames 280 to 303 and
// in the last 8 frames of the frames 80 to 119, 240 to 279 and 400 to
// 439. It saturates in frames 330 to 333. The level change flag is set
// in frames 250 to 259. The render frame is not analyzed in frames 450 to
// 461, and 20 more render frames are analyzed at frame 500. The delay of
// the audio buffer is set at frame 100, which has no effect without an
// external delay estimator, and the capture output is not used in frames
// 540 to 544.
type echoCanceller3Scenario struct {
	canceller       *echoCanceller3
	custom          bool
	numBands        int
	random          *testRandom
	render, capture [][][]float32
	linearOutput    [][][]float32
	delayBuffers    [][]*testDelayBuffer
	inputEnergy     float32
}

func newEchoCanceller3Scenario(
	t *testing.T, sampleRateHz, numRender, numCapture, delaySamples int, mode string,
) *echoCanceller3Scenario {
	t.Helper()
	config := defaultEchoCanceller3Config()
	var multichannelConfig *echoCanceller3Config
	switch mode {
	case echoCanceller3ModeStereo:
		config.multiChannel.stereoDetectionHysteresisSeconds = 0.2
	case echoCanceller3ModeCustom:
		config.filter.highPassFilterEchoReference = true
		config.delay.fixedCaptureDelaySamples = 100
		config.filter.exportLinearAecOutput = true
		multichannel := config
		multichannel.filter.refined.lengthBlocks = 11
		multichannel.filter.coarse.lengthBlocks = 11
		multichannel.filter.refinedInitial.lengthBlocks = 10
		multichannel.filter.coarseInitial.lengthBlocks = 10
		multichannelConfig = &multichannel
	}
	canceller, err := newEchoCanceller3(config, multichannelConfig, sampleRateHz, numRender, numCapture)
	require.NoError(t, err)
	numBands := numBandsForRate(sampleRateHz)
	scenario := &echoCanceller3Scenario{
		canceller:    canceller,
		custom:       mode == echoCanceller3ModeCustom,
		numBands:     numBands,
		random:       newTestRandom(42),
		render:       newSplitBandsFrame(numRender, numBands),
		capture:      newSplitBandsFrame(numCapture, numBands),
		linearOutput: newSplitBandsFrame(numCapture, 1),
		delayBuffers: make([][]*testDelayBuffer, numBands),
	}
	for band := range scenario.delayBuffers {
		scenario.delayBuffers[band] = make([]*testDelayBuffer, numRender)
		for ch := range scenario.delayBuffers[band] {
			scenario.delayBuffers[band][ch] = newTestDelayBuffer(delaySamples)
		}
	}

	return scenario
}

// echoCanceller3ScenarioNearendAmplitude returns the amplitude of the
// nearend signal of a frame.
func echoCanceller3ScenarioNearendAmplitude(index int) float32 {
	burst := index%40 >= 32 && (index/40 == 2 || index/40 == 6 || index/40 == 10)
	if inRange(index, 280, 304) || burst {
		return 3000
	}

	return 30
}

// generateRender draws a band of the render frame and returns the echo
// that it causes.
func (e *echoCanceller3Scenario) generateRender(index, band int) [splitBandSize]float32 {
	var echo, delayed, noise [splitBandSize]float32
	for ch := range e.render {
		// The channels hold the same signal until frame 200.
		if ch == 0 || index >= 200 {
			randomizeSampleVectorAmplitude(e.random, noise[:], 10000)
		}
		samples := e.render[ch][band]
		copy(samples, noise[:])
		if index < 40 || index%40 >= 32 {
			clear(samples)
		}
		e.delayBuffers[band][ch].delay(samples, delayed[:])
		for k := range echo {
			echo[k] += delayed[k] / float32(len(e.render)) //nolint:gosec // G602 false positive on a range loop.
		}
	}

	return echo
}

// generate draws the render and the capture frames.
func (e *echoCanceller3Scenario) generate(index int) {
	nearendAmplitude := echoCanceller3ScenarioNearendAmplitude(index)
	var nearend [splitBandSize]float32
	for band := range e.numBands {
		echo := e.generateRender(index, band)
		for ch := range e.capture {
			randomizeSampleVectorAmplitude(e.random, nearend[:], nearendAmplitude)
			for k := range echo {
				e.capture[ch][band][k] = 0.6*echo[k]/float32(ch+1) + nearend[k] //nolint:gosec // G602 false positive on a range loop.
			}
			if band == 0 && inRange(index, 330, 334) {
				e.capture[ch][band][17] += 40000
			}
		}
	}
}

// step processes a frame.
func (e *echoCanceller3Scenario) step(index int) {
	e.generate(index)
	e.inputEnergy = energy(e.capture[len(e.capture)-1][0])

	if !inRange(index, 450, 462) {
		e.canceller.analyzeRender(e.render)
	}
	if index == 500 {
		for range 20 {
			e.canceller.analyzeRender(e.render)
		}
	}
	fullBand := make([][]float32, len(e.capture))
	for ch := range fullBand {
		fullBand[ch] = e.capture[ch][0]
	}
	e.canceller.analyzeCapture(fullBand)
	if index == 100 {
		e.canceller.setAudioBufferDelay(40)
	}
	e.canceller.setCaptureOutputUsage(!inRange(index, 540, 545))
	levelChange := inRange(index, 250, 260)
	if e.custom {
		e.canceller.processCapture(e.capture, e.linearOutput, levelChange)
	} else {
		e.canceller.processCapture(e.capture, nil, levelChange)
	}
}

// TestEchoCanceller3_MatchesUpstream compares the energy of the output
// of every frame, in dB, and the metrics of every 10th frame with the
// upstream EchoCanceller3 at the pinned commit, like
// TestEchoRemover_MatchesUpstream does.
//
// The custom scenario is sensitive to the rounding of the computations:
// scaling its capture signal by 1.0000001 changes the output of upstream
// itself by 0.0006 dB for the median frame, 0.2 dB for 90% of the frames
// and up to 27 dB for single frames. When the coarse filter has been reset
// from the refined one, the errors of the two filters are equal to the
// last bit, and their comparison decides how the filters are updated. Its
// energies are therefore compared in distribution.
//
// Each line of the reference file is keyed by
// "name sample_rate render_channels capture_channels delay mode".
func TestEchoCanceller3_MatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		sampleRateHz, numRender, numCapture, delaySamples int
		mode                                              string
	}{{48000, 1, 1, 640, aecStateModeDefault}, {16000, 2, 2, 400, echoCanceller3ModeStereo}, {32000, 2, 2, 400, echoCanceller3ModeCustom}} {
		suffix := fmt.Sprintf(" %d %d %d %d %s", tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples, tc.mode)
		t.Run(suffix[1:], func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/echo_canceller3_upstream.txt", suffix)
			metricsReference := reference.filter("metrics")
			scenario := newEchoCanceller3Scenario(t, tc.sampleRateHz, tc.numRender, tc.numCapture, tc.delaySamples,
				tc.mode)
			last := tc.numCapture - 1
			var output, upper, linear, erl, erle levelDeviations

			for index := range echoCanceller3ScenarioFrames {
				scenario.step(index)

				// The frame index, then the energies of the input, the
				// output, its upper bands and the output of the linear
				// filter.
				want := reference.next("energies")
				require.Equal(t, float32(index), want[0])
				require.InEpsilon(t, want[1], scenario.inputEnergy, 1e-5, "frame %d", index)
				output.add(want[2], energy(scenario.capture[last][0]))
				var upperEnergy float32
				for band := 1; band < scenario.numBands; band++ {
					upperEnergy += energy(scenario.capture[last][band])
				}
				upper.add(want[3], upperEnergy)
				if scenario.custom {
					linear.add(want[4], energy(scenario.linearOutput[last][0]))
				}

				if index%10 == 9 {
					// The frame index, the ERL, the ERLE, the delay, and
					// whether the stereo render processing is active.
					wantMetrics := metricsReference.next("metrics")
					gotErl, gotErle, delayMs := scenario.canceller.getMetrics()
					erl = append(erl, math.Abs(gotErl-float64(wantMetrics[1])))
					erle = append(erle, math.Abs(gotErle-float64(wantMetrics[2])))
					stereo := scenario.canceller.multichannelContentDetector.isProperMultiChannelContentDetected()
					require.Equal(t, wantMetrics[4], boolValue(stereo), "frame %d", index) //nolint:testifylint // Flag.
					if !refdata.FusesMultiplyAdd {
						require.Equal(t, int(wantMetrics[3]), delayMs, "frame %d", index)
					}
				}
			}
			if scenario.custom {
				// See the comment of the test.
				output.assertQuantiles(t, "output", 0.05, 1, math.Inf(1))
				upper.assertQuantiles(t, "upper bands", 0.05, 1, math.Inf(1))
				linear.assertQuantiles(t, "linear output", 0.05, 1, math.Inf(1))
				erl.assertQuantiles(t, "ERL", 0.01, 0.1, math.Inf(1))
				erle.assertQuantiles(t, "ERLE", 0.01, 0.1, math.Inf(1))
			} else {
				output.assertBelow(t, "output", 0.01, 0.1, 2, math.Inf(1))
				if scenario.numBands > 1 {
					upper.assertBelow(t, "upper bands", 0.01, 0.1, 2, math.Inf(1))
				}
				erl.assertBelow(t, "ERL", 0.001, 0.01, 0.01, 0.01)
				erle.assertBelow(t, "ERLE", 0.001, 0.1, 1, math.Inf(1))
			}
			require.Empty(t, reference.entries)
			require.Empty(t, metricsReference.entries)
		})
	}
}
