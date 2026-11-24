// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build arm64 || ppc64 || ppc64le || riscv64 || s390x || loong64 || amd64.v3

package refdata

// FusesMultiplyAdd tells whether the Go compiler fuses a multiplication
// and an addition into one operation on this architecture, which changes
// the rounding of the floating-point computations.
const FusesMultiplyAdd = true
