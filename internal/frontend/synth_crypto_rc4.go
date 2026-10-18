// SYNTHETIC (Principle 4) — crypto/rc4: real go1.27.1 source does not compile under graalgo as-published (import closure hits go:linkname / asm / intrinsics / unsafe — B-4); pure-Go port.
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M8 (wave 2): synthetic `rc4` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticRc4Source = `package rc4

import (
	"strconv"
)

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rc4 implements RC4 encryption, as defined in Bruce Schneier's
// Applied Cryptography.
//
// RC4 is cryptographically broken and should not be used for secure
// applications.

// A Cipher is an instance of RC4 using a particular key.
type Cipher struct {
	s    [256]uint32
	i, j uint8
}

type KeySizeError int

func (k KeySizeError) Error() string {
	return "crypto/rc4: invalid key size " + strconv.Itoa(int(k))
}

// NewCipher creates and returns a new [Cipher]. The key argument should be the
// RC4 key, at least 1 byte and at most 256 bytes.
// NewCipher creates and returns a new [Cipher].
// P3-M8: fail-fast stub (exact gc signature). Real callers go through the
// mrM8 desugar to __rc4NewCipher. Re-test trigger: WS D lands.
func NewCipher(key []byte) (*Cipher, error) {
	panic("crypto/rc4: NewCipher: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Reset zeros the key data and makes the [Cipher] unusable.
//
// Deprecated: Reset can't guarantee that the key will be entirely removed from
// the process's memory.
func (c *Cipher) Reset() {
	for _m8ci := range c.s[:] {
		c.s[:][_m8ci] = 0
	}
	c.i, c.j = 0, 0
}

// XORKeyStream sets dst to the result of XORing src with the key stream.
// Dst and src must overlap entirely or not at all.
func (c *Cipher) XORKeyStream(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/rc4: invalid buffer overlap")
	}
	i, j := c.i, c.j
	_ = dst[len(src)-1]
	dst = dst[:len(src)] // eliminate bounds check from loop
	for k, v := range src {
		i += 1
		x := c.s[i]
		j += uint8(x)
		y := c.s[j]
		c.s[i], c.s[j] = y, x
		dst[k] = v ^ uint8(c.s[uint8(x+y)])
	}
	c.i, c.j = i, j
}

// inexactOverlap: see crypto/des (P3-M8 fail-fast without unsafe).
// P3-M8b: the &x[0] == &y[0] fast path is not expressible in frontend/v1
// (WS D); conservatively report no overlap (divergence from gc only for
// caller-bug partial-overlap cases). Re-test: D5 byte-addressed model.
func inexactOverlap(x, y []byte) bool {
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	return false
}

// newCipherPair is the single-result form of NewCipher's results.
type newCipherPair struct {
	cipher *Cipher
	err    error
}

// __rc4NewCipher serves rc4.NewCipher through the mrM8 desugar.
func __rc4NewCipher(key []byte) newCipherPair {
	k := len(key)
	if k < 1 || k > 256 {
		return newCipherPair{nil, KeySizeError(k)}
	}
	var c Cipher
	for i := 0; i < 256; i++ {
		c.s[i] = uint32(i)
	}
	var j uint8 = 0
	for i := 0; i < 256; i++ {
		j += uint8(c.s[i]) + key[i%k]
		c.s[i], c.s[j] = c.s[j], c.s[i]
	}
	return newCipherPair{&c, nil}
}
`
