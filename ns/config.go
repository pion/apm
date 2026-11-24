// SPDX-FileCopyrightText: 2012-2019 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package ns

// SuppressionLevel selects the noise suppression aggressiveness. Values
// match upstream NsConfig::SuppressionLevel.
type SuppressionLevel int

const (
	Level6dB SuppressionLevel = iota
	Level12dB
	Level18dB
	Level21dB
)

// Config configures a Suppressor.
type Config struct {
	TargetLevel SuppressionLevel
}

// DefaultConfig returns the upstream default (12 dB target).
func DefaultConfig() Config { return Config{TargetLevel: Level12dB} }
