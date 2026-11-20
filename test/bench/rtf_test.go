// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package bench

import (
	"testing"
	"time"

	"github.com/pion/apm/apm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRTF_PassthroughIsRealTime measures the real-time factor of a
// passthrough Processor over a fixed amount of audio. We assert RTF < 1
// (it must process audio faster than wall-clock) plus a generous margin.
//
// Once modules are implemented, this test stays valid: RTF will grow
// but the boundary is the same. Adjust the bound in the assertion if a
// real implementation legitimately approaches it.
func TestRTF_PassthroughIsRealTime(t *testing.T) {
	const (
		rate    = apm.Rate16k
		seconds = 10
	)
	proc, err := apm.New(apm.DefaultConfig(rate, 1))
	require.NoError(t, err)
	frame := apm.NewFrame(rate, 1)
	per := rate.SamplesPerFrame()
	framesPerSecond := int(rate) / per
	totalFrames := framesPerSecond * seconds

	start := time.Now()
	for range totalFrames {
		require.NoError(t, proc.ProcessStream(frame))
	}
	elapsed := time.Since(start)
	audioMS := float64(seconds * 1000)
	wallMS := float64(elapsed.Milliseconds())
	rtf := wallMS / audioMS

	t.Logf("RTF = %.4f (processed %d ms of audio in %d ms wall)", rtf, int(audioMS), int(wallMS))
	assert.Less(t, rtf, 0.1, "passthrough RTF")
}
