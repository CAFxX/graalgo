// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptopbkdf2 for this package's root cause and retires-when trigger (crypto/pbkdf2).
package main

// P3-M9 tier-5: synthetic 'crypto/pbkdf2' package.
const syntheticCryptoPbkdf2Source = `// P3-M9 tier-5: synthetic 'crypto/pbkdf2' package.
// go1.27.1 vendored source (pbkdf2.go). No adaptations.
package pbkdf2

import (
	"crypto/internal/fips140/pbkdf2"
	"crypto/internal/fips140hash"
	"crypto/internal/fips140only"
	"errors"
	"hash"
)


// ---- vendored from crypto/pbkdf2/pbkdf2.go ----




// Key derives a key from the password, salt and iteration count, returning a
// []byte of length keyLength that can be used as cryptographic key. The key is
// derived based on the method described as PBKDF2 with the HMAC variant using
// the supplied hash function.
//
// For example, to use a HMAC-SHA-1 based PBKDF2 key derivation function, you
// can get a derived key for e.g. AES-256 (which needs a 32-byte key) by
// doing:
//
//	dk, err := pbkdf2.Key(sha1.New, "some password", salt, 4096, 32)
//
// Remember to get a good random salt. At least 8 bytes is recommended by the
// RFC.
//
// Using a higher iteration count will increase the cost of an exhaustive
// search but will also make derivation proportionally slower.
//
// keyLength must be a positive integer between 1 and (2^32 - 1) * h.Size().
// Setting keyLength to a value outside of this range will result in an error.
type __mrKeyPair struct {
	v0 []byte
	v1 error
}

func __mrKey[Hash hash.Hash](h func() Hash, password string, salt []byte, iter, keyLength int) __mrKeyPair {
	fh := fips140hash.UnwrapNew(h)
	if fips140only.Enforced() {
		if keyLength < 112/8 {
			return __mrKeyPair{nil, errors.New("crypto/pbkdf2: use of keys shorter than 112 bits is not allowed in FIPS 140-only mode")}
		}
		if len(salt) < 128/8 {
			return __mrKeyPair{nil, errors.New("crypto/pbkdf2: use of salts shorter than 128 bits is not allowed in FIPS 140-only mode")}
		}
		if !fips140only.ApprovedHash(fh()) {
			return __mrKeyPair{nil, errors.New("crypto/pbkdf2: use of hash functions other than SHA-2 or SHA-3 is not allowed in FIPS 140-only mode")}
		}
	}
	__fw0, __fw1 := pbkdf2.Key(fh, password, salt, iter, keyLength)
	return __mrKeyPair{__fw0, __fw1}
}

func Key[Hash hash.Hash](h func() Hash, password string, salt []byte, iter, keyLength int) ([]byte, error) {
	panic("crypto/pbkdf2: Key: multi-result desugar")
}

`
