// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package refdata loads reference output recorded from the upstream
// WebRTC implementation, for tests that compare a port against it.
package refdata

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Entry is one line of a reference file: "key: sample sample ...".
type Entry struct {
	Key     string
	Samples []float32
}

// Load reads every entry of the reference file at path, in file order.
// It fails the test on a malformed or empty file.
func Load(tb testing.TB, path string) []Entry {
	tb.Helper()
	file, err := os.Open(path) //nolint:gosec // Test data path chosen by the test.
	require.NoError(tb, err)
	defer func() { assert.NoError(tb, file.Close()) }()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		key, samples, found := strings.Cut(scanner.Text(), ":")
		require.True(tb, found, "missing key in %q", path)

		entry := Entry{Key: key}
		for field := range strings.FieldsSeq(samples) {
			value, err := strconv.ParseFloat(field, 32)
			require.NoError(tb, err)
			entry.Samples = append(entry.Samples, float32(value))
		}
		entries = append(entries, entry)
	}
	require.NoError(tb, scanner.Err())
	require.NotEmpty(tb, entries, "no reference data in %q", path)

	return entries
}
