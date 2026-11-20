// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig is returned for unsupported band or channel counts.
	ErrInvalidConfig = errors.New("subband: invalid configuration")
	// ErrShapeMismatch is returned when a frame does not match the configured shape.
	ErrShapeMismatch = errors.New("subband: frame shape mismatch")
)

// SplittingFilter wraps the 2-band QMF and 3-band DCT filter banks
// behind a single per-channel API. Mirrors modules/audio_processing/
// splitting_filter.{h,cc}.
type SplittingFilter struct {
	numChannels int
	numBands    int
	twoBand     []*QMFState
	threeBand   []*ThreeBandFilterBank
	bandScratch [][]float32 // Per-channel view of the 3 bands.
}

// NewSplittingFilter constructs a filter bank. numBands must be 2 (for
// 32 kHz input) or 3 (for 48 kHz input).
func NewSplittingFilter(numChannels, numBands int) (*SplittingFilter, error) {
	if numBands != 2 && numBands != 3 {
		return nil, fmt.Errorf("%w: numBands must be 2 or 3, got %d", ErrInvalidConfig, numBands)
	}
	if numChannels <= 0 {
		return nil, fmt.Errorf("%w: numChannels must be > 0", ErrInvalidConfig)
	}
	filter := &SplittingFilter{numChannels: numChannels, numBands: numBands}
	if numBands == 2 {
		filter.twoBand = make([]*QMFState, numChannels)
		for i := range filter.twoBand {
			filter.twoBand[i] = &QMFState{}
		}
	} else {
		filter.threeBand = make([]*ThreeBandFilterBank, numChannels)
		for i := range filter.threeBand {
			filter.threeBand[i] = NewThreeBandFilterBank()
		}
		filter.bandScratch = make([][]float32, ThreeBandNumBands)
	}

	return filter, nil
}

// NumBands returns the configured number of bands (2 or 3).
func (s *SplittingFilter) NumBands() int { return s.numBands }

// SamplesPerBand returns the per-band sample count for a 10 ms frame
// at this filter's input rate (160 for both 2- and 3-band configs).
func (s *SplittingFilter) SamplesPerBand() int {
	if s.numBands == 2 {
		return 160 // 32 kHz / 2 → 16 kHz subbands, 10 ms = 160 samples
	}

	return ThreeBandSplitBandSize // 480 / 3 = 160
}

// Analysis splits one 10 ms frame per channel into numBands subbands.
// channels and bands are both [channel][sample]; bands[band] is the
// flattened multi-channel band where stride matches numChannels.
//
// Shapes:
//   - 2-band: channels[i] has 320 samples; bands has 2 entries, each
//     [numChannels][160].
//   - 3-band: channels[i] has 480 samples; bands has 3 entries, each
//     [numChannels][160].
func (s *SplittingFilter) Analysis(channels [][]float32, bands [][][]float32) error {
	if err := s.checkShape(channels, bands); err != nil {
		return err
	}
	if s.numBands == 2 {
		for c := 0; c < s.numChannels; c++ {
			if len(channels[c]) != 320 {
				return fmt.Errorf("%w: 2-band channel %d has %d samples, want 320", ErrShapeMismatch, c, len(channels[c]))
			}
			AnalysisQMF(channels[c], bands[0][c], bands[1][c], s.twoBand[c])
		}

		return nil
	}
	for c := 0; c < s.numChannels; c++ {
		if len(channels[c]) != ThreeBandFullBandSize {
			return fmt.Errorf("%w: 3-band channel %d has %d samples, want %d",
				ErrShapeMismatch, c, len(channels[c]), ThreeBandFullBandSize)
		}
		// Per upstream three_band_filter_bank.cc, the analysis is in-place
		// in the sense that the input is split into the band-wise outputs.
		s.threeBand[c].Analysis(channels[c], s.channelBands(bands, c))
	}

	return nil
}

// Synthesis is the inverse of Analysis.
func (s *SplittingFilter) Synthesis(bands [][][]float32, channels [][]float32) error {
	if err := s.checkShape(channels, bands); err != nil {
		return err
	}
	if s.numBands == 2 {
		for c := 0; c < s.numChannels; c++ {
			SynthesisQMF(bands[0][c], bands[1][c], channels[c], s.twoBand[c])
		}

		return nil
	}
	for c := 0; c < s.numChannels; c++ {
		s.threeBand[c].Synthesis(s.channelBands(bands, c), channels[c])
	}

	return nil
}

func (s *SplittingFilter) checkShape(channels [][]float32, bands [][][]float32) error {
	if len(channels) != s.numChannels {
		return fmt.Errorf("%w: got %d channels, want %d", ErrShapeMismatch, len(channels), s.numChannels)
	}
	if len(bands) != s.numBands {
		return fmt.Errorf("%w: got %d bands, want %d", ErrShapeMismatch, len(bands), s.numBands)
	}

	return nil
}

// channelBands returns the 3 bands of channel c, reusing a scratch slice.
func (s *SplittingFilter) channelBands(bands [][][]float32, c int) [][]float32 {
	for b := range s.bandScratch {
		s.bandScratch[b] = bands[b][c]
	}

	return s.bandScratch
}
