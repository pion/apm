// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package visqol

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pion/apm/internal/wav"
	"github.com/pion/apm/test/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAvailable_GuardsTests(t *testing.T) {
	if !Available() {
		t.Skip("ViSQOL not on PATH; see the package docs")
	}
}

func TestScore_IdenticalFiles(t *testing.T) {
	if !Available() {
		t.Skip("ViSQOL not on PATH")
	}
	dir := t.TempDir()
	clean := synth.Sine(48000, 48000, 1000, 0.5)
	a := filepath.Join(dir, "a.wav")
	b := filepath.Join(dir, "b.wav")
	require.NoError(t, wav.WriteAll(a, [][]float32{clean}, 48000, wav.PCM16))
	require.NoError(t, wav.WriteAll(b, [][]float32{clean}, 48000, wav.PCM16))
	r, err := Score(context.Background(), AudioMode, a, b)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, r.MOSLQO, 4.0, "identical WAV pair should score high")
}

func TestParseOutput_PicksScore(t *testing.T) {
	out := `Visqol version 3.3.3
some banner
MOS-LQO: 4.521
done.`
	r, err := parseOutput(out, "a.wav", "b.wav")
	require.NoError(t, err)
	assert.InDelta(t, 4.521, r.MOSLQO, 1e-9)
}
