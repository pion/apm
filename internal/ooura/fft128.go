// SPDX-FileCopyrightText: 1996-2001 Takuya Ooura
// SPDX-FileCopyrightText: 2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause AND LicenseRef-Ooura

//nolint:varnamelen // The names of ooura_fft.cc.
package ooura

// Fft128 matches OouraFft::Fft, on the C path: it computes in place the
// real FFT of length 128 of a. On return, a[0] and a[1] hold the real
// parts of the DC and the Nyquist bins, and a[2k] and a[2k+1] the real
// and the imaginary parts of bin k. The imaginary parts have the opposite
// sign of the usual DFT.
func Fft128(a *[128]float32) {
	bitrv2128(a)
	cftfsub128(a)
	rftfsub128(a)
	xi := a[0] - a[1]
	a[0] += a[1]
	a[1] = xi
}

// InverseFft128 matches OouraFft::InverseFft: it computes in place the
// inverse of Fft128, scaled by 64.
func InverseFft128(a *[128]float32) {
	a[1] = 0.5 * (a[0] - a[1])
	a[0] -= a[1]
	rftbsub128(a)
	bitrv2128(a)
	cftbsub128(a)
}

func cft1st128(a *[128]float32) {
	const n = 128
	var wk1r, wk1i, wk2r, wk2i, wk3r, wk3i float32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32

	x0r = a[0] + a[2]
	x0i = a[1] + a[3]
	x1r = a[0] - a[2]
	x1i = a[1] - a[3]
	x2r = a[4] + a[6]
	x2i = a[5] + a[7]
	x3r = a[4] - a[6]
	x3i = a[5] - a[7]
	a[0] = x0r + x2r
	a[1] = x0i + x2i
	a[4] = x0r - x2r
	a[5] = x0i - x2i
	a[2] = x1r - x3i
	a[3] = x1i + x3r
	a[6] = x1r + x3i
	a[7] = x1i - x3r
	wk1r = rdftW[2]
	x0r = a[8] + a[10]
	x0i = a[9] + a[11]
	x1r = a[8] - a[10]
	x1i = a[9] - a[11]
	x2r = a[12] + a[14]
	x2i = a[13] + a[15]
	x3r = a[12] - a[14]
	x3i = a[13] - a[15]
	a[8] = x0r + x2r
	a[9] = x0i + x2i
	a[12] = x2i - x0i
	a[13] = x0r - x2r
	x0r = x1r - x3i
	x0i = x1i + x3r
	a[10] = wk1r * (x0r - x0i)
	a[11] = wk1r * (x0r + x0i)
	x0r = x3i + x1r
	x0i = x3r - x1i
	a[14] = wk1r * (x0i - x0r)
	a[15] = wk1r * (x0i + x0r)
	k1 := 0
	for j := 16; j < n; j += 16 {
		k1 += 2
		k2 := 2 * k1
		wk2r = rdftW[k1+0]
		wk2i = rdftW[k1+1]
		wk1r = rdftW[k2+0]
		wk1i = rdftW[k2+1]
		wk3r = rdftWk3riFirst[k1+0]
		wk3i = rdftWk3riFirst[k1+1]
		x0r = a[j+0] + a[j+2]
		x0i = a[j+1] + a[j+3]
		x1r = a[j+0] - a[j+2]
		x1i = a[j+1] - a[j+3]
		x2r = a[j+4] + a[j+6]
		x2i = a[j+5] + a[j+7]
		x3r = a[j+4] - a[j+6]
		x3i = a[j+5] - a[j+7]
		a[j+0] = x0r + x2r
		a[j+1] = x0i + x2i
		x0r -= x2r
		x0i -= x2i
		a[j+4] = wk2r*x0r - wk2i*x0i
		a[j+5] = wk2r*x0i + wk2i*x0r
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j+2] = wk1r*x0r - wk1i*x0i
		a[j+3] = wk1r*x0i + wk1i*x0r
		x0r = x1r + x3i
		x0i = x1i - x3r
		a[j+6] = wk3r*x0r - wk3i*x0i
		a[j+7] = wk3r*x0i + wk3i*x0r
		wk1r = rdftW[k2+2]
		wk1i = rdftW[k2+3]
		wk3r = rdftWk3riSecond[k1+0]
		wk3i = rdftWk3riSecond[k1+1]
		x0r = a[j+8] + a[j+10]
		x0i = a[j+9] + a[j+11]
		x1r = a[j+8] - a[j+10]
		x1i = a[j+9] - a[j+11]
		x2r = a[j+12] + a[j+14]
		x2i = a[j+13] + a[j+15]
		x3r = a[j+12] - a[j+14]
		x3i = a[j+13] - a[j+15]
		a[j+8] = x0r + x2r
		a[j+9] = x0i + x2i
		x0r -= x2r
		x0i -= x2i
		a[j+12] = -wk2i*x0r - wk2r*x0i
		a[j+13] = -wk2i*x0i + wk2r*x0r
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j+10] = wk1r*x0r - wk1i*x0i
		a[j+11] = wk1r*x0i + wk1i*x0r
		x0r = x1r + x3i
		x0i = x1i - x3r
		a[j+14] = wk3r*x0r - wk3i*x0i
		a[j+15] = wk3r*x0i + wk3i*x0r
	}
}

func cftmdl128(a *[128]float32) {
	const (
		l = 8
		n = 128
		m = 32
	)
	var wk1r, wk1i, wk2r, wk2i, wk3r, wk3i float32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32

	for j0 := 0; j0 < l; j0 += 2 {
		j1 := j0 + 8
		j2 := j0 + 16
		j3 := j0 + 24
		x0r = a[j0+0] + a[j1+0]
		x0i = a[j0+1] + a[j1+1]
		x1r = a[j0+0] - a[j1+0]
		x1i = a[j0+1] - a[j1+1]
		x2r = a[j2+0] + a[j3+0]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2+0] - a[j3+0]
		x3i = a[j2+1] - a[j3+1]
		a[j0+0] = x0r + x2r
		a[j0+1] = x0i + x2i
		a[j2+0] = x0r - x2r
		a[j2+1] = x0i - x2i
		a[j1+0] = x1r - x3i
		a[j1+1] = x1i + x3r
		a[j3+0] = x1r + x3i
		a[j3+1] = x1i - x3r
	}
	wk1r = rdftW[2]
	for j0 := m; j0 < l+m; j0 += 2 {
		j1 := j0 + 8
		j2 := j0 + 16
		j3 := j0 + 24
		x0r = a[j0+0] + a[j1+0]
		x0i = a[j0+1] + a[j1+1]
		x1r = a[j0+0] - a[j1+0]
		x1i = a[j0+1] - a[j1+1]
		x2r = a[j2+0] + a[j3+0]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2+0] - a[j3+0]
		x3i = a[j2+1] - a[j3+1]
		a[j0+0] = x0r + x2r
		a[j0+1] = x0i + x2i
		a[j2+0] = x2i - x0i
		a[j2+1] = x0r - x2r
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j1+0] = wk1r * (x0r - x0i)
		a[j1+1] = wk1r * (x0r + x0i)
		x0r = x3i + x1r
		x0i = x3r - x1i
		a[j3+0] = wk1r * (x0i - x0r)
		a[j3+1] = wk1r * (x0i + x0r)
	}
	k1 := 0
	const m2 = 2 * m
	for k := m2; k < n; k += m2 {
		k1 += 2
		k2 := 2 * k1
		wk2r = rdftW[k1+0]
		wk2i = rdftW[k1+1]
		wk1r = rdftW[k2+0]
		wk1i = rdftW[k2+1]
		wk3r = rdftWk3riFirst[k1+0]
		wk3i = rdftWk3riFirst[k1+1]
		for j0 := k; j0 < l+k; j0 += 2 {
			j1 := j0 + 8
			j2 := j0 + 16
			j3 := j0 + 24
			x0r = a[j0+0] + a[j1+0]
			x0i = a[j0+1] + a[j1+1]
			x1r = a[j0+0] - a[j1+0]
			x1i = a[j0+1] - a[j1+1]
			x2r = a[j2+0] + a[j3+0]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2+0] - a[j3+0]
			x3i = a[j2+1] - a[j3+1]
			a[j0+0] = x0r + x2r
			a[j0+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2+0] = wk2r*x0r - wk2i*x0i
			a[j2+1] = wk2r*x0i + wk2i*x0r
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1+0] = wk1r*x0r - wk1i*x0i
			a[j1+1] = wk1r*x0i + wk1i*x0r
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3+0] = wk3r*x0r - wk3i*x0i
			a[j3+1] = wk3r*x0i + wk3i*x0r
		}
		wk1r = rdftW[k2+2]
		wk1i = rdftW[k2+3]
		wk3r = rdftWk3riSecond[k1+0]
		wk3i = rdftWk3riSecond[k1+1]
		for j0 := k + m; j0 < l+(k+m); j0 += 2 {
			j1 := j0 + 8
			j2 := j0 + 16
			j3 := j0 + 24
			x0r = a[j0+0] + a[j1+0]
			x0i = a[j0+1] + a[j1+1]
			x1r = a[j0+0] - a[j1+0]
			x1i = a[j0+1] - a[j1+1]
			x2r = a[j2+0] + a[j3+0]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2+0] - a[j3+0]
			x3i = a[j2+1] - a[j3+1]
			a[j0+0] = x0r + x2r
			a[j0+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2+0] = -wk2i*x0r - wk2r*x0i
			a[j2+1] = -wk2i*x0i + wk2r*x0r
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1+0] = wk1r*x0r - wk1i*x0i
			a[j1+1] = wk1r*x0i + wk1i*x0r
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3+0] = wk3r*x0r - wk3i*x0i
			a[j3+1] = wk3r*x0i + wk3i*x0r
		}
	}
}

func rftfsub128(a *[128]float32) {
	c := rdftW[32:]
	for j1, j2 := 1, 2; j2 < 64; j1, j2 = j1+1, j2+2 {
		k2 := 128 - j2
		k1 := 32 - j1
		wkr := 0.5 - c[k1]
		wki := c[j1]
		xr := a[j2+0] - a[k2+0]
		xi := a[j2+1] + a[k2+1]
		yr := wkr*xr - wki*xi
		yi := wkr*xi + wki*xr
		a[j2+0] -= yr
		a[j2+1] -= yi
		a[k2+0] += yr
		a[k2+1] -= yi
	}
}

func rftbsub128(a *[128]float32) {
	c := rdftW[32:]
	a[1] = -a[1]
	for j1, j2 := 1, 2; j2 < 64; j1, j2 = j1+1, j2+2 {
		k2 := 128 - j2
		k1 := 32 - j1
		wkr := 0.5 - c[k1]
		wki := c[j1]
		xr := a[j2+0] - a[k2+0]
		xi := a[j2+1] + a[k2+1]
		yr := wkr*xr + wki*xi
		yi := wkr*xi - wki*xr
		a[j2+0] -= yr
		a[j2+1] = yi - a[j2+1]
		a[k2+0] = yr + a[k2+0]
		a[k2+1] = yi - a[k2+1]
	}
	a[65] = -a[65]
}

func cftbsub128(a *[128]float32) {
	cft1st128(a)
	cftmdl128(a)
	const l = 32
	for j := 0; j < l; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r := a[j] + a[j1]
		x0i := -a[j+1] - a[j1+1]
		x1r := a[j] - a[j1]
		x1i := -a[j+1] + a[j1+1]
		x2r := a[j2] + a[j3]
		x2i := a[j2+1] + a[j3+1]
		x3r := a[j2] - a[j3]
		x3i := a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i - x2i
		a[j2] = x0r - x2r
		a[j2+1] = x0i + x2i
		a[j1] = x1r - x3i
		a[j1+1] = x1i - x3r
		a[j3] = x1r + x3i
		a[j3+1] = x1i + x3r
	}
}

func cftfsub128(a *[128]float32) {
	cft1st128(a)
	cftmdl128(a)
	const l = 32
	for j := 0; j < l; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r := a[j] + a[j1]
		x0i := a[j+1] + a[j1+1]
		x1r := a[j] - a[j1]
		x1i := a[j+1] - a[j1+1]
		x2r := a[j2] + a[j3]
		x2i := a[j2+1] + a[j3+1]
		x3r := a[j2] - a[j3]
		x3i := a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i + x2i
		a[j2] = x0r - x2r
		a[j2+1] = x0i - x2i
		a[j1] = x1r - x3i
		a[j1+1] = x1i + x3r
		a[j3] = x1r + x3i
		a[j3+1] = x1i - x3r
	}
}

// bitrv2128 matches OouraFft::bitrv2_128: it reorders a in bit-reversed
// order of its complex pairs.
func bitrv2128(a *[128]float32) {
	ip := [4]int{0, 64, 32, 96}
	swap := func(j1, k1 int) {
		a[j1+0], a[k1+0] = a[k1+0], a[j1+0]
		a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
	}
	for k := range 4 {
		for j := range k {
			j1 := 2*j + ip[k]
			k1 := 2*k + ip[j]
			swap(j1, k1)
			j1 += 8
			k1 += 16
			swap(j1, k1)
			j1 += 8
			k1 -= 8
			swap(j1, k1)
			j1 += 8
			k1 += 16
			swap(j1, k1)
		}
		j1 := 2*k + 8 + ip[k]
		k1 := j1 + 8
		swap(j1, k1)
	}
}
