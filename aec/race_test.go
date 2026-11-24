// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build race

package aec

// raceEnabled reports a race build. Like upstream in debug builds, the
// slowest multichannel tests then run fewer channel combinations.
const raceEnabled = true
