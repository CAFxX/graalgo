package main

// P3-M8 (wave 2): synthetic `subtle` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticSubtleSource = `package subtle

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package subtle implements functions that are often useful in cryptographic
// code but require careful thought to use correctly.
//
// P3-M8: software implementation only (no intrinsification; Phase 5+).
// ConstantTime* are ported from crypto/internal/constanttime (the
// boolToUint8 intrinsic is spelled out; XORBytes is a plain loop with the
// exact panic checks). KAT: differential vs gc on the corpus vectors.

// boolToUint8 returns 1 for true and 0 for false. (The stdlib intrinsicifies
// this; the software spelling is semantically identical.)
func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// ConstantTimeCompare returns 1 if the two slices, x and y, have equal contents
// and 0 otherwise. The time taken is a function of the length of the slices and
// is independent of the contents. If the lengths of x and y do not match it
// returns 0 immediately.
func ConstantTimeCompare(x, y []byte) int {
	if len(x) != len(y) {
		return 0
	}
	var v byte
	for i := 0; i < len(x); i++ {
		v |= x[i] ^ y[i]
	}
	if v == 0 {
		return 1
	}
	return 0
}

// ConstantTimeSelect returns x if v == 1 and y if v == 0.
// Its behavior is undefined if v takes any other value.
func ConstantTimeSelect(v, x, y int) int {
	v = int(boolToUint8(v != 0))
	return ^(v-1)&x | (v-1)&y
}

// ConstantTimeByteEq returns 1 if x == y and 0 otherwise.
func ConstantTimeByteEq(x, y uint8) int {
	return int(boolToUint8(x == y))
}

// ConstantTimeEq returns 1 if x == y and 0 otherwise.
func ConstantTimeEq(x, y int32) int {
	return int(boolToUint8(x == y))
}

// ConstantTimeCopy copies the contents of y into x (a slice of equal length)
// if v == 1. If v == 0, x is left unchanged. Its behavior is undefined if v
// takes any other value.
func ConstantTimeCopy(v int, x, y []byte) {
	x = x[:len(y)]
	for i := 0; i < len(y); i++ {
		x[i] = uint8(ConstantTimeSelect(v, int(y[i]), int(x[i])))
	}
}

// ConstantTimeLessOrEq returns 1 if x <= y and 0 otherwise.
// Its behavior is undefined if x or y are negative or > 2**31 - 1.
func ConstantTimeLessOrEq(x, y int) int {
	return int(boolToUint8(x <= y))
}

// inexactOverlap reports whether x and y share memory at a non-zero offset,
// matching crypto/internal/fips140/alias.InexactOverlap.
//
// P3-M8b: the &x[0] == &y[0] fast path is not expressible in frontend/v1
// (WS D: & on index expressions is rejected), and the remaining case
// (different starts: disjoint or inexactly overlapping) needs the unsafe
// handle table to distinguish. We conservatively report no overlap — a
// divergence from gc only for caller-bug cases (partial overlap), where
// gc panics and we silently proceed.
// Re-test trigger: byte-addressed value model (D5 re-test).
func inexactOverlap(x, y []byte) bool {
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	return false
}

// XORBytes sets dst[i] = x[i] ^ y[i] for all i < n = min(len(x), len(y)),
// returning n, the number of bytes written to dst.
//
// If dst does not have length at least n,
// XORBytes panics without writing anything to dst.
//
// dst and x or y may overlap exactly or not at all,
// otherwise XORBytes may panic.
func XORBytes(dst, x, y []byte) int {
	n := len(x)
	if len(y) < n {
		n = len(y)
	}
	if n == 0 {
		return 0
	}
	if n > len(dst) {
		panic("subtle.XORBytes: dst too short")
	}
	if inexactOverlap(dst[:n], x[:n]) || inexactOverlap(dst[:n], y[:n]) {
		panic("subtle.XORBytes: invalid overlap")
	}
	for i := 0; i < n; i++ {
		dst[i] = x[i] ^ y[i]
	}
	return n
}
`
