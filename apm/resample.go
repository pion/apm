// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package apm

import (
	"fmt"

	"github.com/pion/apm/internal/resampler"
)

// suitableProcessRate matches SuitableProcessRate: it identifies the
// native processing rate that best handles a sample rate. It always
// returns one of 16000, 32000 and 48000 Hz.
func suitableProcessRate(minimumRate, maxSplittingRate int, bandSplittingRequired bool) int {
	uppermostNativeRate := 48000
	if bandSplittingRequired {
		uppermostNativeRate = maxSplittingRate
	}
	for _, rate := range []int{16000, 32000, 48000} {
		if rate >= uppermostNativeRate {
			return uppermostNativeRate
		}
		if rate >= minimumRate {
			return rate
		}
	}

	return uppermostNativeRate
}

// rateConverter resamples the 10 ms frames of a stream from one rate to
// another, with one resampler per channel.
type rateConverter struct {
	resamplers []*resampler.PushSincResampler
}

func newRateConverter(from, to SampleRate, numChannels int) (*rateConverter, error) {
	converter := &rateConverter{resamplers: make([]*resampler.PushSincResampler, numChannels)}
	for i := range converter.resamplers {
		r, err := resampler.NewPushSincResampler(from.SamplesPerFrame(), to.SamplesPerFrame())
		if err != nil {
			return nil, fmt.Errorf("apm: resampler: %w", err)
		}
		converter.resamplers[i] = r
	}

	return converter, nil
}

// convert resamples every channel of source into destination.
func (c *rateConverter) convert(source, destination [][]float32) error {
	for i, r := range c.resamplers {
		if err := r.Resample(source[i], destination[i]); err != nil {
			return err
		}
	}

	return nil
}

// allocChannels allocates a [channel][sample] buffer.
func allocChannels(numChannels, numSamples int) [][]float32 {
	channels := make([][]float32, numChannels)
	for i := range channels {
		channels[i] = make([]float32, numSamples)
	}

	return channels
}
