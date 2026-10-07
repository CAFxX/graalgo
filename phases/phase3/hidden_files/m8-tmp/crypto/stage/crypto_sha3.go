package sha3

import (
	"errors"
	"bytes"
	"crypto"
	"hash"
)


// -- synthetic byteorder helpers (inlined from internal/byteorder) --
func boLEUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func boLEPutUint32(b []byte, v uint32) {
	b[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
}
func boLEUint64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
func boLEPutUint64(b []byte, v uint64) {
	b[0] = byte(v); b[1] = byte(v >> 8); b[2] = byte(v >> 16); b[3] = byte(v >> 24)
	b[4] = byte(v >> 32); b[5] = byte(v >> 40); b[6] = byte(v >> 48); b[7] = byte(v >> 56)
}
func boBEUint32(b []byte) uint32 {
	return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24
}
func boBEPutUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24); b[1] = byte(v >> 16); b[2] = byte(v >> 8); b[3] = byte(v)
}
func boBEUint64(b []byte) uint64 {
	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}
func boBEPutUint64(b []byte, v uint64) {
	b[0] = byte(v >> 56); b[1] = byte(v >> 48); b[2] = byte(v >> 40); b[3] = byte(v >> 32)
	b[4] = byte(v >> 24); b[5] = byte(v >> 16); b[6] = byte(v >> 8); b[7] = byte(v)
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
func (d *digest) BlockSize() int { return d.rate }

// Size returns the output size of the hash function in bytes.
func (d *digest) Size() int { return d.outputLen }

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
	ret := *d
	return &ret
}

// permute applies the KeccakF-1600 permutation.
func (d *digest) permute() {
	keccakF1600(&d.a)
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
func (d *digest) Write(p []byte) (n int, err error) { return d.write(p) }
func (d *digest) writeSingle(p []byte) int {
	if d.state != spongeAbsorbing {
		panic("sha3: Write after Read")
	}

	n = len(p)

	for len(p) > 0 {
		x := subtle.XORBytes(d.a[d.n:d.rate], d.a[d.n:d.rate], p)
		d.n += x
		p = p[x:]

		// If the sponge is full, apply the permutation.
		if d.n == d.rate {
			d.permute()
		}
	}

	return
}

// read squeezes an arbitrary number of bytes from the sponge.
func (d *digest) readSingle(out []byte) int {
	// If we're still absorbing, pad and apply the permutation.
	if d.state == spongeAbsorbing {
		d.padAndPermute()
	}

	n = len(out)

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

	return
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (d *digest) Sum(b []byte) []byte {
	return d.sum(b)
}

func (d *digest) sumGeneric(b []byte) []byte {
	if d.state != spongeAbsorbing {
		panic("sha3: Sum after Read")
	}

	// Make a copy of the original hash so that caller can keep writing
	// and summing.
	dup := d.Clone()
	hash := make([]byte, dup.outputLen, 64) // explicit cap to allow stack allocation
	dup.read(hash)
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

func (d *digest) MarshalBinary() ([]byte, error) {
	return d.AppendBinary(make([]byte, 0, marshaledSize))
}

func (d *digest) AppendBinary(b []byte) ([]byte, error) {
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
	return b, nil
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

func bytepadWrite(c *shake, data []byte, rate int) {
	rateEnc := leftEncode(uint64(rate))
	c.Write(rateEnc)
	c.Write(data)
	if padlen := rate - (len(rateEnc)+len(data))%rate; padlen < rate {
		const maxRate = rateK256
		c.Write(make([]byte, padlen, maxRate)) // explicit cap to allow stack allocation
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
func (s *shake) Sum(in []byte) []byte { return s.d.Sum(in) }

// Write absorbs more data into the hash's state.
// It panics if any output has already been read.
func (s *shake) Write(p []byte) (n int, err error) { return s.d.Write(p) }

func (s *shake) Read(out []byte) (n int, err error) {
	// Note that read is not exposed on digest since SHA-3 does not offer
	// variable output length. It is only used internally by Sum.
	return s.d.read(out)
}

// Reset resets the hash to initial state.
func (s *shake) Reset() {
	s.d.Reset()
	if len(s.initBlock) != 0 {
		bytepadWrite(s, s.initBlock, s.d.rate)
	}
}

// Clone returns a copy of the shake context in its current state.
func (s *shake) Clone() *shake {
	ret := *s
	return &ret
}

func (s *shake) MarshalBinary() ([]byte, error) {
	return s.AppendBinary(make([]byte, 0, marshaledSize+len(s.initBlock)))
}

func (s *shake) AppendBinary(b []byte) ([]byte, error) {
	b, err := s.d.AppendBinary(b)
	if err != nil {
		return nil, err
	}
	b = append(b, s.initBlock...)
	return b, nil
}

func (s *shake) UnmarshalBinary(b []byte) error {
	if len(b) < marshaledSize {
		return errors.New("sha3: invalid hash state")
	}
	if err := s.d.UnmarshalBinary(b[:marshaledSize]); err != nil {
		return err
	}
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
	return newCShake128(c, N, S)
}

func newCShake128Inner(c *shake, N, S []byte) *shake {
	if len(N) == 0 && len(S) == 0 {
		*c = *NewShake128()
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
	return newCShake256(c, N, S)
}

func newCShake256Inner(c *shake, N, S []byte) *shake {
	if len(N) == 0 && len(S) == 0 {
		*c = *NewShake256()
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
func keccakF1600Generic(da *[200]byte) {
	// P3-M8: always-copy rewrite of the stdlib's unsafe fast path. The
	// stdlib aliases da as *[25]uint64 via unsafe.Pointer on little-endian
	// hosts (perf-only); the v1 frontend has no unsafe, so we always copy
	// through a [25]uint64 exactly like the stdlib's big-endian path.
	// Output-identical on all hosts (covered by the SHA-3 KAT differential).
	a := new([25]uint64)
	for i := 0; i < 25; i++ {
		a[i] = boLEUint64(da[i*8:])
	}
	defer func() {
		for i := 0; i < 25; i++ {
			boLEPutUint64(da[i*8:], a[i])
		}
	}()

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
}

// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

func keccakF1600(a *[200]byte) {
	keccakF1600Generic(a)
}





// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sha3 implements the SHA-3 hash algorithms and the SHAKE extendable
// output functions defined in FIPS 202.

func init() {
	crypto.RegisterHash(crypto.SHA3_224, func() hash.Hash { return New224() })
	crypto.RegisterHash(crypto.SHA3_256, func() hash.Hash { return New256() })
	crypto.RegisterHash(crypto.SHA3_384, func() hash.Hash { return New384() })
	crypto.RegisterHash(crypto.SHA3_512, func() hash.Hash { return New512() })
}

// Sum224 returns the SHA3-224 hash of data.
func Sum224(data []byte) [28]byte {
	var out [28]byte
	h := sha3.New224()
	h.Write(data)
	h.Sum(out[:0])
	return out
}

// Sum256 returns the SHA3-256 hash of data.
func Sum256(data []byte) [32]byte {
	var out [32]byte
	h := sha3.New256()
	h.Write(data)
	h.Sum(out[:0])
	return out
}

// Sum384 returns the SHA3-384 hash of data.
func Sum384(data []byte) [48]byte {
	var out [48]byte
	h := sha3.New384()
	h.Write(data)
	h.Sum(out[:0])
	return out
}

// Sum512 returns the SHA3-512 hash of data.
func Sum512(data []byte) [64]byte {
	var out [64]byte
	h := sha3.New512()
	h.Write(data)
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
	h := sha3.NewShake128()
	h.Write(data)
	h.Read(out)
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
	h := sha3.NewShake256()
	h.Write(data)
	h.Read(out)
	return out
}

// SHA3 is an instance of a SHA-3 hash. It implements [hash.Hash].
// The zero value is a usable SHA3-256 hash.
type SHA3 struct {
	s sha3.Digest
}

func fips140hash_sha3Unwrap(s *SHA3) *sha3.Digest {
	s.init()
	return &s.s
}

// New224 creates a new SHA3-224 hash.
func New224() *SHA3 {
	return &SHA3{*sha3.New224()}
}

// New256 creates a new SHA3-256 hash.
func New256() *SHA3 {
	return &SHA3{*sha3.New256()}
}

// New384 creates a new SHA3-384 hash.
func New384() *SHA3 {
	return &SHA3{*sha3.New384()}
}

// New512 creates a new SHA3-512 hash.
func New512() *SHA3 {
	return &SHA3{*sha3.New512()}
}

func (s *SHA3) init() {
	if s.s.Size() == 0 {
		*s = *New256()
	}
}

// Write absorbs more data into the hash's state.
func (s *SHA3) Write(p []byte) (n int, err error) {
	s.init()
	return s.s.Write(p)
}

// Sum appends the current hash to b and returns the resulting slice.
func (s *SHA3) Sum(b []byte) []byte {
	s.init()
	return s.s.Sum(b)
}

// Reset resets the hash to its initial state.
func (s *SHA3) Reset() {
	s.init()
	s.s.Reset()
}

// Size returns the number of bytes Sum will produce.
func (s *SHA3) Size() int {
	s.init()
	return s.s.Size()
}

// BlockSize returns the hash's rate.
func (s *SHA3) BlockSize() int {
	s.init()
	return s.s.BlockSize()
}

// MarshalBinary implements [encoding.BinaryMarshaler].
func (s *SHA3) MarshalBinary() ([]byte, error) {
	s.init()
	return s.s.MarshalBinary()
}

// AppendBinary implements [encoding.BinaryAppender].
func (s *SHA3) AppendBinary(p []byte) ([]byte, error) {
	s.init()
	return s.s.AppendBinary(p)
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler].
func (s *SHA3) UnmarshalBinary(data []byte) error {
	s.init()
	return s.s.UnmarshalBinary(data)
}

// Clone implements [hash.Cloner].
func (d *SHA3) Clone() (hash.Cloner, error) {
	r := *d
	return &r, nil
}

// SHAKE is an instance of a SHAKE extendable output function.
// The zero value is a usable SHAKE256 hash.
type SHAKE struct {
	s sha3.SHAKE
}

func (s *SHAKE) init() {
	if s.s.Size() == 0 {
		*s = *NewSHAKE256()
	}
}

// NewSHAKE128 creates a new SHAKE128 XOF.
func NewSHAKE128() *SHAKE {
	return &SHAKE{*sha3.NewShake128()}
}

// NewSHAKE256 creates a new SHAKE256 XOF.
func NewSHAKE256() *SHAKE {
	return &SHAKE{*sha3.NewShake256()}
}

// NewCSHAKE128 creates a new cSHAKE128 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewSHAKE128.
func NewCSHAKE128(N, S []byte) *SHAKE {
	return &SHAKE{*sha3.NewCShake128(N, S)}
}

// NewCSHAKE256 creates a new cSHAKE256 XOF.
//
// N is used to define functions based on cSHAKE, it can be empty when plain
// cSHAKE is desired. S is a customization byte string used for domain
// separation. When N and S are both empty, this is equivalent to NewSHAKE256.
func NewCSHAKE256(N, S []byte) *SHAKE {
	return &SHAKE{*sha3.NewCShake256(N, S)}
}

// Write absorbs more data into the XOF's state.
//
// It panics if any output has already been read.
func (s *SHAKE) Write(p []byte) (n int, err error) {
	s.init()
	return s.s.Write(p)
}

// Read squeezes more output from the XOF.
//
// Any call to Write after a call to Read will panic.
func (s *SHAKE) Read(p []byte) (n int, err error) {
	s.init()
	return s.s.Read(p)
}

// Reset resets the XOF to its initial state.
func (s *SHAKE) Reset() {
	s.init()
	s.s.Reset()
}

// BlockSize returns the rate of the XOF.
func (s *SHAKE) BlockSize() int {
	s.init()
	return s.s.BlockSize()
}

// MarshalBinary implements [encoding.BinaryMarshaler].
func (s *SHAKE) MarshalBinary() ([]byte, error) {
	s.init()
	return s.s.MarshalBinary()
}

// AppendBinary implements [encoding.BinaryAppender].
func (s *SHAKE) AppendBinary(p []byte) ([]byte, error) {
	s.init()
	return s.s.AppendBinary(p)
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler].
func (s *SHAKE) UnmarshalBinary(data []byte) error {
	s.init()
	return s.s.UnmarshalBinary(data)
}
