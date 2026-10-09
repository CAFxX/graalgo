package main

// P3-M9 tier-5: synthetic 'crypto/internal/boring/bbig' package.
const syntheticCryptoBbigSource = `// P3-M9 tier-5: synthetic 'crypto/internal/boring/bbig' stub.
// The real package converts between *big.Int and boring.BigInt via
// unsafe.Slice (aliasing, not in frontend/v1 scope). This stub copies.
// All call sites are inside 'if boring.Enabled' branches (dead; the
// notboring leg is vendored), so the copy-vs-alias distinction is
// unobservable. Synthetic-only.
package bbig

import (
	"crypto/internal/boring"
	"math/big"
)

// Enc converts b to a boring.BigInt.
func Enc(b *big.Int) boring.BigInt {
	if b == nil {
		return nil
	}
	x := b.Bits()
	if len(x) == 0 {
		return boring.BigInt{}
	}
	out := make([]uint, len(x))
	for i, w := range x {
		out[i] = uint(w)
	}
	return out
}

// Dec converts b to a *big.Int.
func Dec(b boring.BigInt) *big.Int {
	if b == nil {
		return nil
	}
	if len(b) == 0 {
		return new(big.Int)
	}
	x := make([]big.Word, len(b))
	for i, w := range b {
		x[i] = big.Word(w)
	}
	return new(big.Int).SetBits(x)
}

`
