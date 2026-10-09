package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/drbg' package.
const syntheticCryptoFips140DrbgSource = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/drbg' stub.
// The real package is a CTR-DRBG (AES-based) seeded from OS entropy.
// This stub reads directly from the graalgo.os.getrandom intrinsic
// (same entropy source; the DRBG construction is an implementation
// detail invisible to callers). Synthetic-only.
package drbg

import "io"

//go:linkname graalgoOsGetrandom graalgo.os.getrandom
func graalgoOsGetrandom(b []byte) int64

// Read fills b with cryptographically secure random bytes.
func Read(b []byte) {
	if len(b) == 0 {
		return
	}
	graalgoOsGetrandom(b)
}

// ReadWithReader uses r to fill b with random bytes. If r is nil or
// the default reader, it uses Read.
func ReadWithReader(r io.Reader, b []byte) error {
	if r == nil || IsDefaultReader(r) {
		Read(b)
		return nil
	}
	_, err := io.ReadFull(r, b)
	return err
}

// DefaultReader is the default random reader type.
type DefaultReader struct{}

func (d DefaultReader) defaultReader() {}

// IsDefaultReader reports whether r embeds the DefaultReader type.
func IsDefaultReader(r io.Reader) bool {
	_, ok := r.(interface{ defaultReader() })
	return ok
}

`
