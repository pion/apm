// SPDX-FileCopyrightText: 2018 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package aec

// movingAverageSpectrum mirrors MovingAverageSpectrum: it averages
// spectra over a number of inputs.
type movingAverageSpectrum struct {
	numElem       int
	memLen        int
	memory        []float32
	memIndex      int
	numberUpdates int
}

// newMovingAverageSpectrum creates a movingAverageSpectrum that accepts
// inputs of length numElem and averages over memLen inputs.
func newMovingAverageSpectrum(numElem, memLen int) movingAverageSpectrum {
	return movingAverageSpectrum{
		numElem: numElem,
		memLen:  memLen - 1,
		memory:  make([]float32, numElem*(memLen-1)),
	}
}

// average mirrors Average: it computes the average of the current input
// and up to memLen-1 previous inputs and stores the result in output.
func (m *movingAverageSpectrum) average(input, output []float32) {
	// Sum all contributions.
	copy(output, input)
	for i := 0; i < len(m.memory); i += m.numElem {
		for k, value := range m.memory[i : i+m.numElem] {
			output[k] += value
		}
	}

	// Divide by the number of points used to compute the average.
	scaling := 1 / float32(m.numberUpdates+1)
	for k := range output {
		output[k] *= scaling
	}

	// Update the memory.
	if m.memLen > 0 {
		copy(m.memory[m.memIndex*m.numElem:], input)
		m.memIndex = (m.memIndex + 1) % m.memLen
	}
	m.numberUpdates = min(m.memLen, m.numberUpdates+1)
}

// updateMemoryLength mirrors UpdateMemoryLength: if a new memory length
// is provided, it resets the state and clears the average memory to use
// the new window size.
func (m *movingAverageSpectrum) updateMemoryLength(memLen int) {
	if m.memLen+1 == memLen {
		return
	}
	m.memLen = memLen - 1
	m.memory = make([]float32, m.numElem*m.memLen)
	m.memIndex = 0
	m.numberUpdates = 0
}
