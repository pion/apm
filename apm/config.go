// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

// Config configures a Processor. SampleRate and NumChannels apply to the
// near-end (capture) stream; ReverseSampleRate and ReverseNumChannels
// apply to the far-end (render) stream and default to the capture-side
// values when zero.
//
// The modules run at 16, 32 or 48 kHz. A stream at another rate is
// resampled to the processing rate and back: an 8 kHz stream runs at
// 16 kHz, and a 48 kHz stream at MaxProcessingRate.
type Config struct {
	SampleRate         SampleRate
	NumChannels        int
	ReverseSampleRate  SampleRate
	ReverseNumChannels int

	// MaxProcessingRate caps the rate of the modules that work on
	// frequency bands (high-pass filter, noise suppressor and echo
	// canceller), like upstream's maximum_internal_processing_rate. It
	// may only be Rate32k or Rate48k; any other value is treated as
	// Rate48k. DefaultConfig uses the upstream default, Rate32k.
	MaxProcessingRate SampleRate

	HPF HPFConfig
	NS  NSConfig
	AGC AGCConfig
	AEC AECConfig
}

// HPFConfig controls the high-pass filter: a 3-stage cascaded biquad
// pre-tuned per processing rate. Like upstream, the filter also runs when
// the noise suppressor or the echo canceller is enabled.
type HPFConfig struct {
	Enabled bool
}

// NSConfig controls the noise suppressor.
type NSConfig struct {
	Enabled bool
	Level   NSLevel
}

// NSLevel selects the noise-suppression aggressiveness, matching upstream
// NsConfig::SuppressionLevel.
type NSLevel int

const (
	NSLow NSLevel = iota
	NSModerate
	NSHigh
	NSVeryHigh
)

// AGCConfig controls the adaptive digital automatic gain control.
type AGCConfig struct {
	Enabled         bool
	TargetLevelDBFS float32
	CompressionGain float32
}

// AECConfig controls the acoustic echo canceller: AEC3 with the upstream
// default configs, on every channel of the far-end and near-end streams.
type AECConfig struct {
	Enabled bool
}

// DefaultConfig returns a Config ready to construct a Processor at the
// given rate and channel count. Modules are all disabled by default;
// enable them by setting their Enabled flag.
func DefaultConfig(rate SampleRate, channels int) Config {
	return Config{
		SampleRate:        rate,
		NumChannels:       channels,
		MaxProcessingRate: Rate32k,
		NS:                NSConfig{Level: NSModerate},
		AGC:               AGCConfig{TargetLevelDBFS: -3, CompressionGain: 9},
	}
}
