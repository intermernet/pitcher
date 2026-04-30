//go:build goexperiment.simd

package algos

import (
	"math"
	"simd/archsimd"
)

// zeroFloat64s sets all elements of s to zero using SIMD.
func zeroFloat64s(s []float64) {
	zero := archsimd.BroadcastFloat64x4(0)
	i := 0
	for ; i+4 <= len(s); i += 4 {
		zero.StoreSlice(s[i:])
	}
	for ; i < len(s); i++ {
		s[i] = 0
	}
}

// copyFloat64s copies elements from src to dst using SIMD.
// dst and src must not overlap in a way that dst is ahead of src.
func copyFloat64s(dst, src []float64) {
	i := 0
	for ; i+4 <= len(src); i += 4 {
		archsimd.LoadFloat64x4Slice(src[i:]).StoreSlice(dst[i:])
	}
	for ; i < len(src); i++ {
		dst[i] = src[i]
	}
}

// mulFloat64s computes dst[i] = a[i] * b[i] using SIMD.
func mulFloat64s(dst, a, b []float64) {
	i := 0
	for ; i+4 <= len(a); i += 4 {
		va := archsimd.LoadFloat64x4Slice(a[i:])
		vb := archsimd.LoadFloat64x4Slice(b[i:])
		va.Mul(vb).StoreSlice(dst[i:])
	}
	for ; i < len(a); i++ {
		dst[i] = a[i] * b[i]
	}
}

// mulAddFloat64s computes acc[i] += a[i] * b[i] using SIMD.
func mulAddFloat64s(acc, a, b []float64) {
	i := 0
	for ; i+4 <= len(acc); i += 4 {
		vacc := archsimd.LoadFloat64x4Slice(acc[i:])
		va := archsimd.LoadFloat64x4Slice(a[i:])
		vb := archsimd.LoadFloat64x4Slice(b[i:])
		va.Mul(vb).Add(vacc).StoreSlice(acc[i:])
	}
	for ; i < len(acc); i++ {
		acc[i] += a[i] * b[i]
	}
}

// computeMagnitudes computes dst[i] = 2 * sqrt(re[i]*re[i] + im[i]*im[i]) using SIMD.
func computeMagnitudes(dst, re, im []float64) {
	two := archsimd.BroadcastFloat64x4(2)
	i := 0
	for ; i+4 <= len(dst); i += 4 {
		vr := archsimd.LoadFloat64x4Slice(re[i:])
		vi := archsimd.LoadFloat64x4Slice(im[i:])
		rSq := vr.Mul(vr)
		iSq := vi.Mul(vi)
		two.Mul(rSq.Add(iSq).Sqrt()).StoreSlice(dst[i:])
	}
	for ; i < len(dst); i++ {
		r, m := re[i], im[i]
		dst[i] = 2 * math.Sqrt(r*r+m*m)
	}
}
