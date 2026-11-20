// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

import (
	"errors"
	"fmt"
)

var (
	// ErrUnsupportedSampleRate is returned for sample rates other than 16 kHz.
	ErrUnsupportedSampleRate = errors.New("ns: unsupported sample rate")
	// ErrInvalidChannelCount is returned when the channel count is not positive or does not match.
	ErrInvalidChannelCount = errors.New("ns: invalid channel count")
	// ErrInvalidFrameSize is returned when a channel does not hold one 10 ms frame.
	ErrInvalidFrameSize = errors.New("ns: invalid frame size")
)

type channelState struct {
	speechProb             *speechProbabilityEstimator
	wienerFilter           *wienerFilter
	noiseEstimator         *noiseEstimator
	prevAnalysisSignalSpec [fftSizeBy2Plus1]float32
	analyzeAnalysisMemory  [overlapSize]float32
	processAnalysisMemory  [overlapSize]float32
	processSynthesisMemory [overlapSize]float32
}

func newChannelState(sp *suppressionParams) *channelState {
	ne := newNoiseEstimator(sp)
	cs := &channelState{
		speechProb:     newSpeechProbabilityEstimator(),
		wienerFilter:   newWienerFilter(sp),
		noiseEstimator: ne,
	}
	for i := range cs.prevAnalysisSignalSpec {
		cs.prevAnalysisSignalSpec[i] = 1
	}

	return cs
}

type filterBankState struct {
	real          [fftSizeBy2Plus1]float32
	imag          [fftSizeBy2Plus1]float32
	extendedFrame [fftSize]float32
}

// Suppressor is the noise-suppression engine for a single stream. One
// instance per stream. Not safe for concurrent use.
type Suppressor struct {
	params            suppressionParams
	numAnalyzedFrames int32
	fft               *fftWrapper
	captureOutputUsed bool
	channels          []*channelState
	filterBankStates  []filterBankState
	energiesBefore    []float32
	gainAdjustments   []float32
}

// NewSuppressor constructs a Suppressor. Only 16 kHz is supported; calls
// at other rates return an error.
func NewSuppressor(cfg Config, sampleRateHz, numChannels int) (*Suppressor, error) {
	if sampleRateHz != 16000 {
		return nil, fmt.Errorf("%w: %d Hz, want 16000", ErrUnsupportedSampleRate, sampleRateHz)
	}
	if numChannels <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidChannelCount, numChannels)
	}
	params := newSuppressionParams(cfg.TargetLevel)
	sup := &Suppressor{
		params:            params,
		numAnalyzedFrames: -1,
		fft:               newFFT(),
		captureOutputUsed: true,
		channels:          make([]*channelState, numChannels),
		filterBankStates:  make([]filterBankState, numChannels),
		energiesBefore:    make([]float32, numChannels),
		gainAdjustments:   make([]float32, numChannels),
	}
	for c := range sup.channels {
		sup.channels[c] = newChannelState(&sup.params)
	}

	return sup, nil
}

// SetCaptureOutputUsed mirrors upstream's SetCaptureOutputUsage: when
// false, Process skips the synthesis step (the output isn't used).
func (s *Suppressor) SetCaptureOutputUsed(used bool) { s.captureOutputUsed = used }

// NumChannels returns the configured channel count.
func (s *Suppressor) NumChannels() int { return len(s.channels) }

// Analyze adapts the noise model. audio is [channel][sample] with 160
// samples per channel (one 10 ms frame at 16 kHz). The audio is read but
// not modified.
func (s *Suppressor) Analyze(audio [][]float32) error {
	if err := s.checkFrames(audio); err != nil {
		return err
	}
	for c := range audio {
		s.channels[c].noiseEstimator.prepareAnalysis()
	}

	zeroFrame := true
	for c := range audio {
		if computeEnergyFrameAndMemory(audio[c], &s.channels[c].analyzeAnalysisMemory) > 0 {
			zeroFrame = false

			break
		}
	}
	if zeroFrame {
		return nil
	}

	s.numAnalyzedFrames++
	if s.numAnalyzedFrames < 0 {
		s.numAnalyzedFrames = 0
	}

	for c := range audio {
		ch := s.channels[c]
		var extended [fftSize]float32
		formExtendedFrame(audio[c], &ch.analyzeAnalysisMemory, &extended)
		applyFilterBankWindow(&extended)

		var re, im [fftSizeBy2Plus1]float32
		s.fft.forward(&extended, &re, &im)

		var signalSpec [fftSizeBy2Plus1]float32
		computeMagnitudeSpectrum(&re, &im, &signalSpec)

		var signalEnergy float32
		for i := range fftSizeBy2Plus1 {
			signalEnergy += re[i]*re[i] + im[i]*im[i]
		}
		signalEnergy /= float32(fftSizeBy2Plus1)

		var signalSpectralSum float32
		for i := range fftSizeBy2Plus1 {
			signalSpectralSum += signalSpec[i]
		}

		ch.noiseEstimator.preUpdate(s.numAnalyzedFrames, &signalSpec, signalSpectralSum)

		var priorSnr, postSnr [fftSizeBy2Plus1]float32
		computeSnr(
			&ch.wienerFilter.filter,
			&ch.prevAnalysisSignalSpec,
			&signalSpec,
			&ch.noiseEstimator.prevNoiseSpec,
			&ch.noiseEstimator.noiseSpec,
			&priorSnr,
			&postSnr,
		)

		ch.speechProb.update(
			s.numAnalyzedFrames,
			&priorSnr,
			&postSnr,
			&ch.noiseEstimator.conservativeSpec,
			&signalSpec,
			signalSpectralSum,
			signalEnergy,
		)

		ch.noiseEstimator.postUpdate(&ch.speechProb.speechProb, &signalSpec)

		ch.prevAnalysisSignalSpec = signalSpec
	}

	return nil
}

// Process applies suppression in place. audio is [channel][sample] with
// 160 samples per channel. Output replaces input.
func (s *Suppressor) Process(audio [][]float32) error {
	if err := s.checkFrames(audio); err != nil {
		return err
	}

	s.updateFilters(audio)
	if !s.captureOutputUsed {
		return nil
	}
	s.applyFilter()
	s.synthesize(audio)

	return nil
}

func (s *Suppressor) checkFrames(audio [][]float32) error {
	if len(audio) != len(s.channels) {
		return fmt.Errorf("%w: got %d, want %d", ErrInvalidChannelCount, len(audio), len(s.channels))
	}
	for c := range audio {
		if len(audio[c]) != nsFrameSize {
			return fmt.Errorf("%w: channel %d has %d samples, want %d", ErrInvalidFrameSize, c, len(audio[c]), nsFrameSize)
		}
	}

	return nil
}

// updateFilters analyzes each channel and updates its Wiener filter.
func (s *Suppressor) updateFilters(audio [][]float32) {
	for c := range audio {
		fbs := &s.filterBankStates[c]
		ch := s.channels[c]
		formExtendedFrame(audio[c], &ch.processAnalysisMemory, &fbs.extendedFrame)
		applyFilterBankWindow(&fbs.extendedFrame)
		s.energiesBefore[c] = computeEnergyOfExtended(&fbs.extendedFrame)

		s.fft.forward(&fbs.extendedFrame, &fbs.real, &fbs.imag)

		var signalSpec [fftSizeBy2Plus1]float32
		computeMagnitudeSpectrum(&fbs.real, &fbs.imag, &signalSpec)

		ch.wienerFilter.update(
			s.numAnalyzedFrames,
			&ch.noiseEstimator.noiseSpec,
			&ch.noiseEstimator.prevNoiseSpec,
			&ch.noiseEstimator.parametricSpec,
			&signalSpec,
		)
	}
}

// applyFilter multiplies every channel spectrum by the shared filter.
func (s *Suppressor) applyFilter() {
	var filter [fftSizeBy2Plus1]float32
	if len(s.channels) == 1 {
		filter = s.channels[0].wienerFilter.filter
	} else {
		s.aggregateWienerFilters(&filter)
	}

	for c := range s.filterBankStates {
		fbs := &s.filterBankStates[c]
		for i := range fftSizeBy2Plus1 {
			fbs.real[i] *= filter[i]
			fbs.imag[i] *= filter[i]
		}
	}
}

// synthesize turns the filtered spectra back into audio frames.
func (s *Suppressor) synthesize(audio [][]float32) {
	for c := range audio {
		fbs := &s.filterBankStates[c]
		s.fft.inverse(&fbs.real, &fbs.imag, &fbs.extendedFrame)
	}

	for c := range audio {
		fbs := &s.filterBankStates[c]
		energyAfter := computeEnergyOfExtended(&fbs.extendedFrame)
		applyFilterBankWindow(&fbs.extendedFrame)
		s.gainAdjustments[c] = s.channels[c].wienerFilter.computeOverallScalingFactor(
			s.numAnalyzedFrames,
			s.channels[c].speechProb.priorSpeechProb,
			s.energiesBefore[c],
			energyAfter,
		)
	}

	gainAdjustment := s.minGainAdjustment()
	for c := range audio {
		fbs := &s.filterBankStates[c]
		for i := range fftSize {
			fbs.extendedFrame[i] *= gainAdjustment
		}
		overlapAndAdd(&fbs.extendedFrame, &s.channels[c].processSynthesisMemory, audio[c])
		clipToInt16Range(audio[c])
	}
}

func (s *Suppressor) minGainAdjustment() float32 {
	gainAdjustment := s.gainAdjustments[0]
	for c := 1; c < len(s.channels); c++ {
		if s.gainAdjustments[c] < gainAdjustment {
			gainAdjustment = s.gainAdjustments[c]
		}
	}

	return gainAdjustment
}

// clipToInt16Range clips to [-32768, 32767] (legacy int16 range, still enforced upstream).
func clipToInt16Range(frame []float32) {
	for i, v := range frame {
		if v > 32767 {
			frame[i] = 32767
		} else if v < -32768 {
			frame[i] = -32768
		}
	}
}

func (s *Suppressor) aggregateWienerFilters(filter *[fftSizeBy2Plus1]float32) {
	*filter = s.channels[0].wienerFilter.filter
	for c := 1; c < len(s.channels); c++ {
		f := &s.channels[c].wienerFilter.filter
		for k := range fftSizeBy2Plus1 {
			if f[k] < filter[k] {
				filter[k] = f[k]
			}
		}
	}
}

func formExtendedFrame(frame []float32, oldData *[overlapSize]float32, extended *[fftSize]float32) {
	copy(extended[:overlapSize], oldData[:])
	copy(extended[overlapSize:], frame)
	copy(oldData[:], extended[fftSize-overlapSize:])
}

func overlapAndAdd(extended *[fftSize]float32, overlapMemory *[overlapSize]float32, outputFrame []float32) {
	for i := range overlapSize {
		outputFrame[i] = overlapMemory[i] + extended[i]
	}
	copy(outputFrame[overlapSize:nsFrameSize], extended[overlapSize:nsFrameSize])
	copy(overlapMemory[:], extended[nsFrameSize:])
}

func computeEnergyOfExtended(x *[fftSize]float32) float32 {
	var e float32
	for _, v := range x {
		e += v * v
	}

	return e
}

func computeEnergyFrameAndMemory(frame []float32, oldData *[overlapSize]float32) float32 {
	var e float32
	for _, v := range oldData {
		e += v * v
	}
	for _, v := range frame {
		e += v * v
	}

	return e
}

func computeMagnitudeSpectrum(re, im, signalSpectrum *[fftSizeBy2Plus1]float32) {
	signalSpectrum[0] = abs32(re[0]) + 1
	signalSpectrum[fftSizeBy2Plus1-1] = abs32(re[fftSizeBy2Plus1-1]) + 1
	for i := 1; i < fftSizeBy2Plus1-1; i++ {
		signalSpectrum[i] = sqrtFastApprox(re[i]*re[i]+im[i]*im[i]) + 1
	}
}

func computeSnr(
	filter, prevSignalSpectrum, signalSpectrum, prevNoiseSpectrum, noiseSpectrum, priorSnr, postSnr *[fftSizeBy2Plus1]float32,
) {
	for i := range fftSizeBy2Plus1 {
		prevEstimate := prevSignalSpectrum[i] / (prevNoiseSpectrum[i] + 0.0001) * filter[i]
		if signalSpectrum[i] > noiseSpectrum[i] {
			postSnr[i] = signalSpectrum[i]/(noiseSpectrum[i]+0.0001) - 1.0
		} else {
			postSnr[i] = 0
		}
		priorSnr[i] = 0.98*prevEstimate + (1.0-0.98)*postSnr[i]
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}

	return x
}
