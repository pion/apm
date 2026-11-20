// SPDX-FileCopyrightText: 2017 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// delayAdjustment mirrors EchoPathVariability::DelayAdjustment.
type delayAdjustment int

const (
	delayAdjustmentNone delayAdjustment = iota
	delayAdjustmentBufferFlush
	delayAdjustmentNewDetectedDelay
)

// echoPathVariability mirrors EchoPathVariability.
type echoPathVariability struct {
	gainChange  bool
	delayChange delayAdjustment
	clockDrift  bool
}

// audioPathChanged mirrors AudioPathChanged.
func (v echoPathVariability) audioPathChanged() bool {
	return v.gainChange || v.delayChange != delayAdjustmentNone
}
