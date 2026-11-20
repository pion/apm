// SPDX-FileCopyrightText: 2012-2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package apm

// Config configures a Processor. SampleRate and NumChannels apply to the
// near-end (capture) stream; ReverseSampleRate and ReverseNumChannels
// apply to the far-end (render) stream and default to the capture-side
// values when zero.
//
// The modules run at 16, 32 or 48 kHz. An 8 kHz stream is resampled to
// 16 kHz and back.
type Config struct {
	SampleRate         SampleRate
	NumChannels        int
	ReverseSampleRate  SampleRate
	ReverseNumChannels int

	HPF HPFConfig
	NS  NSConfig
	AGC AGCConfig
	AEC AECConfig
}

// HPFConfig controls the high-pass filter: a 3-stage cascaded biquad
// pre-tuned per processing rate.
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

// AECConfig controls the acoustic echo canceller.
type AECConfig struct {
	Enabled bool
}

// DefaultConfig returns a Config ready to construct a Processor at the
// given rate and channel count. Modules are all disabled by default;
// enable them by setting their Enabled flag.
func DefaultConfig(rate SampleRate, channels int) Config {
	return Config{
		SampleRate:  rate,
		NumChannels: channels,
		NS:          NSConfig{Level: NSModerate},
		AGC:         AGCConfig{TargetLevelDBFS: -3, CompressionGain: 9},
	}
}
