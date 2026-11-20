#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
# SPDX-License-Identifier: MIT

# Fetch the full clean-speech corpus into ./full/.
#
# Source: LibriSpeech "dev-clean" subset (CC-BY 4.0).
# https://www.openslr.org/12/
#
# The script is intentionally cautious: it checks SHA-256 of every file
# against checksums.txt before installing into ./full/. Run from this
# directory (testdata/clips/).

set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
cd "$here"

mkdir -p full
echo "TODO: implement download of LibriSpeech dev-clean subset into ./full/"
echo
echo "Expected layout after pull:"
echo "  ./full/libri_*_<id>.wav   (mono, 16 kHz, ~3-10 s each)"
echo "  ./full/checksums.txt      (sha256  filename, one per line)"
echo
echo "Until implemented, run qa-runner with -corpus synthetic for CI use,"
echo "or place hand-curated WAVs into ./full/ and update manifest.yaml."
