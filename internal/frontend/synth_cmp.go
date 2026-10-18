// SYNTHETIC (Principle 4) — cmp: real go1.27.1 source does not compile under graalgo as-published (import closure hits go:linkname / asm / intrinsics / unsafe — B-4); pure-Go port.
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (wave 3, tier-1a worker): synthetic `cmp` package.
//
// REAL Go source adapted from gc go1.27.1 ($GOROOT/src/cmp),
// embedded as text and type-checked on demand with the overlay importer.
// The resulting *types.Package is what overlayImporter.Import("cmp")
// returns; the exporter emits the synthetic declarations' bodies into
// the same wire document via exportSyntheticPackages.
//
// Verbatim copy of gc go1.27.1 cmp/cmp.go (self-contained,
// no imports). No intrinsics needed.
package main

// syntheticCmpSource is the P3-M9 synthetic `cmp` package.
const syntheticCmpSource = `package cmp
// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cmp provides types and functions related to comparing
// ordered values.

// Ordered is a constraint that permits any ordered type: any type
// that supports the operators < <= >= >.
// If future releases of Go add new ordered types,
// this constraint will be modified to include them.
//
// Note that floating-point types may contain NaN ("not-a-number") values.
// An operator such as == or < will always report false when
// comparing a NaN value with any other value, NaN or not.
// See the [Compare] function for a consistent way to compare NaN values.
type Ordered interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64 |
		~string
}

// Less reports whether x is less than y.
// For floating-point types, a NaN is considered less than any non-NaN,
// and -0.0 is not less than (is equal to) 0.0.
func Less[T Ordered](x, y T) bool {
	return (isNaN(x) && !isNaN(y)) || x < y
}

// Compare returns
//
//	-1 if x is less than y,
//	 0 if x equals y,
//	+1 if x is greater than y.
//
// For floating-point types, a NaN is considered less than any non-NaN,
// a NaN is considered equal to a NaN, and -0.0 is equal to 0.0.
func Compare[T Ordered](x, y T) int {
	xNaN := isNaN(x)
	yNaN := isNaN(y)
	if xNaN {
		if yNaN {
			return 0
		}
		return -1
	}
	if yNaN {
		return +1
	}
	if x < y {
		return -1
	}
	if x > y {
		return +1
	}
	return 0
}

// isNaN reports whether x is a NaN without requiring the math package.
// This will always return false if T is not floating-point.
func isNaN[T Ordered](x T) bool {
	return x != x
}

// Or returns the first of its arguments that is not equal to the zero value.
// If no argument is non-zero, it returns the zero value.
func Or[T comparable](vals ...T) T {
	var zero T
	for _, val := range vals {
		if val != zero {
			return val
		}
	}
	return zero
}
`
