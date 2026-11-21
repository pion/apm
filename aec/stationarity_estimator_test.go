// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package aec

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// stationarityScenarioSteps is the number of steps of the reference
// scenarios of the stationarity estimator and of the echo audibility.
const stationarityScenarioSteps = 3000

// stationarityScenarioLevel returns the level of the render signal of a
// step: stationary noise, with bursts in steps 600 to 699, 1400 to 1409
// and every 300 steps for 40 steps from step 1800 on. The bursts of steps
// 1000 to 1099 only cover the bands 10 to 19. With marginal, short bursts
// are repeated in steps 1500 to 1699, at about the level where a band
// stops being stationary, there is a weak burst in steps 2200 to 2209 and
// the signal is very weak in steps 2501 to 2699.
func stationarityScenarioLevel(index, band int, marginal bool) float32 {
	if marginal {
		if level, found := stationarityScenarioMarginalLevel(index); found {
			return level
		}
	}
	switch {
	case inRange(index, 600, 700):
		return 1000
	case inRange(index, 1000, 1100) && inRange(band, 10, 20):
		return 300
	case inRange(index, 1400, 1410):
		return 50
	case index >= 1800 && index%300 < 40:
		return 200
	default:
		return 1
	}
}

// stationarityScenarioMarginalLevel returns the level of the steps that
// differ with marginal.
func stationarityScenarioMarginalLevel(index int) (float32, bool) {
	switch {
	case inRange(index, 1500, 1700):
		if index%13 < 5 {
			return 25, true
		}

		return 1, true
	case inRange(index, 2200, 2210):
		return 7, true
	case inRange(index, 2501, 2700):
		return 1e-4, true
	default:
		return 0, false
	}
}

// inRange tells whether from <= value < to.
func inRange(value, from, to int) bool { return value >= from && value < to }

// flagChanges records flags when they differ from the previous ones, as
// the upstream reference does: the step, then the flags.
type flagChanges struct {
	previous []float32
	changes  [][]float32
}

func (f *flagChanges) add(index int, flags []float32) {
	if f.previous != nil && fmt.Sprint(flags) == fmt.Sprint(f.previous) {
		return
	}
	f.previous = flags
	f.changes = append(f.changes, append([]float32{float32(index)}, flags...))
}

// assertMatches compares the changes with the lines of the reference
// that have this name.
func (f *flagChanges) assertMatches(t *testing.T, reference *referenceCursor, name string) {
	t.Helper()
	for _, change := range f.changes {
		require.Equal(t, reference.next(name), change, "step %v", change[0])
	}
}

// TestStationarityEstimator_MatchesUpstream compares the stationarity of
// the block and of every band, and the noise spectrum of every 50th
// step, with the upstream StationarityEstimator at the pinned commit, on a spectrum buffer that is filled with random
// spectra. The number of spectra to look ahead changes every 100 steps
// and the estimator is reset at step 2500. Each line of the reference
// file is keyed by "name render_channels".
func TestStationarityEstimator_MatchesUpstream(t *testing.T) {
	for _, numChannels := range []int{1, 2} {
		t.Run(fmt.Sprint(numChannels), func(t *testing.T) {
			reference := newReferenceCursor(t, "testdata/stationarity_estimator_upstream.txt",
				fmt.Sprintf(" %d", numChannels))
			flagsReference := reference.filter("stationarity")
			random := newTestRandom(42)
			buffer := newSpectrumBuffer(30, numChannels)
			estimator := newStationarityEstimator()
			var reverb [kFftLengthBy2Plus1]float32
			var changes flagChanges

			for index := range stationarityScenarioSteps {
				buffer.decWriteIndex()
				for ch := range numChannels {
					for k := range kFftLengthBy2Plus1 {
						draw := random.randFloat()
						buffer.buffer[buffer.write][ch][k] = 1e4 * stationarityScenarioLevel(index, k, true) * (0.5 + draw)
					}
				}
				for k := range reverb {
					reverb[k] = 2e3 * random.randFloat()
				}
				estimator.updateNoiseEstimator(buffer.buffer[buffer.write])
				idxCurrent := buffer.offsetIndex(buffer.write, 14)
				estimator.updateStationarityFlags(buffer, &reverb, idxCurrent, (index/100)%16)
				if index == 2500 {
					estimator.reset()
				}
				if index < 30 || index%50 == 0 {
					want := reference.next("noise")
					require.Equal(t, float32(index), want[0])
					require.InEpsilonSlice(t, want[1:], estimator.noise.noiseSpectrum[:], 1e-6, "step %d", index)
				}

				flags := []float32{boolValue(estimator.isBlockStationary())}
				for band := range kFftLengthBy2Plus1 {
					flags = append(flags, boolValue(estimator.isBandStationary(band)))
				}
				changes.add(index, flags)
			}
			changes.assertMatches(t, flagsReference, "stationarity")
			require.Empty(t, flagsReference.entries)
			require.Empty(t, reference.entries)
		})
	}
}
