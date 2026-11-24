// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package refdata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reference.txt")
	require.NoError(t, os.WriteFile(path, []byte("a 0: 1 -2.5\nb 1: 3e2\n"), 0o600))

	assert.Equal(t, []Entry{
		{Key: "a 0", Samples: []float32{1, -2.5}},
		{Key: "b 1", Samples: []float32{300}},
	}, Load(t, path))
}
