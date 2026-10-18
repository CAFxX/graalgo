// SYNTHETIC (Principle 4) — crypto/sha3: real go1.27.1 source does not compile under graalgo as-published (import closure hits go:linkname / asm / intrinsics / unsafe — B-4); pure-Go port.
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M8 (wave 2): synthetic `sha3` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticSha3Source = `package sha3

import (
	"bytes"
	"crypto"
	"errors"
	"hash"
)

// -- synthetic byteorder helpers (inlined from internal/byteorder) --
func boLEUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func boLEPutUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
func boLEUint64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
func boLEPutUint64(b []byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}
func boBEUint32(b []byte) uint32 {
	return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24
}
func boBEPutUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}
func boBEUint64(b []byte) uint64 {
	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}
func boBEPutUint64(b []byte, v uint64) {
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}
func boBEAppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func boBEAppendUint64(b []byte, v uint64) []byte {
	return append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func boLEAppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
func boLEAppendUint64(b []byte, v uint64) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}

// -- end synthetic byteorder helpers --

// -- synthetic math/bits helpers (inlined; the used operations only) --
// NB: the shift count is a SIGNED int, matching math/bits.RotateLeftN:
// callers pass negative counts (e.g. bits.RotateLeft32(x, -17)).
func rotl32(x uint32, n int) uint32 { return x<<uint(n&31) | x>>uint((32-n)&31) }
func rotl64(x uint64, n int) uint64 { return x<<uint(n&63) | x>>uint((64-n)&63) }
func add64(x, y, carry uint64) (sum, carryOut uint64) {
	sum1 := x + y
	c1 := uint64(0)
	if sum1 < x {
		c1 = 1
	}
	sum = sum1 + carry
	c2 := uint64(0)
	if sum < sum1 {
		c2 = 1
	}
	return sum, c1 | c2
}
func sub64(x, y, borrow uint64) (diff, borrowOut uint64) {
	diff1 := x - y
	b1 := uint64(0)
	if diff1 > x {
		b1 = 1
	}
	diff = diff1 - borrow
	b2 := uint64(0)
	if diff > diff1 {
		b2 = 1
	}
	return diff, b1 | b2
}
func len64(x uint64) int {
	n := 0
	for x != 0 {
		n++
		x >>= 1
	}
	return n
}

// -- end synthetic math/bits helpers --

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sha3 implements the SHA-3 fixed-output-length hash functions and
// the SHAKE variable-output-length functions defined by [FIPS 202], as well as
// the cSHAKE extendable-output-length functions defined by [SP 800-185].
//
// [FIPS 202]: https://doi.org/10.6028/NIST.FIPS.202
// [SP 800-185]: https://doi.org/10.6028/NIST.SP.800-185

// spongeDirection indicates the direction bytes are flowing through the sponge.
type spongeDirection int

const (
	// spongeAbsorbing indicates that the sponge is absorbing input.
	spongeAbsorbing spongeDirection = iota
	// spongeSqueezing indicates that the sponge is being squeezed.
	spongeSqueezing
)

type digest struct {
	a [1600 / 8]byte // main state of the hash

	// a[n:rate] is the buffer. If absorbing, it's the remaining space to XOR
	// into before running the permutation. If squeezing, it's the remaining
	// output to produce before running the permutation.
	n, rate int

	// dsbyte contains the "domain separation" bits and the first bit of
	// the padding. Sections 6.1 and 6.2 of [1] separate the outputs of the
	// SHA-3 and SHAKE functions by appending bitstrings to the message.
	// Using a little-endian bit-ordering convention, these are "01" for SHA-3
	// and "1111" for SHAKE, or 00000010b and 00001111b, respectively. Then the
	// padding rule from section 5.1 is applied to pad the message to a multiple
	// of the rate, which involves adding a "1" bit, zero or more "0" bits, and
	// a final "1" bit. We merge the first "1" bit from the padding into dsbyte,
	// giving 00000110b (0x06) and 00011111b (0x1f).
	// [1] http://csrc.nist.gov/publications/drafts/fips-202/fips_202_draft.pdf
	//     "Draft FIPS 202: SHA-3 Standard: Permutation-Based Hash and
	//      Extendable-Output Functions (May 2014)"
	dsbyte byte

	outputLen int             // the default output size in bytes
	state     spongeDirection // whether the sponge is absorbing or squeezing
}

// BlockSize returns the rate of sponge underlying this hash function.
func (d digest) BlockSize() int { return d.rate }

// Size returns the output size of the hash function in bytes.
func (d digest) Size() int { return d.outputLen }

// Reset resets the digest to its initial state.
func (d *digest) Reset() {
	// Zero the permutation's state.
	for _m8ci := range d.a[:] {
		d.a[:][_m8ci] = 0
	}
	d.state = spongeAbsorbing
	d.n = 0
}

func (d *digest) Clone() *digest {
	// P3-M8b: *d / &ret are not expressible in frontend/v1 (WS D);
	// &digest{...} on a composite literal is. Field-by-field copy
	// (arrays are values, so the copy is complete).
	return &digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}
}

// permute applies the KeccakF-1600 permutation.
func (d *digest) permute() {
	// P3-M8b: value semantics (frontend/v1 has no & on selectors, WS D).
	d.a = keccakF1600(d.a)
	d.n = 0
}

// padAndPermute appends the domain separation bits in dsbyte, applies
// the multi-bitrate 10..1 padding rule, and permutes the state.
func (d *digest) padAndPermute() {
	// Pad with this instance's domain-separator bits. We know that there's
	// at least one byte of space in the sponge because, if it were full,
	// permute would have been called to empty it. dsbyte also contains the
	// first one bit for the padding. See the comment in the state struct.
	d.a[d.n] ^= d.dsbyte
	// This adds the final one bit for the padding. Because of the way that
	// bits are numbered from the LSB upwards, the final bit is the MSB of
	// the last byte.
	d.a[d.rate-1] ^= 0x80
	// Apply the permutation
	d.permute()
	d.state = spongeSqueezing
}

// Write absorbs more data into the hash's state.
func (d *digest) WriteSingle(p []byte) int {
	if d.state != spongeAbsorbing {
		panic("sha3: Write after Read")
	}

	for len(p) > 0 {
		x := len(p)
		if x > d.rate-d.n {
			x = d.rate - d.n
		}
		for i := 0; i < x; i++ {
			d.a[d.n+i] ^= p[i]
		}
		d.n += x
		p = p[x:]

		// If the sponge is full, apply the permutation.
		if d.n == d.rate {
			d.permute()
		}
	}

	return len(p)
}

// read squeezes an arbitrary number of bytes from the sponge.
func (d *digest) ReadSingle(out []byte) int {
	// If we're still absorbing, pad and apply the permutation.
	if d.state == spongeAbsorbing {
		d.padAndPermute()
	}

	// Now, do the squeezing.
	for len(out) > 0 {
		// Apply the permutation if we've squeezed the sponge dry.
		if d.n == d.rate {
			d.permute()
		}

		x := copy(out, d.a[d.n:d.rate])
		d.n += x
		out = out[x:]
	}

	return len(out)
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (d *digest) Sum(b []byte) []byte {
	return d.sumSingle(b)
}

func (d *digest) sumSingle(b []byte) []byte {
	if d.state != spongeAbsorbing {
		panic("sha3: Sum after Read")
	}

	// Make a copy of the original hash so that caller can keep writing
	// and summing.
	dup := d.Clone()
	hash := make([]byte, dup.outputLen, 64) // explicit cap to allow stack allocation
	dup.ReadSingle(hash)
	return append(b, hash...)
}

const (
	magicSHA3   = "sha\x08"
	magicShake  = "sha\x09"
	magicCShake = "sha\x0a"
	magicKeccak = "sha\x0b"
	// magic || rate || main state || n || sponge direction
	marshaledSize = len(magicSHA3) + 1 + 200 + 1 + 1
)

func (d digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

func (d digest) AppendSingle(b []byte) []byte {
	switch d.dsbyte {
	case dsbyteSHA3:
		b = append(b, magicSHA3...)
	case dsbyteShake:
		b = append(b, magicShake...)
	case dsbyteCShake:
		b = append(b, magicCShake...)
	case dsbyteKeccak:
		b = append(b, magicKeccak...)
	default:
		panic("unknown dsbyte")
	}
	// rate is at most 168, and n is at most rate.
	b = append(b, byte(d.rate))
	b = append(b, d.a[:]...)
	b = append(b, byte(d.n), byte(d.state))
	return b
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) != marshaledSize {
		return errors.New("sha3: invalid hash state")
	}

	magic := string(b[:len(magicSHA3)])
	b = b[len(magicSHA3):]
	switch {
	case magic == magicSHA3 && d.dsbyte == dsbyteSHA3:
	case magic == magicShake && d.dsbyte == dsbyteShake:
	case magic == magicCShake && d.dsbyte == dsbyteCShake:
	case magic == magicKeccak && d.dsbyte == dsbyteKeccak:
	default:
		return errors.New("sha3: invalid hash state identifier")
	}

	rate := int(b[0])
	b = b[1:]
	if rate != d.rate {
		return errors.New("sha3: invalid hash state function")
	}

	copy(d.a[:], b)
	b = b[len(d.a):]

	n, state := int(b[0]), spongeDirection(b[1])
	if n > d.rate {
		return errors.New("sha3: invalid hash state")
	}
	d.n = n
	if state != spongeAbsorbing && state != spongeSqueezing {
		return errors.New("sha3: invalid hash state")
	}
	d.state = state

	return nil
}

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// New224 returns a new digest computing the SHA3-224 hash.
func newDigest224() *digest {
	return &digest{rate: rateK448, outputLen: 28, dsbyte: dsbyteSHA3}
}

// New256 returns a new digest computing the SHA3-256 hash.
func newDigest256() *digest {
	return &digest{rate: rateK512, outputLen: 32, dsbyte: dsbyteSHA3}
}

// New384 returns a new digest computing the SHA3-384 hash.
func newDigest384() *digest {
	return &digest{rate: rateK768, outputLen: 48, dsbyte: dsbyteSHA3}
}

// New512 returns a new digest computing the SHA3-512 hash.
func newDigest512() *digest {
	return &digest{rate: rateK1024, outputLen: 64, dsbyte: dsbyteSHA3}
}

const (
	dsbyteSHA3   = 0b00000110
	dsbyteKeccak = 0b00000001
	dsbyteShake  = 0b00011111
	dsbyteCShake = 0b00000100

	// rateK[c] is the rate in bytes for Keccak[c] where c is the capacity in
	// bits. Given the sponge size is 1600 bits, the rate is 1600 - c bits.
	rateK256  = (1600 - 256) / 8
	rateK448  = (1600 - 448) / 8
	rateK512  = (1600 - 512) / 8
	rateK768  = (1600 - 768) / 8
	rateK1024 = (1600 - 1024) / 8
)

// NewLegacyKeccak256 returns a new digest computing the legacy, non-standard
// Keccak-256 hash.
func newLegacyKeccak256() *digest {
	return &digest{rate: rateK512, outputLen: 32, dsbyte: dsbyteKeccak}
}

// NewLegacyKeccak512 returns a new digest computing the legacy, non-standard
// Keccak-512 hash.
func newLegacyKeccak512() *digest {
	return &digest{rate: rateK1024, outputLen: 64, dsbyte: dsbyteKeccak}
}

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

type shake struct {
	d digest // SHA-3 state context and Read/Write operations

	// initBlock is the cSHAKE specific initialization set of bytes. It is initialized
	// by newCShake function and stores concatenation of N followed by S, encoded
	// by the method specified in 3.3 of [1].
	// It is stored here in order for Reset() to be able to put context into
	// initial state.
	initBlock []byte
}

// P3-M8b: copyDigest/storeDigest work around frontend/v1 WS D limits (no
// & on selector, no * deref). Mutating digest methods have pointer
// receivers; s.d is a value, so call sites copy to a *digest, mutate,
// and store back field by field (arrays copy by value).
func copyDigest(d digest) *digest {
	return &digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}
}

func (s *shake) storeDigest(d *digest) {
	s.d.a = d.a
	s.d.n = d.n
	s.d.rate = d.rate
	s.d.dsbyte = d.dsbyte
	s.d.outputLen = d.outputLen
	s.d.state = d.state
}

func bytepadWrite(c *shake, data []byte, rate int) {
	rateEnc := leftEncode(uint64(rate))
	c.WriteSingle(rateEnc)
	c.WriteSingle(data)
	if padlen := rate - (len(rateEnc)+len(data))%rate; padlen < rate {
		const maxRate = rateK256
		c.WriteSingle(make([]byte, padlen, maxRate)) // explicit cap to allow stack allocation
	}
}

func leftEncode(x uint64) []byte {
	// Let n be the smallest positive integer for which 2^(8n) > x.
	n := (len64(x) + 7) / 8
	if n == 0 {
		n = 1
	}
	// Return n || x with n as a byte and x an n bytes in big-endian order.
	b := make([]byte, 9)
	boBEPutUint64(b[1:], x)
	b = b[9-n-1:]
	b[0] = byte(n)
	return b
}

func newCShake(c *shake, N, S []byte, rate, outputLen int, dsbyte byte) *shake {
	c.d = digest{rate: rate, outputLen: outputLen, dsbyte: dsbyte}
	c.initBlock = make([]byte, 0, 9+len(N)+9+len(S)) // leftEncode returns max 9 bytes
	c.initBlock = append(c.initBlock, leftEncode(uint64(len(N))*8)...)
	c.initBlock = append(c.initBlock, N...)
	c.initBlock = append(c.initBlock, leftEncode(uint64(len(S))*8)...)
	c.initBlock = append(c.initBlock, S...)
	bytepadWrite(c, c.initBlock, c.d.rate)
	return c
}

func (s *shake) BlockSize() int { return s.d.BlockSize() }
func (s *shake) Size() int      { return s.d.Size() }

// Sum appends a portion of output to b and returns the resulting slice. The
// output length is selected to provide full-strength generic security: 32 bytes
// for SHAKE128 and 64 bytes for SHAKE256. It does not change the underlying
// state. It panics if any output has already been read.
func (s *shake) Sum(in []byte) []byte {
	// P3-M8b: s.d is a value; Sum has a pointer receiver (WS D).
	// Sum is read-only (it clones), so a copy is exact.
	return copyDigest(s.d).Sum(in)
}

// Write absorbs more data into the hash's state.
// It panics if any output has already been read.
func (s *shake) WriteSingle(p []byte) int {
	// P3-M8b: s.d is a value; WriteSingle has a pointer receiver (WS D).
	d := copyDigest(s.d)
	n := d.WriteSingle(p)
	s.storeDigest(d)
	return n
}

func (s *shake) ReadSingle(out []byte) int {
	// P3-M8b: s.d is a value; ReadSingle has a pointer receiver (WS D).
	d := copyDigest(s.d)
	n := d.ReadSingle(out)
	s.storeDigest(d)
	return n
}

// Reset resets the hash to initial state.
func (s *shake) Reset() {
	// P3-M8b: s.d is a value; Reset has a pointer receiver (WS D).
	d := copyDigest(s.d)
	d.Reset()
	s.storeDigest(d)
	if len(s.initBlock) != 0 {
		bytepadWrite(s, s.initBlock, s.d.rate)
	}
}

// Clone returns a copy of the shake context in its current state.
func (s *shake) Clone() *shake {
	// P3-M8b: *s / &ret are not expressible in frontend/v1 (WS D);
	// &shake{...} on a composite literal is. The digest field copies
	// by value (its array copies completely); initBlock shares storage,
	// exactly as in the stdlib.
	return &shake{d: s.d, initBlock: s.initBlock}
}

func (s *shake) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize+len(s.initBlock)))
}

func (s *shake) AppendSingle(b []byte) []byte {
	b = s.d.AppendSingle(b)
	b = append(b, s.initBlock...)
	return b
}

func (s *shake) UnmarshalBinary(b []byte) error {
	if len(b) < marshaledSize {
		return errors.New("sha3: invalid hash state")
	}
	// P3-M8b: s.d is a value; UnmarshalBinary has a pointer receiver (WS D).
	d := copyDigest(s.d)
	if err := d.UnmarshalBinary(b[:marshaledSize]); err != nil {
		return err
	}
	s.storeDigest(d)
	s.initBlock = bytes.Clone(b[marshaledSize:])
	return nil
}

// NewShake128 creates a new SHAKE128 XOF.
func newShake128() *shake {
	return &shake{d: digest{rate: rateK256, outputLen: 32, dsbyte: dsbyteShake}}
}

// NewShake256 creates a new SHAKE256 XOF.
func newShake256() *shake {
	return &shake{d: digest{rate: rateK512, outputLen: 64, dsbyte: dsbyteShake}}
}

// NewCShake128 creates a new cSHAKE128 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewShake128.
func newCShake128(N, S []byte) *shake {
	// The actual logic is in a separate function to outline this allocation.
	c := &shake{}
	return newCShake128Inner(c, N, S)
}

func newCShake128Inner(c *shake, N, S []byte) *shake {
	if len(N) == 0 && len(S) == 0 {
		// P3-M8b: *c = *... is not expressible in frontend/v1 (WS D:
		// no StarExpr); copy the fresh state field by field.
		tmp := newShake128()
		c.d = tmp.d
		c.initBlock = tmp.initBlock
		return c
	}
	return newCShake(c, N, S, rateK256, 32, dsbyteCShake)
}

// NewCShake256 creates a new cSHAKE256 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewShake256.
func newCShake256(N, S []byte) *shake {
	// The actual logic is in a separate function to outline this allocation.
	c := &shake{}
	return newCShake256Inner(c, N, S)
}

func newCShake256Inner(c *shake, N, S []byte) *shake {
	if len(N) == 0 && len(S) == 0 {
		// P3-M8b: *c = *... is not expressible in frontend/v1 (WS D:
		// no StarExpr); copy the fresh state field by field.
		tmp := newShake256()
		c.d = tmp.d
		c.initBlock = tmp.initBlock
		return c
	}
	return newCShake(c, N, S, rateK512, 64, dsbyteCShake)
}

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// rc stores the round constants for use in the ι step.
var rc = [24]uint64{
	0x0000000000000001,
	0x0000000000008082,
	0x800000000000808A,
	0x8000000080008000,
	0x000000000000808B,
	0x0000000080000001,
	0x8000000080008081,
	0x8000000000008009,
	0x000000000000008A,
	0x0000000000000088,
	0x0000000080008009,
	0x000000008000000A,
	0x000000008000808B,
	0x800000000000008B,
	0x8000000000008089,
	0x8000000000008003,
	0x8000000000008002,
	0x8000000000000080,
	0x000000000000800A,
	0x800000008000000A,
	0x8000000080008081,
	0x8000000000008080,
	0x0000000080000001,
	0x8000000080008008,
}

// keccakF1600Generic applies the Keccak permutation.
// P3-M8b: value-semantics form — frontend/v1 rejects & on selectors
// (WS D), so the state array is taken and returned by value (arrays
// copy; semantics identical).
func keccakF1600Generic(da [200]byte) [200]byte {
	// P3-M8: always-copy rewrite of the stdlib's unsafe fast path. The
	// stdlib aliases da as *[25]uint64 via unsafe.Pointer on little-endian
	// hosts (perf-only); the v1 frontend has no unsafe, so we always copy
	// through a [25]uint64 exactly like the stdlib's big-endian path.
	// Output-identical on all hosts (covered by the SHA-3 KAT differential).
	a := new([25]uint64)
	for i := 0; i < 25; i++ {
		a[i] = boLEUint64(da[i*8:])
	}

	// Implementation translated from Keccak-inplace.c
	// in the keccak reference code.
	var t, bc0, bc1, bc2, bc3, bc4, d0, d1, d2, d3, d4 uint64

	for i := 0; i < 24; i += 4 {
		// Combines the 5 steps in each round into 2 steps.
		// Unrolls 4 rounds per loop and spreads some steps across rounds.

		// Round 1
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[6] ^ d1
		bc1 = rotl64(t, 44)
		t = a[12] ^ d2
		bc2 = rotl64(t, 43)
		t = a[18] ^ d3
		bc3 = rotl64(t, 21)
		t = a[24] ^ d4
		bc4 = rotl64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i]
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc2 = rotl64(t, 3)
		t = a[16] ^ d1
		bc3 = rotl64(t, 45)
		t = a[22] ^ d2
		bc4 = rotl64(t, 61)
		t = a[3] ^ d3
		bc0 = rotl64(t, 28)
		t = a[9] ^ d4
		bc1 = rotl64(t, 20)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc4 = rotl64(t, 18)
		t = a[1] ^ d1
		bc0 = rotl64(t, 1)
		t = a[7] ^ d2
		bc1 = rotl64(t, 6)
		t = a[13] ^ d3
		bc2 = rotl64(t, 25)
		t = a[19] ^ d4
		bc3 = rotl64(t, 8)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc1 = rotl64(t, 36)
		t = a[11] ^ d1
		bc2 = rotl64(t, 10)
		t = a[17] ^ d2
		bc3 = rotl64(t, 15)
		t = a[23] ^ d3
		bc4 = rotl64(t, 56)
		t = a[4] ^ d4
		bc0 = rotl64(t, 27)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc3 = rotl64(t, 41)
		t = a[21] ^ d1
		bc4 = rotl64(t, 2)
		t = a[2] ^ d2
		bc0 = rotl64(t, 62)
		t = a[8] ^ d3
		bc1 = rotl64(t, 55)
		t = a[14] ^ d4
		bc2 = rotl64(t, 39)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		// Round 2
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[16] ^ d1
		bc1 = rotl64(t, 44)
		t = a[7] ^ d2
		bc2 = rotl64(t, 43)
		t = a[23] ^ d3
		bc3 = rotl64(t, 21)
		t = a[14] ^ d4
		bc4 = rotl64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+1]
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc2 = rotl64(t, 3)
		t = a[11] ^ d1
		bc3 = rotl64(t, 45)
		t = a[2] ^ d2
		bc4 = rotl64(t, 61)
		t = a[18] ^ d3
		bc0 = rotl64(t, 28)
		t = a[9] ^ d4
		bc1 = rotl64(t, 20)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc4 = rotl64(t, 18)
		t = a[6] ^ d1
		bc0 = rotl64(t, 1)
		t = a[22] ^ d2
		bc1 = rotl64(t, 6)
		t = a[13] ^ d3
		bc2 = rotl64(t, 25)
		t = a[4] ^ d4
		bc3 = rotl64(t, 8)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc1 = rotl64(t, 36)
		t = a[1] ^ d1
		bc2 = rotl64(t, 10)
		t = a[17] ^ d2
		bc3 = rotl64(t, 15)
		t = a[8] ^ d3
		bc4 = rotl64(t, 56)
		t = a[24] ^ d4
		bc0 = rotl64(t, 27)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc3 = rotl64(t, 41)
		t = a[21] ^ d1
		bc4 = rotl64(t, 2)
		t = a[12] ^ d2
		bc0 = rotl64(t, 62)
		t = a[3] ^ d3
		bc1 = rotl64(t, 55)
		t = a[19] ^ d4
		bc2 = rotl64(t, 39)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		// Round 3
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[11] ^ d1
		bc1 = rotl64(t, 44)
		t = a[22] ^ d2
		bc2 = rotl64(t, 43)
		t = a[8] ^ d3
		bc3 = rotl64(t, 21)
		t = a[19] ^ d4
		bc4 = rotl64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+2]
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc2 = rotl64(t, 3)
		t = a[1] ^ d1
		bc3 = rotl64(t, 45)
		t = a[12] ^ d2
		bc4 = rotl64(t, 61)
		t = a[23] ^ d3
		bc0 = rotl64(t, 28)
		t = a[9] ^ d4
		bc1 = rotl64(t, 20)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc4 = rotl64(t, 18)
		t = a[16] ^ d1
		bc0 = rotl64(t, 1)
		t = a[2] ^ d2
		bc1 = rotl64(t, 6)
		t = a[13] ^ d3
		bc2 = rotl64(t, 25)
		t = a[24] ^ d4
		bc3 = rotl64(t, 8)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc1 = rotl64(t, 36)
		t = a[6] ^ d1
		bc2 = rotl64(t, 10)
		t = a[17] ^ d2
		bc3 = rotl64(t, 15)
		t = a[3] ^ d3
		bc4 = rotl64(t, 56)
		t = a[14] ^ d4
		bc0 = rotl64(t, 27)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc3 = rotl64(t, 41)
		t = a[21] ^ d1
		bc4 = rotl64(t, 2)
		t = a[7] ^ d2
		bc0 = rotl64(t, 62)
		t = a[18] ^ d3
		bc1 = rotl64(t, 55)
		t = a[4] ^ d4
		bc2 = rotl64(t, 39)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		// Round 4
		bc0 = a[0] ^ a[5] ^ a[10] ^ a[15] ^ a[20]
		bc1 = a[1] ^ a[6] ^ a[11] ^ a[16] ^ a[21]
		bc2 = a[2] ^ a[7] ^ a[12] ^ a[17] ^ a[22]
		bc3 = a[3] ^ a[8] ^ a[13] ^ a[18] ^ a[23]
		bc4 = a[4] ^ a[9] ^ a[14] ^ a[19] ^ a[24]
		d0 = bc4 ^ (bc1<<1 | bc1>>63)
		d1 = bc0 ^ (bc2<<1 | bc2>>63)
		d2 = bc1 ^ (bc3<<1 | bc3>>63)
		d3 = bc2 ^ (bc4<<1 | bc4>>63)
		d4 = bc3 ^ (bc0<<1 | bc0>>63)

		bc0 = a[0] ^ d0
		t = a[1] ^ d1
		bc1 = rotl64(t, 44)
		t = a[2] ^ d2
		bc2 = rotl64(t, 43)
		t = a[3] ^ d3
		bc3 = rotl64(t, 21)
		t = a[4] ^ d4
		bc4 = rotl64(t, 14)
		a[0] = bc0 ^ (bc2 &^ bc1) ^ rc[i+3]
		a[1] = bc1 ^ (bc3 &^ bc2)
		a[2] = bc2 ^ (bc4 &^ bc3)
		a[3] = bc3 ^ (bc0 &^ bc4)
		a[4] = bc4 ^ (bc1 &^ bc0)

		t = a[5] ^ d0
		bc2 = rotl64(t, 3)
		t = a[6] ^ d1
		bc3 = rotl64(t, 45)
		t = a[7] ^ d2
		bc4 = rotl64(t, 61)
		t = a[8] ^ d3
		bc0 = rotl64(t, 28)
		t = a[9] ^ d4
		bc1 = rotl64(t, 20)
		a[5] = bc0 ^ (bc2 &^ bc1)
		a[6] = bc1 ^ (bc3 &^ bc2)
		a[7] = bc2 ^ (bc4 &^ bc3)
		a[8] = bc3 ^ (bc0 &^ bc4)
		a[9] = bc4 ^ (bc1 &^ bc0)

		t = a[10] ^ d0
		bc4 = rotl64(t, 18)
		t = a[11] ^ d1
		bc0 = rotl64(t, 1)
		t = a[12] ^ d2
		bc1 = rotl64(t, 6)
		t = a[13] ^ d3
		bc2 = rotl64(t, 25)
		t = a[14] ^ d4
		bc3 = rotl64(t, 8)
		a[10] = bc0 ^ (bc2 &^ bc1)
		a[11] = bc1 ^ (bc3 &^ bc2)
		a[12] = bc2 ^ (bc4 &^ bc3)
		a[13] = bc3 ^ (bc0 &^ bc4)
		a[14] = bc4 ^ (bc1 &^ bc0)

		t = a[15] ^ d0
		bc1 = rotl64(t, 36)
		t = a[16] ^ d1
		bc2 = rotl64(t, 10)
		t = a[17] ^ d2
		bc3 = rotl64(t, 15)
		t = a[18] ^ d3
		bc4 = rotl64(t, 56)
		t = a[19] ^ d4
		bc0 = rotl64(t, 27)
		a[15] = bc0 ^ (bc2 &^ bc1)
		a[16] = bc1 ^ (bc3 &^ bc2)
		a[17] = bc2 ^ (bc4 &^ bc3)
		a[18] = bc3 ^ (bc0 &^ bc4)
		a[19] = bc4 ^ (bc1 &^ bc0)

		t = a[20] ^ d0
		bc3 = rotl64(t, 41)
		t = a[21] ^ d1
		bc4 = rotl64(t, 2)
		t = a[22] ^ d2
		bc0 = rotl64(t, 62)
		t = a[23] ^ d3
		bc1 = rotl64(t, 55)
		t = a[24] ^ d4
		bc2 = rotl64(t, 39)
		a[20] = bc0 ^ (bc2 &^ bc1)
		a[21] = bc1 ^ (bc3 &^ bc2)
		a[22] = bc2 ^ (bc4 &^ bc3)
		a[23] = bc3 ^ (bc0 &^ bc4)
		a[24] = bc4 ^ (bc1 &^ bc0)
	}
	// Write the state back (replaces the removed defer).
	for i := 0; i < 25; i++ {
		boLEPutUint64(da[i*8:], a[i])
	}
	return da
}

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

func keccakF1600(a [200]byte) [200]byte {
	return keccakF1600Generic(a)
}

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sha3 implements the SHA-3 hash algorithms and the SHAKE extendable
// output functions defined in FIPS 202.

// registered wires this package into crypto.RegisterHash without
// init() (synthetic packages have no init support; package var
// initializers run at synthetic-package import).
var sha3Registered = registerSha3()

func registerSha3() bool {
	crypto.RegisterHash(crypto.SHA3_224, func() hash.Hash { return New224() })
	crypto.RegisterHash(crypto.SHA3_256, func() hash.Hash { return New256() })
	crypto.RegisterHash(crypto.SHA3_384, func() hash.Hash { return New384() })
	crypto.RegisterHash(crypto.SHA3_512, func() hash.Hash { return New512() })
	return true
}

// Sum224 returns the SHA3-224 hash of data.
func Sum224(data []byte) [28]byte {
	var out [28]byte
	h := newDigest224()
	h.WriteSingle(data)
	h.Sum(out[:0])
	return out
}

// Sum256 returns the SHA3-256 hash of data.
func Sum256(data []byte) [32]byte {
	var out [32]byte
	h := newDigest256()
	h.WriteSingle(data)
	h.Sum(out[:0])
	return out
}

// Sum384 returns the SHA3-384 hash of data.
func Sum384(data []byte) [48]byte {
	var out [48]byte
	h := newDigest384()
	h.WriteSingle(data)
	h.Sum(out[:0])
	return out
}

// Sum512 returns the SHA3-512 hash of data.
func Sum512(data []byte) [64]byte {
	var out [64]byte
	h := newDigest512()
	h.WriteSingle(data)
	h.Sum(out[:0])
	return out
}

// SumSHAKE128 applies the SHAKE128 extendable output function to data and
// returns an output of the given length in bytes.
func SumSHAKE128(data []byte, length int) []byte {
	// Outline the allocation for up to 256 bits of output to the caller's stack.
	out := make([]byte, 32)
	return sumSHAKE128(out, data, length)
}

func sumSHAKE128(out, data []byte, length int) []byte {
	if len(out) < length {
		out = make([]byte, length)
	} else {
		out = out[:length]
	}
	h := newShake128()
	h.WriteSingle(data)
	h.ReadSingle(out)
	return out
}

// SumSHAKE256 applies the SHAKE256 extendable output function to data and
// returns an output of the given length in bytes.
func SumSHAKE256(data []byte, length int) []byte {
	// Outline the allocation for up to 512 bits of output to the caller's stack.
	out := make([]byte, 64)
	return sumSHAKE256(out, data, length)
}

func sumSHAKE256(out, data []byte, length int) []byte {
	if len(out) < length {
		out = make([]byte, length)
	} else {
		out = out[:length]
	}
	h := newShake256()
	h.WriteSingle(data)
	h.ReadSingle(out)
	return out
}

// SHA3 is an instance of a SHA-3 hash. It implements [hash.Hash].
// The zero value is a usable SHA3-256 hash.
type SHA3 struct {
	d digest
}

// P3-M8b: storeDigest workarounds frontend/v1 WS D limits (no & on
// selector, no * deref); see copyDigest above.
func (s *SHA3) storeDigest(d *digest) {
	s.d.a = d.a
	s.d.n = d.n
	s.d.rate = d.rate
	s.d.dsbyte = d.dsbyte
	s.d.outputLen = d.outputLen
	s.d.state = d.state
}

// New224 creates a new SHA3-224 hash.
func New224() *SHA3 {
	// P3-M8b: &SHA3{*...} is not expressible (no StarExpr in v1).
	d := newDigest224()
	return &SHA3{d: digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}}
}

// New256 creates a new SHA3-256 hash.
func New256() *SHA3 {
	// P3-M8b: &SHA3{*...} is not expressible (no StarExpr in v1).
	d := newDigest256()
	return &SHA3{d: digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}}
}

// New384 creates a new SHA3-384 hash.
func New384() *SHA3 {
	// P3-M8b: &SHA3{*...} is not expressible (no StarExpr in v1).
	d := newDigest384()
	return &SHA3{d: digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}}
}

// New512 creates a new SHA3-512 hash.
func New512() *SHA3 {
	// P3-M8b: &SHA3{*...} is not expressible (no StarExpr in v1).
	d := newDigest512()
	return &SHA3{d: digest{a: d.a, n: d.n, rate: d.rate, dsbyte: d.dsbyte, outputLen: d.outputLen, state: d.state}}
}

func (s *SHA3) init() {
	if s.d.Size() == 0 {
		// P3-M8b: *s = *... is not expressible (no StarExpr in v1).
		d := newDigest256()
		s.d.a = d.a
		s.d.n = d.n
		s.d.rate = d.rate
		s.d.dsbyte = d.dsbyte
		s.d.outputLen = d.outputLen
		s.d.state = d.state
	}
}

// Write absorbs more data into the hash's state.
func (s *SHA3) WriteSingle(p []byte) int {
	s.init()
	// P3-M8b: s.d is a value; WriteSingle has a pointer receiver (WS D).
	d := copyDigest(s.d)
	n := d.WriteSingle(p)
	s.storeDigest(d)
	return n
}

// Write(p []byte) (n int, err error) implements hash.Hash. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHA3) Write(p []byte) (n int, err error) {
	panic("crypto/sha3: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Sum appends the current hash to b and returns the resulting slice.
func (s *SHA3) Sum(b []byte) []byte {
	s.init()
	// P3-M8b: s.d is a value; Sum has a pointer receiver (WS D).
	// Sum is read-only (it clones), so a copy is exact.
	return copyDigest(s.d).Sum(b)
}

// Reset resets the hash to its initial state.
func (s *SHA3) Reset() {
	s.init()
	// P3-M8b: s.d is a value; Reset has a pointer receiver (WS D).
	d := copyDigest(s.d)
	d.Reset()
	s.storeDigest(d)
}

// Size returns the number of bytes Sum will produce.
func (s *SHA3) Size() int {
	s.init()
	return s.d.Size()
}

// BlockSize returns the hash's rate.
func (s *SHA3) BlockSize() int {
	s.init()
	return s.d.BlockSize()
}

// MarshalBinary implements [encoding.BinaryMarshaler].
func (s *SHA3) MarshalSingle() []byte {
	s.init()
	return s.d.MarshalSingle()
}

// MarshalBinary() ([]byte, error) implements encoding.BinaryMarshaler. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHA3) MarshalBinary() ([]byte, error) {
	panic("crypto/sha3: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendBinary implements [encoding.BinaryAppender].
func (s *SHA3) AppendSingle(p []byte) []byte {
	s.init()
	return s.d.AppendSingle(p)
}

// AppendBinary(p []byte) ([]byte, error) implements encoding.BinaryAppender. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHA3) AppendBinary(p []byte) ([]byte, error) {
	panic("crypto/sha3: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler].
func (s *SHA3) UnmarshalBinary(data []byte) error {
	s.init()
	// P3-M8b: s.d is a value; UnmarshalBinary has a pointer receiver (WS D).
	d := copyDigest(s.d)
	if err := d.UnmarshalBinary(data); err != nil {
		return err
	}
	s.storeDigest(d)
	return nil
}

// Clone implements [hash.Cloner].
func (d *SHA3) CloneSinglePair() hash.ClonePair {
	// P3-M8b: (*d) / &r are not expressible in frontend/v1 (WS D);
	// &SHA3{...} on a composite literal is. Field-by-field copy
	// (arrays are values, so the copy is complete).
	return hash.ClonePair{C: &SHA3{digest{a: d.d.a, n: d.d.n, rate: d.d.rate, dsbyte: d.d.dsbyte, outputLen: d.d.outputLen, state: d.d.state}}}
}

// Clone() (hash.Cloner, error) implements hash.Cloner. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (d *SHA3) Clone() (hash.Cloner, error) {
	panic("crypto/sha3: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// SHAKE is an instance of a SHAKE extendable output function.
// The zero value is a usable SHAKE256 hash.
type SHAKE struct {
	// P3-M8b: d is a pointer — the shake methods have pointer receivers
	// (WS D: no implicit & on a value field).
	d *shake
}

func (s *SHAKE) init() {
	if s.d.Size() == 0 {
		// P3-M8b: *s = *... is not expressible (no StarExpr in v1).
		// s.d is *shake; replace it with a fresh one.
		s.d = newShake256()
	}
}

// NewSHAKE128 creates a new SHAKE128 XOF.
func NewSHAKE128() *SHAKE {
	// P3-M8b: &SHAKE{*...} is not expressible (no StarExpr in v1).
	return &SHAKE{d: newShake128()}
}

// NewSHAKE256 creates a new SHAKE256 XOF.
func NewSHAKE256() *SHAKE {
	return &SHAKE{d: newShake256()}
}

// NewCSHAKE128 creates a new cSHAKE128 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewSHAKE128.
func NewCSHAKE128(N, S []byte) *SHAKE {
	// P3-M8b: &SHAKE{*...} is not expressible (no StarExpr in v1).
	return &SHAKE{d: newCShake128(N, S)}
}

// NewCSHAKE256 creates a new cSHAKE256 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewSHAKE256.
func NewCSHAKE256(N, S []byte) *SHAKE {
	// P3-M8b: &SHAKE{*...} is not expressible (no StarExpr in v1).
	return &SHAKE{d: newCShake256(N, S)}
}

// Write absorbs more data into the XOF's state.
//
// It panics if any output has already been read.
func (s *SHAKE) WriteSingle(p []byte) int {
	s.init()
	return s.d.WriteSingle(p)
}

// Write(p []byte) (n int, err error) implements hash.Hash. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHAKE) Write(p []byte) (n int, err error) {
	panic("crypto/sha3: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Read squeezes more output from the XOF.
//
// Any call to Write after a call to Read will panic.
func (s *SHAKE) ReadSingle(p []byte) int {
	s.init()
	return s.d.ReadSingle(p)
}

// Read(p []byte) (n int, err error) implements hash.XOF. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHAKE) Read(p []byte) (n int, err error) {
	panic("crypto/sha3: Read: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Reset resets the XOF to its initial state.
func (s *SHAKE) Reset() {
	s.init()
	s.d.Reset()
}

// BlockSize returns the rate of the XOF.
func (s *SHAKE) BlockSize() int {
	s.init()
	return s.d.BlockSize()
}

// MarshalBinary implements [encoding.BinaryMarshaler].
func (s *SHAKE) MarshalSingle() []byte {
	s.init()
	return s.d.MarshalSingle()
}

// MarshalBinary() ([]byte, error) implements encoding.BinaryMarshaler. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHAKE) MarshalBinary() ([]byte, error) {
	panic("crypto/sha3: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendBinary implements [encoding.BinaryAppender].
func (s *SHAKE) AppendSingle(p []byte) []byte {
	s.init()
	return s.d.AppendSingle(p)
}

// AppendBinary(p []byte) ([]byte, error) implements encoding.BinaryAppender. The v1 frontend cannot emit or call multi-result
// functions (WS D): this keeps the exact gc signature for type-checking with
// a fail-fast body. Real callers go through the mrM8 desugar (see
// m8-staging/crypto/03-mrdesugar.patch). Re-test trigger: WS D lands.
func (s *SHAKE) AppendBinary(p []byte) ([]byte, error) {
	panic("crypto/sha3: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler].
func (s *SHAKE) UnmarshalBinary(data []byte) error {
	s.init()
	return s.d.UnmarshalBinary(data)
}
`
