// SPDX-FileCopyrightText: 2016 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package biquad

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// hpf16k returns the upstream 16 kHz HPF coefficient set, so the
// behavior under test matches what hpf/ uses.
func hpf16k() []Coefficients {
	return []Coefficients{
		{
			B: [3]float32{0.8773539420715290582, -1.754683920749088077, 0.8773539420715289472},
			A: [2]float32{-1.881687317862849707, 0.8880584644559580410},
		},
		{
			B: [3]float32{1.0, -1.999810143464515022, 1.0},
			A: [2]float32{-1.976035417167170793, 0.9779708644868606582},
		},
		{
			B: [3]float32{1.0, -1.999669231394235469, 1.0},
			A: [2]float32{-1.994265767864654482, 0.9954861594635392441},
		},
	}
}

func TestCascade_DCRejection(t *testing.T) {
	c := NewCascade(hpf16k())
	in := make([]float32, 4096)
	for i := range in {
		in[i] = 1.0
	}
	c.Process(in)
	var sum float64
	for _, v := range in[2000:] {
		sum += math.Abs(float64(v))
	}
	avg := sum / float64(len(in)-2000)
	assert.LessOrEqual(t, avg, 0.001, "DC residual")
}

func TestCascade_PassesHighFreq(t *testing.T) {
	rate := 16000.0
	c := NewCascade(hpf16k())
	n := 4096
	in := make([]float32, n)
	for i := range in {
		in[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / rate))
	}
	ref := make([]float32, n)
	copy(ref, in)
	c.Process(in)
	var sIn, sOut float64
	for i := 1000; i < n; i++ {
		sIn += float64(ref[i]) * float64(ref[i])
		sOut += float64(in[i]) * float64(in[i])
	}
	ratio := sOut / sIn
	assert.InDelta(t, 1.0, ratio, 0.1, "passband 1 kHz ratio")
}

func TestCascade_Reset(t *testing.T) {
	c := NewCascade(hpf16k())
	in := make([]float32, 1024)
	for i := range in {
		in[i] = float32(math.Sin(float64(i) * 0.1))
	}
	c.Process(in)
	c.Reset()
	for _, st := range c.stages {
		assert.Equal(t, [2]float32{}, st.x)
		assert.Equal(t, [2]float32{}, st.y)
	}
}

func TestCascade_ZeroAlloc(t *testing.T) {
	c := NewCascade(hpf16k())
	in := make([]float32, 160)
	assert.Zero(t, testing.AllocsPerRun(100, func() { c.Process(in) }))
}
