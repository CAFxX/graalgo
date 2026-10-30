// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptointernalfips140only for this package's root cause and retires-when trigger (crypto/internal/fips140only).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140only' package.
const syntheticCryptoFips140onlySource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140only' stub.
// The real package reports whether FIPS 140-only mode is enforced.
// In default (non-FIPS) mode, Enforced is false. This stub pins that
// default; ApprovedHash mirrors the real type switch over the
// synthetic fips140 digests. Synthetic-only.
package fips140only

import (
	"crypto/internal/fips140/sha256"
	"crypto/internal/fips140/sha3"
	"crypto/internal/fips140/sha512"
	"hash"
	"io"
)

// Enforced reports whether FIPS 140-only mode is enforced. Always
// false here, matching gc's default.
func Enforced() bool { return false }

// ApprovedHash reports whether h is a FIPS-approved hash.
func ApprovedHash(h hash.Hash) bool {
	switch h.(type) {
	case *sha256.Digest, *sha512.Digest, *sha3.Digest:
		return true
	default:
		return false
	}
}

// ApprovedRandomReader reports whether r is a FIPS-approved random
// reader. Always false here (default mode).
func ApprovedRandomReader(r io.Reader) bool { return false }

`
