//go:build !goexperiment.simd

package algos

import "math"

// zeroFloat64s sets all elements of s to zero.
func zeroFloat64s(s []float64) {
	for i := range s {
		s[i] = 0
	}
}

// copyFloat64s copies elements from src to dst.
func copyFloat64s(dst, src []float64) {
	copy(dst, src)
}

// mulFloat64s computes dst[i] = a[i] * b[i].
func mulFloat64s(dst, a, b []float64) {
	for i := range a {
		dst[i] = a[i] * b[i]
	}
}

// mulAddFloat64s computes acc[i] += a[i] * b[i].
func mulAddFloat64s(acc, a, b []float64) {
	for i := range acc {
		acc[i] += a[i] * b[i]
	}
}

// computeMagnitudes computes dst[i] = 2 * sqrt(re[i]*re[i] + im[i]*im[i]).
func computeMagnitudes(dst, re, im []float64) {
	for i := range dst {
		r, m := re[i], im[i]
		dst[i] = 2 * math.Sqrt(r*r+m*m)
	}
}

// mulScalarAddFloat64s computes acc[i] += src[i] * scalar.
func mulScalarAddFloat64s(acc, src []float64, scalar float64) {
	for i := range acc {
		acc[i] += src[i] * scalar
	}
}
