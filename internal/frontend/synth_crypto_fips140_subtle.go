// SYNTHETIC (Principle 4) — crypto/internal/fips140/subtle: synthetic-only support package (not in the user import allow-list); exists to serve the synthetic packages importing it.
// RETIRES WHEN: the synthetic packages importing it retire — then delete this file.
// RE-TEST TRIGGER: re-evaluate when its dependents retire (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/subtle' package.
const syntheticCryptoFips140SubtleSource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/subtle'.
// go1.27.1 vendored constant_time.go. Synthetic-only.
package subtle

import (
	"crypto/internal/constanttime"
	"crypto/internal/fips140deps/byteorder"
	"math/bits"
	"crypto/internal/fips140/alias"
)


// ---- vendored from crypto/internal/fips140/subtle/constant_time.go ----




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

	return constanttime.ByteEq(v, 0)
}

// ConstantTimeLessOrEqBytes returns 1 if x <= y and 0 otherwise. The comparison
// is lexicographical, or big-endian. The time taken is a function of the length of
// the slices and is independent of the contents. If the lengths of x and y do not
// match it returns 0 immediately.
func ConstantTimeLessOrEqBytes(x, y []byte) int {
	if len(x) != len(y) {
		return 0
	}

	// Do a constant time subtraction chain y - x.
	// If there is no borrow at the end, then x <= y.
	var b uint64
	for len(x) > 8 {
		x0 := byteorder.BEUint64(x[len(x)-8:])
		y0 := byteorder.BEUint64(y[len(y)-8:])
		_, b = bits.Sub64(y0, x0, b)
		x = x[:len(x)-8]
		y = y[:len(y)-8]
	}
	if len(x) > 0 {
		xb := make([]byte, 8)
		yb := make([]byte, 8)
		copy(xb[8-len(x):], x)
		copy(yb[8-len(y):], y)
		x0 := byteorder.BEUint64(xb)
		y0 := byteorder.BEUint64(yb)
		_, b = bits.Sub64(y0, x0, b)
	}
	return int(b ^ 1)
}

// ConstantTimeCopy copies the contents of y into x (a slice of equal length)
// if v == 1. If v == 0, x is left unchanged. Its behavior is undefined if v
// takes any other value.
func ConstantTimeCopy(v int, x, y []byte) {
	if len(x) != len(y) {
		panic("subtle: slices have different lengths")
	}

	xmask := byte(v - 1)
	ymask := byte(^(v - 1))
	for i := 0; i < len(x); i++ {
		x[i] = x[i]&xmask | y[i]&ymask
	}
}


// ---- vendored from crypto/internal/fips140/subtle/xor.go ----



// XORBytes sets dst[i] = x[i] ^ y[i] for all i < n = min(len(x), len(y)),
// returning n, the number of bytes written to dst.
//
// If dst does not have length at least n,
// XORBytes panics without writing anything to dst.
//
// dst and x or y may overlap exactly or not at all,
// otherwise XORBytes may panic.
func XORBytes(dst, x, y []byte) int {
	n := min(len(x), len(y))
	if n == 0 {
		return 0
	}
	if n > len(dst) {
		panic("subtle.XORBytes: dst too short")
	}
	if alias.InexactOverlap(dst[:n], x[:n]) || alias.InexactOverlap(dst[:n], y[:n]) {
		panic("subtle.XORBytes: invalid overlap")
	}
	// P3-M9: pure-Go loop (no unsafe/& on index exprs in frontend/v1).
	for i := 0; i < n; i++ {
		dst[i] = x[i] ^ y[i]
	}
	return n
}

`
