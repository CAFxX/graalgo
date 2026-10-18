package main

// P3-M8 (wave 2): synthetic `md5` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticMd5Source = `package md5

import (
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

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package md5 implements the MD5 hash algorithm as defined in RFC 1321.
//
// MD5 is cryptographically broken and should not be used for secure
// applications.

// registered wires this package into crypto.RegisterHash without
// init() (synthetic packages have no init support; package var
// initializers run at synthetic-package import).
var md5Registered = registerMd5()

func registerMd5() bool {
	crypto.RegisterHash(crypto.MD5, func() hash.Hash { return New() })
	return true
}

// The size of an MD5 checksum in bytes.
const Size = 16

// The blocksize of MD5 in bytes.
const BlockSize = 64

// The maximum number of bytes that can be passed to block(). The limit exists
// because implementations that rely on assembly routines are not preemptible.

const (
	init0 = 0x67452301
	init1 = 0xEFCDAB89
	init2 = 0x98BADCFE
	init3 = 0x10325476
)

// digest represents the partial evaluation of a checksum.
type digest struct {
	s   [4]uint32
	x   [BlockSize]byte
	nx  int
	len uint64
}

func (d *digest) Reset() {
	d.s[0] = init0
	d.s[1] = init1
	d.s[2] = init2
	d.s[3] = init3
	d.nx = 0
	d.len = 0
}

const (
	magic         = "md5\x01"
	marshaledSize = len(magic) + 4*4 + BlockSize + 8
)

func (d *digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// encoding.__binaryMarshal).
func (d *digest) MarshalBinary() ([]byte, error) {
	panic("crypto/md5: MarshalBinary: unreachable (call sites desugar to __binaryMarshal")
}

func (d *digest) AppendSingle(b []byte) []byte {
	b = append(b, magic...)
	b = boBEAppendUint32(b, d.s[0])
	b = boBEAppendUint32(b, d.s[1])
	b = boBEAppendUint32(b, d.s[2])
	b = boBEAppendUint32(b, d.s[3])
	b = append(b, d.x[:d.nx]...)
	b = append(b, make([]byte, len(d.x)-d.nx)...)
	b = boBEAppendUint64(b, d.len)
	return b
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub;
// real logic is AppendSingle, served via the mrM8 desugar to
// encoding.__binaryAppend).
func (d *digest) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/md5: AppendBinary: unreachable (call sites desugar to __binaryAppend")
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic) || string(b[:len(magic)]) != magic {
		return errors.New("crypto/md5: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("crypto/md5: invalid hash state size")
	}
	b = b[len(magic):]
	// P3-M8b: consumeUint32/consumeUint64 inlined — their multi-result
	// form cannot be assigned to index/selectors targets (WS D).
	d.s[0] = boBEUint32(b[0:4])
	d.s[1] = boBEUint32(b[4:8])
	d.s[2] = boBEUint32(b[8:12])
	d.s[3] = boBEUint32(b[12:16])
	b = b[16:]
	b = b[copy(d.x[:], b):]
	d.len = boBEUint64(b[0:8])
	b = b[8:]
	d.nx = int(d.len % BlockSize)
	return nil
}

func (d *digest) CloneSinglePair() hash.ClonePair {
	// P3-M8b: (*d) / &r are not expressible in frontend/v1 (WS D);
	// &digest{...} on a composite literal is. Field-by-field copy
	// (arrays are values, so the copy is complete).
	return hash.ClonePair{C: &digest{s: d.s, x: d.x, nx: d.nx, len: d.len}}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is
// CloneSinglePair, served via the mrM8 desugar to hash.__hashClone).
func (d *digest) Clone() (hash.Cloner, error) {
	panic("crypto/md5: Clone: unreachable (call sites desugar to __hashClone")
}

// New returns a new [hash.Hash] computing the MD5 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New() hash.Hash {
	d := new(digest)
	d.Reset()
	return d
}

func (d *digest) Size() int { return Size }

func (d *digest) BlockSize() int { return BlockSize }

func (d *digest) WriteSingle(p []byte) int {
	// Note that we currently call block or blockGeneric
	// directly (guarded using haveAsm) because this allows
	// escape analysis to see that p and d don't escape.
	d.len += uint64(len(p))
	if d.nx > 0 {
		n := copy(d.x[d.nx:], p)
		d.nx += n
		if d.nx == BlockSize {
			blockGeneric(d, d.x[:])
			d.nx = 0
		}
		p = p[n:]
	}
	if len(p) >= BlockSize {
		n := len(p) &^ (BlockSize - 1)
		blockGeneric(d, p[:n])
		p = p[n:]
	}
	if len(p) > 0 {
		d.nx = copy(d.x[:], p)
	}
	return len(p)
}

// Write implements hash.Hash. The v1 frontend cannot emit or call
// multi-result functions (WS D): this keeps the exact gc signature
// for type-checking with a fail-fast body. Real callers go through
// the mrM8 desugar to hash.__hashWrite (see
// m8-staging/crypto/03-mrdesugar.patch), which delegates to the
// single-result WriteSingle above.
func (d *digest) Write(p []byte) (nn int, err error) {
	panic("crypto/md5: Write: unreachable (call sites desugar to __hashWrite")
}

func (d *digest) Sum(in []byte) []byte {
	// Make a copy of d so that caller can keep writing and summing.
	// P3-M8b: *d is not expressible in frontend/v1 (WS D); copy field
	// by field (arrays are values, so the copy is complete).
	d0 := &digest{s: d.s, x: d.x, nx: d.nx, len: d.len}
	hash := d0.checkSum()
	return append(in, hash[:]...)
}

func (d *digest) checkSum() [Size]byte {

	// Append 0x80 to the end of the message and then append zeros
	// until the length is a multiple of 56 bytes. Finally append
	// 8 bytes representing the message length in bits.
	//
	// 1 byte end marker :: 0-63 padding bytes :: 8 byte length
	tmp := [1 + 63 + 8]byte{0x80}
	pad := (55 - d.len) % 64             // calculate number of padding bytes
	boLEPutUint64(tmp[1+pad:], d.len<<3) // append length in bits
	d.Write(tmp[:1+pad+8])

	// The previous write ensures that a whole number of
	// blocks (i.e. a multiple of 64 bytes) have been hashed.
	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [Size]byte
	boLEPutUint32(digest[0:], d.s[0])
	boLEPutUint32(digest[4:], d.s[1])
	boLEPutUint32(digest[8:], d.s[2])
	boLEPutUint32(digest[12:], d.s[3])
	return digest
}

// Sum returns the MD5 checksum of the data.
func Sum(data []byte) [Size]byte {
	// P3-M8b: d is a pointer — method calls on a value digest would need
	// an implicit address-of (WS D).
	d := &digest{}
	d.Reset()
	d.Write(data)
	return d.checkSum()
}

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Code generated by go run gen.go -output md5block.go; DO NOT EDIT.

func blockGeneric(dig *digest, p []byte) {
	// load state
	a, b, c, d := dig.s[0], dig.s[1], dig.s[2], dig.s[3]

	for i := 0; i <= len(p)-BlockSize; i += BlockSize {
		// eliminate bounds checks on p
		q := p[i:]
		q = q[:BlockSize:BlockSize]

		// save current state
		aa, bb, cc, dd := a, b, c, d

		// load input block
		x0 := boLEUint32(q[4*0x0:])
		x1 := boLEUint32(q[4*0x1:])
		x2 := boLEUint32(q[4*0x2:])
		x3 := boLEUint32(q[4*0x3:])
		x4 := boLEUint32(q[4*0x4:])
		x5 := boLEUint32(q[4*0x5:])
		x6 := boLEUint32(q[4*0x6:])
		x7 := boLEUint32(q[4*0x7:])
		x8 := boLEUint32(q[4*0x8:])
		x9 := boLEUint32(q[4*0x9:])
		xa := boLEUint32(q[4*0xa:])
		xb := boLEUint32(q[4*0xb:])
		xc := boLEUint32(q[4*0xc:])
		xd := boLEUint32(q[4*0xd:])
		xe := boLEUint32(q[4*0xe:])
		xf := boLEUint32(q[4*0xf:])

		// round 1
		a = b + rotl32((((c^d)&b)^d)+a+x0+0xd76aa478, 7)
		d = a + rotl32((((b^c)&a)^c)+d+x1+0xe8c7b756, 12)
		c = d + rotl32((((a^b)&d)^b)+c+x2+0x242070db, 17)
		b = c + rotl32((((d^a)&c)^a)+b+x3+0xc1bdceee, 22)
		a = b + rotl32((((c^d)&b)^d)+a+x4+0xf57c0faf, 7)
		d = a + rotl32((((b^c)&a)^c)+d+x5+0x4787c62a, 12)
		c = d + rotl32((((a^b)&d)^b)+c+x6+0xa8304613, 17)
		b = c + rotl32((((d^a)&c)^a)+b+x7+0xfd469501, 22)
		a = b + rotl32((((c^d)&b)^d)+a+x8+0x698098d8, 7)
		d = a + rotl32((((b^c)&a)^c)+d+x9+0x8b44f7af, 12)
		c = d + rotl32((((a^b)&d)^b)+c+xa+0xffff5bb1, 17)
		b = c + rotl32((((d^a)&c)^a)+b+xb+0x895cd7be, 22)
		a = b + rotl32((((c^d)&b)^d)+a+xc+0x6b901122, 7)
		d = a + rotl32((((b^c)&a)^c)+d+xd+0xfd987193, 12)
		c = d + rotl32((((a^b)&d)^b)+c+xe+0xa679438e, 17)
		b = c + rotl32((((d^a)&c)^a)+b+xf+0x49b40821, 22)

		// round 2
		a = b + rotl32((((b^c)&d)^c)+a+x1+0xf61e2562, 5)
		d = a + rotl32((((a^b)&c)^b)+d+x6+0xc040b340, 9)
		c = d + rotl32((((d^a)&b)^a)+c+xb+0x265e5a51, 14)
		b = c + rotl32((((c^d)&a)^d)+b+x0+0xe9b6c7aa, 20)
		a = b + rotl32((((b^c)&d)^c)+a+x5+0xd62f105d, 5)
		d = a + rotl32((((a^b)&c)^b)+d+xa+0x02441453, 9)
		c = d + rotl32((((d^a)&b)^a)+c+xf+0xd8a1e681, 14)
		b = c + rotl32((((c^d)&a)^d)+b+x4+0xe7d3fbc8, 20)
		a = b + rotl32((((b^c)&d)^c)+a+x9+0x21e1cde6, 5)
		d = a + rotl32((((a^b)&c)^b)+d+xe+0xc33707d6, 9)
		c = d + rotl32((((d^a)&b)^a)+c+x3+0xf4d50d87, 14)
		b = c + rotl32((((c^d)&a)^d)+b+x8+0x455a14ed, 20)
		a = b + rotl32((((b^c)&d)^c)+a+xd+0xa9e3e905, 5)
		d = a + rotl32((((a^b)&c)^b)+d+x2+0xfcefa3f8, 9)
		c = d + rotl32((((d^a)&b)^a)+c+x7+0x676f02d9, 14)
		b = c + rotl32((((c^d)&a)^d)+b+xc+0x8d2a4c8a, 20)

		// round 3
		a = b + rotl32((b^c^d)+a+x5+0xfffa3942, 4)
		d = a + rotl32((a^b^c)+d+x8+0x8771f681, 11)
		c = d + rotl32((d^a^b)+c+xb+0x6d9d6122, 16)
		b = c + rotl32((c^d^a)+b+xe+0xfde5380c, 23)
		a = b + rotl32((b^c^d)+a+x1+0xa4beea44, 4)
		d = a + rotl32((a^b^c)+d+x4+0x4bdecfa9, 11)
		c = d + rotl32((d^a^b)+c+x7+0xf6bb4b60, 16)
		b = c + rotl32((c^d^a)+b+xa+0xbebfbc70, 23)
		a = b + rotl32((b^c^d)+a+xd+0x289b7ec6, 4)
		d = a + rotl32((a^b^c)+d+x0+0xeaa127fa, 11)
		c = d + rotl32((d^a^b)+c+x3+0xd4ef3085, 16)
		b = c + rotl32((c^d^a)+b+x6+0x04881d05, 23)
		a = b + rotl32((b^c^d)+a+x9+0xd9d4d039, 4)
		d = a + rotl32((a^b^c)+d+xc+0xe6db99e5, 11)
		c = d + rotl32((d^a^b)+c+xf+0x1fa27cf8, 16)
		b = c + rotl32((c^d^a)+b+x2+0xc4ac5665, 23)

		// round 4
		a = b + rotl32((c^(b|^d))+a+x0+0xf4292244, 6)
		d = a + rotl32((b^(a|^c))+d+x7+0x432aff97, 10)
		c = d + rotl32((a^(d|^b))+c+xe+0xab9423a7, 15)
		b = c + rotl32((d^(c|^a))+b+x5+0xfc93a039, 21)
		a = b + rotl32((c^(b|^d))+a+xc+0x655b59c3, 6)
		d = a + rotl32((b^(a|^c))+d+x3+0x8f0ccc92, 10)
		c = d + rotl32((a^(d|^b))+c+xa+0xffeff47d, 15)
		b = c + rotl32((d^(c|^a))+b+x1+0x85845dd1, 21)
		a = b + rotl32((c^(b|^d))+a+x8+0x6fa87e4f, 6)
		d = a + rotl32((b^(a|^c))+d+xf+0xfe2ce6e0, 10)
		c = d + rotl32((a^(d|^b))+c+x6+0xa3014314, 15)
		b = c + rotl32((d^(c|^a))+b+xd+0x4e0811a1, 21)
		a = b + rotl32((c^(b|^d))+a+x4+0xf7537e82, 6)
		d = a + rotl32((b^(a|^c))+d+xb+0xbd3af235, 10)
		c = d + rotl32((a^(d|^b))+c+x2+0x2ad7d2bb, 15)
		b = c + rotl32((d^(c|^a))+b+x9+0xeb86d391, 21)

		// add saved state
		a += aa
		b += bb
		c += cc
		d += dd
	}

	// save state
	dig.s[0], dig.s[1], dig.s[2], dig.s[3] = a, b, c, d
}

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
`
