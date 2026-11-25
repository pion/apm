// SPDX-FileCopyrightText: 1996-2001 Takuya Ooura
// SPDX-FileCopyrightText: 2015 The WebRTC project authors
// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT AND BSD-3-Clause AND LicenseRef-Ooura

//nolint:varnamelen,dupl,nestif // The code of fft4g.cc, which repeats parts of ooura_fft.cc.
package ooura

import "math"

// Rdft matches WebRtc_rdft of fft_size_256/fft4g.cc: it computes in place
// the real FFT of length n of a, n being a power of two, with isgn >= 0,
// and its inverse, scaled by n/2, with isgn < 0. ip, of length at least
// 2+sqrt(n/2), and w, of length n/2, hold the work area and the cos/sin
// table, which are computed on the first call, with ip[0] set to 0.
func Rdft(n, isgn int, a []float32, ip []int, w []float32) {
	nw := ip[0]
	if n > nw<<2 {
		nw = n >> 2
		makewt(nw, ip, w)
	}
	nc := ip[1]
	if n > nc<<2 {
		nc = n >> 2
		makect(nc, ip, w[nw:])
	}
	if isgn >= 0 {
		if n > 4 {
			bitrv2(n, ip[2:], a)
			cftfsub(n, a, w)
			rftfsub(n, a, nc, w[nw:])
		} else if n == 4 {
			cftfsub(n, a, w)
		}
		xi := a[0] - a[1]
		a[0] += a[1]
		a[1] = xi
	} else {
		a[1] = 0.5 * (a[0] - a[1])
		a[0] -= a[1]
		if n > 4 {
			rftbsub(n, a, nc, w[nw:])
			bitrv2(n, ip[2:], a)
			cftbsub(n, a, w)
		} else if n == 4 {
			cftfsub(n, a, w)
		}
	}
}

// cosOf and sinOf match the cos and sin of the double precision C library
// on a float argument, cast back to float.
func cosOf(x float32) float32 { return float32(math.Cos(float64(x))) }
func sinOf(x float32) float32 { return float32(math.Sin(float64(x))) }

// atanOne matches atanf(1.0f).
func atanOne() float32 { return float32(math.Atan(1)) }

func makewt(nw int, ip []int, w []float32) {
	ip[0] = nw
	ip[1] = 1
	if nw > 2 {
		nwh := nw >> 1
		delta := atanOne() / float32(nwh)
		w[0] = 1
		w[1] = 0
		w[nwh] = cosOf(delta * float32(nwh))
		w[nwh+1] = w[nwh]
		if nwh > 2 {
			for j := 2; j < nwh; j += 2 {
				x := cosOf(delta * float32(j))
				y := sinOf(delta * float32(j))
				w[j] = x
				w[j+1] = y
				w[nw-j] = y
				w[nw-j+1] = x
			}
			bitrv2(nw, ip[2:], w)
		}
	}
}

func makect(nc int, ip []int, c []float32) {
	ip[1] = nc
	if nc > 1 {
		nch := nc >> 1
		delta := atanOne() / float32(nch)
		c[0] = cosOf(delta * float32(nch))
		c[nch] = 0.5 * c[0]
		for j := 1; j < nch; j++ {
			c[j] = 0.5 * cosOf(delta*float32(j))
			c[nc-j] = 0.5 * sinOf(delta*float32(j))
		}
	}
}

func bitrv2(n int, ip []int, a []float32) {
	swap := func(j1, k1 int) {
		a[j1], a[k1] = a[k1], a[j1]
		a[j1+1], a[k1+1] = a[k1+1], a[j1+1]
	}
	ip[0] = 0
	l := n
	m := 1
	for m<<3 < l {
		l >>= 1
		for j := range m {
			ip[m+j] = ip[j] + l
		}
		m <<= 1
	}
	m2 := 2 * m
	if m<<3 == l {
		for k := range m {
			for j := range k {
				j1 := 2*j + ip[k]
				k1 := 2*k + ip[j]
				swap(j1, k1)
				j1 += m2
				k1 += 2 * m2
				swap(j1, k1)
				j1 += m2
				k1 -= m2
				swap(j1, k1)
				j1 += m2
				k1 += 2 * m2
				swap(j1, k1)
			}
			j1 := 2*k + m2 + ip[k]
			k1 := j1 + m2
			swap(j1, k1)
		}
	} else {
		for k := 1; k < m; k++ {
			for j := range k {
				j1 := 2*j + ip[k]
				k1 := 2*k + ip[j]
				swap(j1, k1)
				j1 += m2
				k1 += m2
				swap(j1, k1)
			}
		}
	}
}

func cftfsub(n int, a, w []float32) {
	l := 2
	if n > 8 {
		cft1st(n, a, w)
		l = 8
		for l<<2 < n {
			cftmdl(n, l, a, w)
			l <<= 2
		}
	}
	if l<<2 == n {
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
	} else {
		for j := 0; j < l; j += 2 {
			j1 := j + l
			x0r := a[j] - a[j1]
			x0i := a[j+1] - a[j1+1]
			a[j] += a[j1]
			a[j+1] += a[j1+1]
			a[j1] = x0r
			a[j1+1] = x0i
		}
	}
}

func cftbsub(n int, a, w []float32) {
	l := 2
	if n > 8 {
		cft1st(n, a, w)
		l = 8
		for l<<2 < n {
			cftmdl(n, l, a, w)
			l <<= 2
		}
	}
	if l<<2 == n {
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
	} else {
		for j := 0; j < l; j += 2 {
			j1 := j + l
			x0r := a[j] - a[j1]
			x0i := -a[j+1] + a[j1+1]
			a[j] += a[j1]
			a[j+1] = -a[j+1] - a[j1+1]
			a[j1] = x0r
			a[j1+1] = x0i
		}
	}
}

func cft1st(n int, a, w []float32) {
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
	wk1r = w[2]
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
		wk2r = w[k1]
		wk2i = w[k1+1]
		wk1r = w[k2]
		wk1i = w[k2+1]
		wk3r = wk1r - 2*wk2i*wk1i
		wk3i = 2*wk2i*wk1r - wk1i
		x0r = a[j] + a[j+2]
		x0i = a[j+1] + a[j+3]
		x1r = a[j] - a[j+2]
		x1i = a[j+1] - a[j+3]
		x2r = a[j+4] + a[j+6]
		x2i = a[j+5] + a[j+7]
		x3r = a[j+4] - a[j+6]
		x3i = a[j+5] - a[j+7]
		a[j] = x0r + x2r
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
		wk1r = w[k2+2]
		wk1i = w[k2+3]
		wk3r = wk1r - 2*wk2r*wk1i
		wk3i = 2*wk2r*wk1r - wk1i
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

func cftmdl(n, l int, a, w []float32) {
	var wk1r, wk1i, wk2r, wk2i, wk3r, wk3i float32
	var x0r, x0i, x1r, x1i, x2r, x2i, x3r, x3i float32

	m := l << 2
	for j := 0; j < l; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r = a[j] + a[j1]
		x0i = a[j+1] + a[j1+1]
		x1r = a[j] - a[j1]
		x1i = a[j+1] - a[j1+1]
		x2r = a[j2] + a[j3]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2] - a[j3]
		x3i = a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i + x2i
		a[j2] = x0r - x2r
		a[j2+1] = x0i - x2i
		a[j1] = x1r - x3i
		a[j1+1] = x1i + x3r
		a[j3] = x1r + x3i
		a[j3+1] = x1i - x3r
	}
	wk1r = w[2]
	for j := m; j < l+m; j += 2 {
		j1 := j + l
		j2 := j1 + l
		j3 := j2 + l
		x0r = a[j] + a[j1]
		x0i = a[j+1] + a[j1+1]
		x1r = a[j] - a[j1]
		x1i = a[j+1] - a[j1+1]
		x2r = a[j2] + a[j3]
		x2i = a[j2+1] + a[j3+1]
		x3r = a[j2] - a[j3]
		x3i = a[j2+1] - a[j3+1]
		a[j] = x0r + x2r
		a[j+1] = x0i + x2i
		a[j2] = x2i - x0i
		a[j2+1] = x0r - x2r
		x0r = x1r - x3i
		x0i = x1i + x3r
		a[j1] = wk1r * (x0r - x0i)
		a[j1+1] = wk1r * (x0r + x0i)
		x0r = x3i + x1r
		x0i = x3r - x1i
		a[j3] = wk1r * (x0i - x0r)
		a[j3+1] = wk1r * (x0i + x0r)
	}
	k1 := 0
	m2 := 2 * m
	for k := m2; k < n; k += m2 {
		k1 += 2
		k2 := 2 * k1
		wk2r = w[k1]
		wk2i = w[k1+1]
		wk1r = w[k2]
		wk1i = w[k2+1]
		wk3r = wk1r - 2*wk2i*wk1i
		wk3i = 2*wk2i*wk1r - wk1i
		for j := k; j < l+k; j += 2 {
			j1 := j + l
			j2 := j1 + l
			j3 := j2 + l
			x0r = a[j] + a[j1]
			x0i = a[j+1] + a[j1+1]
			x1r = a[j] - a[j1]
			x1i = a[j+1] - a[j1+1]
			x2r = a[j2] + a[j3]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2] - a[j3]
			x3i = a[j2+1] - a[j3+1]
			a[j] = x0r + x2r
			a[j+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2] = wk2r*x0r - wk2i*x0i
			a[j2+1] = wk2r*x0i + wk2i*x0r
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1] = wk1r*x0r - wk1i*x0i
			a[j1+1] = wk1r*x0i + wk1i*x0r
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3] = wk3r*x0r - wk3i*x0i
			a[j3+1] = wk3r*x0i + wk3i*x0r
		}
		wk1r = w[k2+2]
		wk1i = w[k2+3]
		wk3r = wk1r - 2*wk2r*wk1i
		wk3i = 2*wk2r*wk1r - wk1i
		for j := k + m; j < l+(k+m); j += 2 {
			j1 := j + l
			j2 := j1 + l
			j3 := j2 + l
			x0r = a[j] + a[j1]
			x0i = a[j+1] + a[j1+1]
			x1r = a[j] - a[j1]
			x1i = a[j+1] - a[j1+1]
			x2r = a[j2] + a[j3]
			x2i = a[j2+1] + a[j3+1]
			x3r = a[j2] - a[j3]
			x3i = a[j2+1] - a[j3+1]
			a[j] = x0r + x2r
			a[j+1] = x0i + x2i
			x0r -= x2r
			x0i -= x2i
			a[j2] = -wk2i*x0r - wk2r*x0i
			a[j2+1] = -wk2i*x0i + wk2r*x0r
			x0r = x1r - x3i
			x0i = x1i + x3r
			a[j1] = wk1r*x0r - wk1i*x0i
			a[j1+1] = wk1r*x0i + wk1i*x0r
			x0r = x1r + x3i
			x0i = x1i - x3r
			a[j3] = wk3r*x0r - wk3i*x0i
			a[j3+1] = wk3r*x0i + wk3i*x0r
		}
	}
}

func rftfsub(n int, a []float32, nc int, c []float32) {
	m := n >> 1
	ks := 2 * nc / m
	kk := 0
	for j := 2; j < m; j += 2 {
		k := n - j
		kk += ks
		wkr := 0.5 - c[nc-kk]
		wki := c[kk]
		xr := a[j] - a[k]
		xi := a[j+1] + a[k+1]
		yr := wkr*xr - wki*xi
		yi := wkr*xi + wki*xr
		a[j] -= yr
		a[j+1] -= yi
		a[k] += yr
		a[k+1] -= yi
	}
}

func rftbsub(n int, a []float32, nc int, c []float32) {
	a[1] = -a[1]
	m := n >> 1
	ks := 2 * nc / m
	kk := 0
	for j := 2; j < m; j += 2 {
		k := n - j
		kk += ks
		wkr := 0.5 - c[nc-kk]
		wki := c[kk]
		xr := a[j] - a[k]
		xi := a[j+1] + a[k+1]
		yr := wkr*xr + wki*xi
		yi := wkr*xi - wki*xr
		a[j] -= yr
		a[j+1] = yi - a[j+1]
		a[k] += yr
		a[k+1] = yi - a[k+1]
	}
	a[m+1] = -a[m+1]
}
