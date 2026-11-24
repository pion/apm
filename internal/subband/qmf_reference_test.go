// SPDX-FileCopyrightText: 2011-2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause

package subband

import (
	"testing"

	"github.com/pion/apm/internal/refdata"
	"github.com/stretchr/testify/assert"
)

// Reference vectors from WebRtcSpl_AnalysisQMF and WebRtcSpl_SynthesisQMF
// at the pinned upstream commit: four consecutive 16-sample frames run
// through one analysis and one synthesis state. Frames two and three
// saturate the synthesis output.

func qmfReferenceInput() [][]float32 {
	return [][]float32{
		{
			-31431, -31684, 2828, 8841, 26871, -25398, -270, 3168,
			6298, 18576, -30157, 15977, 30527, -27941, 13081, -2603,
		},
		{
			-6785, 30115, -6153, 29672, 32353, 10728, -5054, -5,
			-13941, -5027, 11489, -22591, -26951, 23113, -12350, -32305,
		},
		{
			-2447, -13653, 4783, 30295, -31755, 18334, -12960, -11723,
			29818, 3068, 10961, 10286, 8088, -22201, 28119, 17525,
		},
		{
			-2814, 14790, -11025, 13936, 4984, 17132, -3753, -19270,
			-2822, 11263, 1376, 24425, -7104, 27894, 15876, -23406,
		},
	}
}

func qmfReferenceLow() [][]float32 {
	return [][]float32{
		{-4447.02148, -20886.709, -5803.37988, 16887.6992, -12690.9258, 9614.61914, 2791.72168, 1152.97656},
		{5726.86621, -3186.5918, 28801.959, 11349.3779, 5674.3667, -17974.752, 3823.28271, -18471.1523},
		{-2524.66504, -17004.5, 7849.81641, 4000.66309, -11321.7871, 18445.5176, 5999.34912, 4292.44336},
		{5859.19531, 14921.7168, -4641.36572, 13335.0273, -4472.84375, -6552.39746, 16599.0977, 9979.05664},
	}
}

func qmfReferenceHigh() [][]float32 {
	return [][]float32{
		{2917.16992, 3579.65625, -10931.6045, -14189.71, -7273.90234, 12162.7734, 16762.3281, -18748.2031},
		{-19282.8633, 22799.9355, 13493.9941, -5602.23535, 3644.73291, 4418.74268, -10116.5742, 9645.25586},
		{12497.8076, -26113.3789, 19295.1484, 25089.9727, 2687.55811, -22164.9238, 2219.35303, -8501.10156},
		{-16416.0391, 18328.8691, 10367.4102, 3427.27295, 2070.62695, -4806.92969, 16420.6328, 11950.332},
	}
}

func qmfReferenceOutput() [][]float32 {
	return [][]float32{
		{
			-355.577515, -358.439758, -5303.18262, -5278.10205, -18201.2793, -17924.7559, -8857.19141, -14180.2607,
			28245.8379, 375.006836, 12413.8672, -10087.8252, -16076.6953, 17814.8906, -1765.07031, 21726.668,
		},
		{
			2118.27832, -28656.332, 29836.7227, -2544.48633, -4503.51953, 26421.9824, -16480.6309, 32767,
			32767, 4990.625, -1693.12891, -1987.71729, -12303.2959, -5260.04785, -6611.41113, -16301.0879,
		},
		{
			-1494.62646, 5066.68359, -31247.9609, -9905.75195, 3293.28125, -19685.4219, 5516.37598, 15195.4824,
			-31566.8945, 32767, -5927.17285, -29399.5977, 12164.418, 9117.11523, 32767, 2013.14941,
		},
		{
			-10155.6875, -3682.01611, 28323.8516, -3243.6543, 5222.38086, 28916.459, -17478.1934, 14504.9512,
			1672.17578, 63.4619141, 4235.60352, 8700.84961, -7962.34521, -16081.5293, 257.787109, 32767,
		},
	}
}

// TestQMF_MatchesUpstream compares the bands and the output with
// upstream, bit for bit.
func TestQMF_MatchesUpstream(t *testing.T) {
	refdata.SkipIfFusesMultiplyAdd(t)

	var state QMFState
	for i, in := range qmfReferenceInput() {
		low := make([]float32, len(in)/2)
		high := make([]float32, len(in)/2)
		out := make([]float32, len(in))

		AnalysisQMF(in, low, high, &state)
		assert.Equal(t, qmfReferenceLow()[i], low, "low band, frame %d", i)
		assert.Equal(t, qmfReferenceHigh()[i], high, "high band, frame %d", i)

		SynthesisQMF(low, high, out, &state)
		assert.Equal(t, qmfReferenceOutput()[i], out, "output, frame %d", i)
	}
}
