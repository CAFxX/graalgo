// SYNTHETIC (Principle 4) — crypto/sha512: real go1.27.1 source does not compile under graalgo as-published (import closure hits go:linkname / asm / intrinsics / unsafe — B-4); pure-Go port.
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M8 (wave 2): synthetic `sha512` package.
// Software implementation only (no intrinsification).
// See phases/phase3/hidden_files/m8-analysis/crypto.md.
const syntheticSha512Source = `package sha512

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

// Package sha512 implements the SHA-384, SHA-512, SHA-512/224, and SHA-512/256
// hash algorithms as defined in FIPS 180-4.
//
// All the hash.Hash implementations returned by this package also
// implement encoding.BinaryMarshaler and encoding.BinaryUnmarshaler to
// marshal and unmarshal the internal state of the hash.

// registered wires this package into crypto.RegisterHash without
// init() (synthetic packages have no init support; package var
// initializers run at synthetic-package import).
var sha512Registered = registerSha512()

func registerSha512() bool {
	crypto.RegisterHash(crypto.SHA384, func() hash.Hash { return New384() })
	crypto.RegisterHash(crypto.SHA512, func() hash.Hash { return New() })
	crypto.RegisterHash(crypto.SHA512_224, func() hash.Hash { return New512_224() })
	crypto.RegisterHash(crypto.SHA512_256, func() hash.Hash { return New512_256() })
	return true
}

const (
	// Size is the size, in bytes, of a SHA-512 checksum.
	Size = 64

	// Size224 is the size, in bytes, of a SHA-512/224 checksum.
	Size224 = 28

	// Size256 is the size, in bytes, of a SHA-512/256 checksum.
	Size256 = 32

	// Size384 is the size, in bytes, of a SHA-384 checksum.
	Size384 = 48

	// BlockSize is the block size, in bytes, of the SHA-512/224,
	// SHA-512/256, SHA-384 and SHA-512 hash functions.
	BlockSize = 128
)

// New returns a new [hash.Hash] computing the SHA-512 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New() hash.Hash {
	return newDigest()
}

// New512_224 returns a new [hash.Hash] computing the SHA-512/224 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New512_224() hash.Hash {
	return newDigest512_224()
}

// New512_256 returns a new [hash.Hash] computing the SHA-512/256 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New512_256() hash.Hash {
	return newDigest512_256()
}

// New384 returns a new [hash.Hash] computing the SHA-384 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New384() hash.Hash {
	return newDigest384()
}

// Sum512 returns the SHA512 checksum of the data.
func Sum512(data []byte) [Size]byte {
	d := newDigest()
	d.WriteSingle(data)
	var sum [Size]byte
	d.Sum(sum[:0])
	return sum
}

// Sum384 returns the SHA384 checksum of the data.
func Sum384(data []byte) [Size384]byte {
	d := newDigest384()
	d.WriteSingle(data)
	var sum [Size384]byte
	d.Sum(sum[:0])
	return sum
}

// Sum512_224 returns the Sum512/224 checksum of the data.
func Sum512_224(data []byte) [Size224]byte {
	d := newDigest512_224()
	d.WriteSingle(data)
	var sum [Size224]byte
	d.Sum(sum[:0])
	return sum
}

// Sum512_256 returns the Sum512/256 checksum of the data.
func Sum512_256(data []byte) [Size256]byte {
	d := newDigest512_256()
	d.WriteSingle(data)
	var sum [Size256]byte
	d.Sum(sum[:0])
	return sum
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sha512 implements the SHA-384, SHA-512, SHA-512/224, and SHA-512/256
// hash algorithms as defined in FIPS 180-4.

const (
	// size512 is the size, in bytes, of a SHA-512 checksum.
	size512 = 64

	// size224 is the size, in bytes, of a SHA-512/224 checksum.
	size224 = 28

	// size256 is the size, in bytes, of a SHA-512/256 checksum.
	size256 = 32

	// size384 is the size, in bytes, of a SHA-384 checksum.
	size384 = 48

	// blockSize is the block size, in bytes, of the SHA-512/224,
	// SHA-512/256, SHA-384 and SHA-512 hash functions.
	blockSize = 128
)

const (
	chunk     = 128
	init0     = 0x6a09e667f3bcc908
	init1     = 0xbb67ae8584caa73b
	init2     = 0x3c6ef372fe94f82b
	init3     = 0xa54ff53a5f1d36f1
	init4     = 0x510e527fade682d1
	init5     = 0x9b05688c2b3e6c1f
	init6     = 0x1f83d9abfb41bd6b
	init7     = 0x5be0cd19137e2179
	init0_224 = 0x8c3d37c819544da2
	init1_224 = 0x73e1996689dcd4d6
	init2_224 = 0x1dfab7ae32ff9c82
	init3_224 = 0x679dd514582f9fcf
	init4_224 = 0x0f6d2b697bd44da8
	init5_224 = 0x77e36f7304c48942
	init6_224 = 0x3f9d85a86a1d36c8
	init7_224 = 0x1112e6ad91d692a1
	init0_256 = 0x22312194fc2bf72c
	init1_256 = 0x9f555fa3c84c64c2
	init2_256 = 0x2393b86b6f53b151
	init3_256 = 0x963877195940eabd
	init4_256 = 0x96283ee2a88effe3
	init5_256 = 0xbe5e1e2553863992
	init6_256 = 0x2b0199fc2c85b8aa
	init7_256 = 0x0eb72ddc81c52ca2
	init0_384 = 0xcbbb9d5dc1059ed8
	init1_384 = 0x629a292a367cd507
	init2_384 = 0x9159015a3070dd17
	init3_384 = 0x152fecd8f70e5939
	init4_384 = 0x67332667ffc00b31
	init5_384 = 0x8eb44a8768581511
	init6_384 = 0xdb0c2e0d64f98fa7
	init7_384 = 0x47b5481dbefa4fa4
)

// digest is a SHA-384, SHA-512, SHA-512/224, or SHA-512/256 [hash.Hash]
// implementation.
type digest struct {
	h    [8]uint64
	x    [chunk]byte
	nx   int
	len  uint64
	size int // size224, size256, size384, or size512
}

func (d *digest) Reset() {
	switch d.size {
	case size384:
		d.h[0] = init0_384
		d.h[1] = init1_384
		d.h[2] = init2_384
		d.h[3] = init3_384
		d.h[4] = init4_384
		d.h[5] = init5_384
		d.h[6] = init6_384
		d.h[7] = init7_384
	case size224:
		d.h[0] = init0_224
		d.h[1] = init1_224
		d.h[2] = init2_224
		d.h[3] = init3_224
		d.h[4] = init4_224
		d.h[5] = init5_224
		d.h[6] = init6_224
		d.h[7] = init7_224
	case size256:
		d.h[0] = init0_256
		d.h[1] = init1_256
		d.h[2] = init2_256
		d.h[3] = init3_256
		d.h[4] = init4_256
		d.h[5] = init5_256
		d.h[6] = init6_256
		d.h[7] = init7_256
	case size512:
		d.h[0] = init0
		d.h[1] = init1
		d.h[2] = init2
		d.h[3] = init3
		d.h[4] = init4
		d.h[5] = init5
		d.h[6] = init6
		d.h[7] = init7
	default:
		panic("unknown size")
	}
	d.nx = 0
	d.len = 0
}

const (
	magic384      = "sha\x04"
	magic512_224  = "sha\x05"
	magic512_256  = "sha\x06"
	magic512      = "sha\x07"
	marshaledSize = len(magic512) + 8*8 + chunk + 8
)

func (d *digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// encoding.__binaryMarshal). Re-test trigger: WS D lands.
func (d *digest) MarshalBinary() ([]byte, error) {
	panic("crypto/sha512: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) AppendSingle(b []byte) []byte {
	switch d.size {
	case size384:
		b = append(b, magic384...)
	case size224:
		b = append(b, magic512_224...)
	case size256:
		b = append(b, magic512_256...)
	case size512:
		b = append(b, magic512...)
	default:
		panic("unknown size")
	}
	b = boBEAppendUint64(b, d.h[0])
	b = boBEAppendUint64(b, d.h[1])
	b = boBEAppendUint64(b, d.h[2])
	b = boBEAppendUint64(b, d.h[3])
	b = boBEAppendUint64(b, d.h[4])
	b = boBEAppendUint64(b, d.h[5])
	b = boBEAppendUint64(b, d.h[6])
	b = boBEAppendUint64(b, d.h[7])
	b = append(b, d.x[:d.nx]...)
	b = append(b, make([]byte, len(d.x)-d.nx)...)
	b = boBEAppendUint64(b, d.len)
	return b
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub;
// real logic is AppendSingle, served via the mrM8 desugar to
// encoding.__binaryAppend). Re-test trigger: WS D lands.
func (d *digest) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/sha512: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic512) {
		return errors.New("crypto/sha512: invalid hash state identifier")
	}
	switch {
	case d.size == size384 && string(b[:len(magic384)]) == magic384:
	case d.size == size224 && string(b[:len(magic512_224)]) == magic512_224:
	case d.size == size256 && string(b[:len(magic512_256)]) == magic512_256:
	case d.size == size512 && string(b[:len(magic512)]) == magic512:
	default:
		return errors.New("crypto/sha512: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("crypto/sha512: invalid hash state size")
	}
	b = b[len(magic512):]
	// P3-M8b: consumeUint64 inlined — its multi-result form cannot be
	// assigned to index targets (WS D).
	d.h[0] = boBEUint64(b[0:8])
	d.h[1] = boBEUint64(b[8:16])
	d.h[2] = boBEUint64(b[16:24])
	d.h[3] = boBEUint64(b[24:32])
	d.h[4] = boBEUint64(b[32:40])
	d.h[5] = boBEUint64(b[40:48])
	d.h[6] = boBEUint64(b[48:56])
	d.h[7] = boBEUint64(b[56:64])
	b = b[64:]
	b = b[copy(d.x[:], b):]
	d.len = boBEUint64(b[0:8])
	b = b[8:]
	d.nx = int(d.len % chunk)
	return nil
}

func (d *digest) CloneSinglePair() hash.ClonePair {
	// P3-M8b: (*d) / &r are not expressible in frontend/v1 (WS D);
	// &digest{...} on a composite literal is. Field-by-field copy
	// (arrays are values, so the copy is complete).
	return hash.ClonePair{C: &digest{h: d.h, x: d.x, nx: d.nx, len: d.len, size: d.size}}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is
// CloneSinglePair, served via the mrM8 desugar to hash.__hashClone).
// Re-test trigger: WS D lands.
func (d *digest) Clone() (hash.Cloner, error) {
	panic("crypto/sha512: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// New returns a new digest computing the SHA-512 hash.
func newDigest() *digest {
	d := &digest{size: size512}
	d.Reset()
	return d
}

// New512_224 returns a new digest computing the SHA-512/224 hash.
func newDigest512_224() *digest {
	d := &digest{size: size224}
	d.Reset()
	return d
}

// New512_256 returns a new digest computing the SHA-512/256 hash.
func newDigest512_256() *digest {
	d := &digest{size: size256}
	d.Reset()
	return d
}

// New384 returns a new digest computing the SHA-384 hash.
func newDigest384() *digest {
	d := &digest{size: size384}
	d.Reset()
	return d
}

func (d *digest) Size() int {
	return d.size
}

func (d *digest) BlockSize() int { return blockSize }

func (d *digest) WriteSingle(p []byte) int {
	d.len += uint64(len(p))
	if d.nx > 0 {
		n := copy(d.x[d.nx:], p)
		d.nx += n
		if d.nx == chunk {
			block(d, d.x[:])
			d.nx = 0
		}
		p = p[n:]
	}
	if len(p) >= chunk {
		n := len(p) &^ (chunk - 1)
		block(d, p[:n])
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
// single-result WriteSingle above. Re-test trigger: WS D lands.
func (d *digest) Write(p []byte) (nn int, err error) {
	panic("crypto/sha512: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) Sum(in []byte) []byte {
	// Make a copy of d so that caller can keep writing and summing.
	// P3-M8b: *d is not expressible in frontend/v1 (WS D); copy field
	// by field (arrays are values, so the copy is complete).
	d0 := &digest{h: d.h, x: d.x, nx: d.nx, len: d.len, size: d.size}
	hash := d0.checkSum()
	return append(in, hash[:d.size]...)
}

func (d *digest) checkSum() [size512]byte {
	// Padding. Add a 1 bit and 0 bits until 112 bytes mod 128.
	len := d.len
	var tmp [128 + 16]byte // padding + length buffer
	tmp[0] = 0x80
	var t uint64
	if len%128 < 112 {
		t = 112 - len%128
	} else {
		t = 128 + 112 - len%128
	}

	// Length in bits.
	len <<= 3
	padlen := tmp[:t+16]
	// Upper 64 bits are always zero, because len variable has type uint64,
	// and tmp is already zeroed at that index, so we can skip updating it.
	// boBEPutUint64(padlen[t+0:], 0)
	boBEPutUint64(padlen[t+8:], len)
	d.Write(padlen)

	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [size512]byte
	boBEPutUint64(digest[0:], d.h[0])
	boBEPutUint64(digest[8:], d.h[1])
	boBEPutUint64(digest[16:], d.h[2])
	boBEPutUint64(digest[24:], d.h[3])
	boBEPutUint64(digest[32:], d.h[4])
	boBEPutUint64(digest[40:], d.h[5])
	if d.size != size384 {
		boBEPutUint64(digest[48:], d.h[6])
		boBEPutUint64(digest[56:], d.h[7])
	}

	return digest
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// SHA512 block step.
// In its own file so that a faster assembly or C version
// can be substituted easily.

var _K = [...]uint64{
	0x428a2f98d728ae22,
	0x7137449123ef65cd,
	0xb5c0fbcfec4d3b2f,
	0xe9b5dba58189dbbc,
	0x3956c25bf348b538,
	0x59f111f1b605d019,
	0x923f82a4af194f9b,
	0xab1c5ed5da6d8118,
	0xd807aa98a3030242,
	0x12835b0145706fbe,
	0x243185be4ee4b28c,
	0x550c7dc3d5ffb4e2,
	0x72be5d74f27b896f,
	0x80deb1fe3b1696b1,
	0x9bdc06a725c71235,
	0xc19bf174cf692694,
	0xe49b69c19ef14ad2,
	0xefbe4786384f25e3,
	0x0fc19dc68b8cd5b5,
	0x240ca1cc77ac9c65,
	0x2de92c6f592b0275,
	0x4a7484aa6ea6e483,
	0x5cb0a9dcbd41fbd4,
	0x76f988da831153b5,
	0x983e5152ee66dfab,
	0xa831c66d2db43210,
	0xb00327c898fb213f,
	0xbf597fc7beef0ee4,
	0xc6e00bf33da88fc2,
	0xd5a79147930aa725,
	0x06ca6351e003826f,
	0x142929670a0e6e70,
	0x27b70a8546d22ffc,
	0x2e1b21385c26c926,
	0x4d2c6dfc5ac42aed,
	0x53380d139d95b3df,
	0x650a73548baf63de,
	0x766a0abb3c77b2a8,
	0x81c2c92e47edaee6,
	0x92722c851482353b,
	0xa2bfe8a14cf10364,
	0xa81a664bbc423001,
	0xc24b8b70d0f89791,
	0xc76c51a30654be30,
	0xd192e819d6ef5218,
	0xd69906245565a910,
	0xf40e35855771202a,
	0x106aa07032bbd1b8,
	0x19a4c116b8d2d0c8,
	0x1e376c085141ab53,
	0x2748774cdf8eeb99,
	0x34b0bcb5e19b48a8,
	0x391c0cb3c5c95a63,
	0x4ed8aa4ae3418acb,
	0x5b9cca4f7763e373,
	0x682e6ff3d6b2b8a3,
	0x748f82ee5defb2fc,
	0x78a5636f43172f60,
	0x84c87814a1f0ab72,
	0x8cc702081a6439ec,
	0x90befffa23631e28,
	0xa4506cebde82bde9,
	0xbef9a3f7b2c67915,
	0xc67178f2e372532b,
	0xca273eceea26619c,
	0xd186b8c721c0c207,
	0xeada7dd6cde0eb1e,
	0xf57d4f7fee6ed178,
	0x06f067aa72176fba,
	0x0a637dc5a2c898a6,
	0x113f9804bef90dae,
	0x1b710b35131c471b,
	0x28db77f523047d84,
	0x32caab7b40c72493,
	0x3c9ebe0a15c9bebc,
	0x431d67c49c100d4c,
	0x4cc5d4becb3e42b6,
	0x597f299cfc657e2a,
	0x5fcb6fab3ad6faec,
	0x6c44198c4a475817,
}

func blockGeneric(dig *digest, p []byte) {
	var w [80]uint64
	h0, h1, h2, h3, h4, h5, h6, h7 := dig.h[0], dig.h[1], dig.h[2], dig.h[3], dig.h[4], dig.h[5], dig.h[6], dig.h[7]
	for len(p) >= chunk {
		a, b, c, d, e, f, g, h := h0, h1, h2, h3, h4, h5, h6, h7

		for i := 0; i < 80; i++ {
			if i < 16 {
				j := i * 8
				w[i] = uint64(p[j])<<56 | uint64(p[j+1])<<48 | uint64(p[j+2])<<40 | uint64(p[j+3])<<32 |
					uint64(p[j+4])<<24 | uint64(p[j+5])<<16 | uint64(p[j+6])<<8 | uint64(p[j+7])
			} else {
				v1 := w[i-2]
				t1 := rotl64(v1, -19) ^ rotl64(v1, -61) ^ (v1 >> 6)
				v2 := w[i-15]
				t2 := rotl64(v2, -1) ^ rotl64(v2, -8) ^ (v2 >> 7)

				w[i] = t1 + w[i-7] + t2 + w[i-16]
			}

			t1 := h + (rotl64(e, -14) ^ rotl64(e, -18) ^ rotl64(e, -41)) + ((e & f) ^ (^e & g)) + _K[i] + w[i]

			t2 := (rotl64(a, -28) ^ rotl64(a, -34) ^ rotl64(a, -39)) + ((a & b) ^ (a & c) ^ (b & c))

			h = g
			g = f
			f = e
			e = d + t1
			d = c
			c = b
			b = a
			a = t1 + t2
		}

		h0 += a
		h1 += b
		h2 += c
		h3 += d
		h4 += e
		h5 += f
		h6 += g
		h7 += h

		p = p[chunk:]
	}

	dig.h[0], dig.h[1], dig.h[2], dig.h[3], dig.h[4], dig.h[5], dig.h[6], dig.h[7] = h0, h1, h2, h3, h4, h5, h6, h7
}

// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

func block(dig *digest, p []byte) {
	blockGeneric(dig, p)
}
`
