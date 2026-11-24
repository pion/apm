// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
)

// Scenario names.
const (
	scenarioNSClean   = "ns_clean"
	scenarioNSNoisy   = "ns_noisy"
	scenarioAGCTarget = "agc_target"
	scenarioAECEcho   = "aec_echo"
)

// Cell is one row of the QA matrix: a single scenario configuration to
// evaluate. The runner produces one Score per Cell.
type Cell struct {
	ClipPath  string  `json:"clip"`
	NoisePath string  `json:"noise,omitempty"`
	IRPath    string  `json:"ir,omitempty"`
	SNRDB     float64 `json:"snr_db,omitempty"` //nolint:tagliatelle // Report keys are snake_case.
	Scenario  string  `json:"scenario"`
}

func discoverWavs(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".wav" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(out)

	return out, nil
}

// buildMatrix enumerates the Cartesian product of clips x noise x IRs,
// constrained to a sensible subset of SNR points. When clips or noise are
// missing the matrix degrades gracefully so the runner can still execute
// on whatever fixtures are present.
func buildMatrix(clipsDir, noiseDir, irsDir string) ([]Cell, error) {
	var clips, noises, irs []string
	for _, d := range []struct {
		dir string
		out *[]string
	}{{clipsDir, &clips}, {noiseDir, &noises}, {irsDir, &irs}} {
		wavs, err := discoverWavs(d.dir)
		if err != nil {
			return nil, err
		}
		*d.out = wavs
	}
	snrs := []float64{0, 5, 10, 15}

	var cells []Cell
	for _, c := range clips {
		cells = append(cells, Cell{ClipPath: c, Scenario: scenarioNSClean})
	}
	for _, c := range clips {
		for _, n := range noises {
			for _, s := range snrs {
				cells = append(cells, Cell{ClipPath: c, NoisePath: n, SNRDB: s, Scenario: scenarioNSNoisy})
			}
		}
	}
	for _, c := range clips {
		cells = append(cells, Cell{ClipPath: c, Scenario: scenarioAGCTarget})
	}
	for _, c := range clips {
		for _, ir := range irs {
			cells = append(cells, Cell{ClipPath: c, IRPath: ir, Scenario: scenarioAECEcho})
		}
	}

	return cells, nil
}
