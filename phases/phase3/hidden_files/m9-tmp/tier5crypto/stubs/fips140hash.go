// P3-M9 tier-5: synthetic `crypto/internal/fips140hash` stub.
// The real package unwraps crypto/sha3's FIPS-140 wrapper to reach the
// inner hash (via //go:linkname, not in frontend/v1 scope). The
// synthetic crypto/sha3 IS the inner implementation, so Unwrap is the
// identity. Synthetic-only.
package fips140hash

import "hash"

// Unwrap returns h unchanged (synthetic crypto/sha3 needs no unwrapping).
func Unwrap(h hash.Hash) hash.Hash { return h }

// UnwrapNew returns a function that calls newHash and applies Unwrap.
func UnwrapNew[Hash hash.Hash](newHash func() Hash) func() hash.Hash {
	return func() hash.Hash { return Unwrap(newHash()) }
}
