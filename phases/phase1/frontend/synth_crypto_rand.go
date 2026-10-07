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

import "io"

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
	Read(src)
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
`
