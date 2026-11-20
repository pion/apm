// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Command qa-runner evaluates a matrix of clip x noise x IR scenarios
// against the APM Processor and emits a JSON report. It is the
// equivalent of WebRTC's py_quality_assessment harness.
//
// Default mode is "smoke": runs against testdata/{clips,noise,irs}/smoke/
// and is expected to complete in seconds. Full-corpus mode pulls heavier
// data via testdata/*/pull.sh first.
//
// Usage:
//
//	qa-runner -out report.json
//	qa-runner -corpus full -out report.json
//	qa-runner -corpus synthetic -out report.json   # for CI without fixtures
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/pion/apm/internal/wav"
	"github.com/pion/apm/test/synth"
)

const corpusSynthetic = "synthetic"

var errNoTestdata = errors.New("could not locate testdata/")

func main() {
	corpus := flag.String("corpus", "smoke", "corpus: smoke | full | synthetic")
	out := flag.String("out", "qa-report.json", "output JSON report path")
	root := flag.String("testdata", "", "override testdata/ root (default: repo testdata)")
	flag.Parse()

	if err := run(*corpus, *out, *root); err != nil {
		fmt.Fprintln(os.Stderr, "qa-runner:", err)
		os.Exit(1)
	}
}

func run(corpus, outPath, root string) error {
	subdir := corpus
	switch {
	case corpus == corpusSynthetic:
		dir, err := materializeSynthetic()
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		root = dir
		subdir = ""
	case root == "":
		var err error
		root, err = locateTestdata()
		if err != nil {
			return err
		}
	}

	cells, err := buildMatrix(
		filepath.Join(root, "clips", subdir),
		filepath.Join(root, "noise", subdir),
		filepath.Join(root, "irs", subdir),
	)
	if err != nil {
		return err
	}
	log.Printf("qa-runner: %d cells from %s/{clips,noise,irs}%s", len(cells), root, subOrEmpty(subdir))

	start := time.Now()
	scores := make([]Score, len(cells))
	for i, c := range cells {
		scores[i] = scoreCell(c)
	}
	report := Report{
		Generated: time.Now().UTC(),
		Corpus:    corpus,
		Cells:     len(cells),
		Scores:    scores,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	if err := writeReport(outPath, &report); err != nil {
		return err
	}
	log.Printf("qa-runner: wrote %s (%d cells, %d ms)", outPath, len(scores), report.ElapsedMS)

	return nil
}

func writeReport(path string, report *Report) error {
	f, err := os.Create(path) //nolint:gosec // Path given by a CLI flag.
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}

func subOrEmpty(s string) string {
	if s == "" {
		return ""
	}

	return "/" + s
}

// Report is the top-level JSON document written by qa-runner.
type Report struct {
	Generated time.Time `json:"generated"`
	Corpus    string    `json:"corpus"`
	Cells     int       `json:"cells"`
	ElapsedMS int64     `json:"elapsed_ms"` //nolint:tagliatelle // Report keys are snake_case.
	Scores    []Score   `json:"scores"`
}

// locateTestdata searches upward from the working directory for a
// testdata directory with the expected subdirs, so the runner works from
// the repo root or from inside cmd/qa-runner/.
func locateTestdata() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		td := filepath.Join(dir, "testdata")
		if hasSubdir(td, "clips") && hasSubdir(td, "noise") && hasSubdir(td, "irs") {
			return td, nil
		}
	}

	return "", fmt.Errorf("%w from %s", errNoTestdata, cwd)
}

func hasSubdir(parent, name string) bool {
	info, err := os.Stat(filepath.Join(parent, name))

	return err == nil && info.IsDir()
}

// materializeSynthetic builds a throwaway corpus tree with a few generated
// WAV files, so CI smoke runs always have a non-empty corpus.
func materializeSynthetic() (string, error) {
	dir, err := os.MkdirTemp("", "apm-synth-corpus-*")
	if err != nil {
		return "", err
	}
	rng := rand.New(rand.NewPCG(42, 42)) //nolint:gosec // Deterministic test corpus.

	files := []struct {
		path string
		data []float32
	}{
		// Two short "clean speech" stand-ins (sweep + sine) at 16 kHz.
		{filepath.Join("clips", "sweep_200_3000.wav"), synth.Sweep(16000, 16000, 200, 3000, 0.4)},
		{filepath.Join("clips", "sine_500.wav"), synth.Sine(16000, 16000, 500, 0.4)},
		// One speech-shaped noise sample at 16 kHz.
		{filepath.Join("noise", "ssn.wav"), synth.SpeechShapedNoise(32000, 16000, rng)},
		// One IR (exponential decay, 150 ms RT60).
		{filepath.Join("irs", "decay_150ms.wav"), synth.ExpDecayIR(800, 16000, 0.15, rng)},
	}
	for _, f := range files {
		path := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", err
		}
		if err := wav.WriteAll(path, [][]float32{f.data}, 16000, wav.Float32); err != nil {
			return "", err
		}
	}

	return dir, nil
}
