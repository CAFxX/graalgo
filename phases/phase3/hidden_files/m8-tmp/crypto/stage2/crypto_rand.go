package rand

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rand implements a cryptographically secure random number generator.
//
// P3-M8: fail-fast (no entropy source). The frozen graalgo.os.* table
// (os.md §2.4) contains NO entropy symbol, and syscall.md has none either.
// Reader/Read/Text fail fast LOUDLY at the fillEntropy choke point.
// Int/Prime are omitted (signatures require math/big, deferred to M9+).
// Re-test triggers: (1) entropy symbol lands in graalgo.os.*; (2) math/big lands.

import "io"

// Reader is a global, shared instance of a cryptographically secure random
// number generator. P3-M8: fail-fast (see package doc).
var Reader io.Reader = &reader{}

type reader struct{}

func (r *reader) Read(b []byte) (n int, err error) {
	panic("crypto/rand: Read: no entropy source (re-test: graalgo.os entropy symbol)")
}

// readPair is the single-result form of Read's results.
type readPair struct {
	n   int
	err error
}

// __randRead serves rand.Read through the mrM8 desugar.
func __randRead(b []byte) readPair {
	panic("crypto/rand: Read: no entropy source (re-test: graalgo.os entropy symbol)")
}

// Read generates len(b) random bytes from Reader.
//
// P3-M8: fail-fast stub (exact gc signature). Re-test trigger: entropy.
func Read(b []byte) (n int, err error) {
	panic("crypto/rand: Read: no entropy source (re-test: graalgo.os entropy symbol)")
}

// Text returns a string of 32 random characters from [a-zA-Z0-9].
//
// P3-M8: fail-fast (needs entropy). Re-test trigger: entropy.
func Text() string {
	panic("crypto/rand: Text: no entropy source (re-test: graalgo.os entropy symbol)")
}

// Int and Prime are OMITTED in P3-M8: their signatures require math/big
// (rand.Int(rand *big.Int), rand.Prime(rand int)), deferred to M9+
// (re-test: math/big lands). They are not stubbed because a stub with the
// wrong signature would be worse than absence; callers get a clean
// "undefined" at type-check time.
