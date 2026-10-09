package main

// P3-M8 (wave 2): synthetic `rand` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticRandSource = `package rand

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rand implements a cryptographically secure random number generator.
//
// P3-M8: Reader/Read/Text are REAL via the "graalgo.os.getrandom"
// intrinsic (java.security.SecureRandom; added to the frozen table
// post-analysis, coordinator decision — the table had no entropy symbol).
// Int/Prime are omitted (signatures require math/big, deferred to M9+).
// Re-test trigger: math/big lands (Int/Prime).

import (
	"errors"
	"io"
	"math/big"
)

//go:linkname graalgoOsGetrandom graalgo.os.getrandom
func graalgoOsGetrandom(b []byte) int64

// Reader is a global, shared instance of a cryptographically secure random
// number generator.
var Reader io.Reader = &reader{}

type reader struct{}

func (r *reader) Read(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(graalgoOsGetrandom(b)), nil
}

// readPair is the single-result form of Read's results.
type readPair struct {
	n   int
	err error
}

// __randRead serves rand.Read through the mrM8 desugar.
func __randRead(b []byte) readPair {
	if len(b) == 0 {
		return readPair{}
	}
	return readPair{n: int(graalgoOsGetrandom(b))}
}

// Read generates len(b) random bytes from Reader.
func Read(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(graalgoOsGetrandom(b)), nil
}

// Text returns a cryptographically random string using the standard RFC 4648
// base32 alphabet. Matches go1.27.1 crypto/rand/text.go exactly.
func Text() string {
	const base32alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	// ⌈log₃₂ 2¹²⁸⌉ = 26 chars
	src := make([]byte, 26)
	// P3-M8b: Read(src) as a bare statement is not desugarable (WS D);
	// call the single-result __-helper directly (same entropy source).
	__randRead(src)
	for i := range src {
		src[i] = base32alphabet[src[i]%32]
	}
	return string(src)
}

// Int and Prime are OMITTED in P3-M8: their signatures require math/big
// (rand.Int(rand *big.Int), rand.Prime(rand int)), deferred to M9+
// (re-test: math/big lands). They are not stubbed because a stub with the
// wrong signature would be worse than absence; callers get a clean
// "undefined" at type-check time.

// P3-M9 (tier-5b): Prime for RSA key generation (multi-result desugar).
type __mrPrimePair struct {
	v0 *big.Int
	v1 error
}

func __mrPrime(r io.Reader, bits int) __mrPrimePair {
	if bits < 2 {
		return __mrPrimePair{nil, errors.New("crypto/rand: prime size must be at least 2-bit")}
	}
	b := uint(bits % 8)
	if b == 0 {
		b = 8
	}
	bytes := make([]byte, (bits+7)/8)
	p := new(big.Int)
	for {
		if _, err := io.ReadFull(r, bytes); err != nil {
			return __mrPrimePair{nil, err}
		}
		bytes[0] &= uint8(int(1<<b) - 1)
		if b >= 2 {
			bytes[0] |= 3 << (b - 2)
		} else {
			bytes[0] |= 1
			if len(bytes) > 1 {
				bytes[1] |= 0x80
			}
		}
		bytes[len(bytes)-1] |= 1
		p.SetBytes(bytes)
		if p.ProbablyPrime(20) {
			return __mrPrimePair{p, nil}
		}
	}
}

func Prime(r io.Reader, bits int) (*big.Int, error) {
	panic("crypto/rand: Prime: multi-result desugar")
}
`
