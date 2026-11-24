// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReverbModel(t *testing.T) {
	var model reverbModel
	power := make([]float32, kFftLengthBy2Plus1)
	scaling := make([]float32, kFftLengthBy2Plus1)
	fill(power, 100)
	fill(scaling, 0.5)

	// reverb = (reverb + power * scaling) * decay.
	model.updateReverbNoFreqShaping(power, 0.1, 0.5)
	assert.InDelta(t, 5, model.getReverb()[0], 1e-6)
	model.updateReverb(power, scaling, 0.5)
	assert.InDelta(t, 27.5, model.getReverb()[kFftLengthBy2], 1e-5)

	// No update without a decay.
	model.updateReverb(power, scaling, 0)
	model.updateReverbNoFreqShaping(power, 0.1, -1)
	assert.InDelta(t, 27.5, model.getReverb()[1], 1e-5)

	model.reset()
	assert.Equal(t, [kFftLengthBy2Plus1]float32{}, *model.getReverb())
}
