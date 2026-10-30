// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptosha256 for this package's root cause and retires-when trigger (crypto/sha256).
package main

// P3-M8 (wave 2): synthetic `sha256` package.
// Software implementation only (no intrinsification).
// See ../../docs/arch/phase3-analysis/m8-analysis/crypto.md.
const syntheticSha256Source = `package sha256

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

// Package sha256 implements the SHA224 and SHA256 hash algorithms as defined
// in FIPS 180-4.

// registered wires this package into crypto.RegisterHash without
// init() (synthetic packages have no init support; package var
// initializers run at synthetic-package import).
var sha256Registered = registerSha256()

func registerSha256() bool {
	crypto.RegisterHash(crypto.SHA224, func() hash.Hash { return New224() })
	crypto.RegisterHash(crypto.SHA256, func() hash.Hash { return New() })
	return true
}

// The size of a SHA256 checksum in bytes.
const Size = 32

// The size of a SHA224 checksum in bytes.
const Size224 = 28

// The blocksize of SHA256 and SHA224 in bytes.
const BlockSize = 64

// New returns a new [hash.Hash] computing the SHA256 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New() hash.Hash {
	return newDigest()
}

// New224 returns a new [hash.Hash] computing the SHA224 checksum. The Hash
// also implements [encoding.BinaryMarshaler], [encoding.BinaryAppender] and
// [encoding.BinaryUnmarshaler] to marshal and unmarshal the internal
// state of the hash.
func New224() hash.Hash {
	return newDigest224()
}

// Sum256 returns the SHA256 checksum of the data.
func Sum256(data []byte) [Size]byte {
	d := newDigest()
	d.WriteSingle(data)
	var sum [Size]byte
	d.Sum(sum[:0])
	return sum
}

// Sum224 returns the SHA224 checksum of the data.
func Sum224(data []byte) [Size224]byte {
	d := newDigest224()
	d.WriteSingle(data)
	var sum [Size224]byte
	d.Sum(sum[:0])
	return sum
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sha256 implements the SHA-224 and SHA-256 hash algorithms as defined
// in FIPS 180-4.

// The size of a SHA-256 checksum in bytes.
const size = 32

// The size of a SHA-224 checksum in bytes.
const size224 = 28

// The block size of SHA-256 and SHA-224 in bytes.
const blockSize = 64

// The maximum number of bytes that can be passed to block(). The limit exists
// because implementations that rely on assembly routines are not preemptible.
const maxAsmIters = 1024
const maxAsmSize = blockSize * maxAsmIters // 64KiB

const (
	chunk     = 64
	init0     = 0x6A09E667
	init1     = 0xBB67AE85
	init2     = 0x3C6EF372
	init3     = 0xA54FF53A
	init4     = 0x510E527F
	init5     = 0x9B05688C
	init6     = 0x1F83D9AB
	init7     = 0x5BE0CD19
	init0_224 = 0xC1059ED8
	init1_224 = 0x367CD507
	init2_224 = 0x3070DD17
	init3_224 = 0xF70E5939
	init4_224 = 0xFFC00B31
	init5_224 = 0x68581511
	init6_224 = 0x64F98FA7
	init7_224 = 0xBEFA4FA4
)

// digest is a SHA-224 or SHA-256 [hash.Hash] implementation.
type digest struct {
	h     [8]uint32
	x     [chunk]byte
	nx    int
	len   uint64
	is224 bool // mark if this digest is SHA-224
}

const (
	magic224      = "sha\x02"
	magic256      = "sha\x03"
	marshaledSize = len(magic256) + 8*4 + chunk + 8
)

func (d *digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// encoding.__binaryMarshal). Re-test trigger: WS D lands.
func (d *digest) MarshalBinary() ([]byte, error) {
	panic("crypto/sha256: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) AppendSingle(b []byte) []byte {
	if d.is224 {
		b = append(b, magic224...)
	} else {
		b = append(b, magic256...)
	}
	b = boBEAppendUint32(b, d.h[0])
	b = boBEAppendUint32(b, d.h[1])
	b = boBEAppendUint32(b, d.h[2])
	b = boBEAppendUint32(b, d.h[3])
	b = boBEAppendUint32(b, d.h[4])
	b = boBEAppendUint32(b, d.h[5])
	b = boBEAppendUint32(b, d.h[6])
	b = boBEAppendUint32(b, d.h[7])
	b = append(b, d.x[:d.nx]...)
	b = append(b, make([]byte, len(d.x)-d.nx)...)
	b = boBEAppendUint64(b, d.len)
	return b
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub;
// real logic is AppendSingle, served via the mrM8 desugar to
// encoding.__binaryAppend). Re-test trigger: WS D lands.
func (d *digest) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/sha256: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic224) || (d.is224 && string(b[:len(magic224)]) != magic224) || (!d.is224 && string(b[:len(magic256)]) != magic256) {
		return errors.New("crypto/sha256: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("crypto/sha256: invalid hash state size")
	}
	b = b[len(magic224):]
	// Intra-package calls: the Appendix-M desugar only rewrites
	// package-qualified call sites, so use the single-result __-helpers
	// directly (same pattern as the syscall/reflect packages).
	q0 := __consumeUint32(b)
	b, d.h[0] = q0.rest, q0.v
	q1 := __consumeUint32(b)
	b, d.h[1] = q1.rest, q1.v
	q2 := __consumeUint32(b)
	b, d.h[2] = q2.rest, q2.v
	q3 := __consumeUint32(b)
	b, d.h[3] = q3.rest, q3.v
	q4 := __consumeUint32(b)
	b, d.h[4] = q4.rest, q4.v
	q5 := __consumeUint32(b)
	b, d.h[5] = q5.rest, q5.v
	q6 := __consumeUint32(b)
	b, d.h[6] = q6.rest, q6.v
	q7 := __consumeUint32(b)
	b, d.h[7] = q7.rest, q7.v
	b = b[copy(d.x[:], b):]
	q8 := __consumeUint64(b)
	b, d.len = q8.rest, q8.v
	d.nx = int(d.len % chunk)
	return nil
}

// consumeUint64/consumeUint32 are multi-result helpers for the digest's
// UnmarshalBinary. The multi-result bodies below run only on the gc
// reference leg (they are skipped at synthetic import, WS D); the
// single-result __-helpers carry the real implementations used by the
// intra-package call sites above.
type consumeUint64Pair struct {
	rest []byte
	v    uint64
}

func __consumeUint64(b []byte) consumeUint64Pair {
	return consumeUint64Pair{b[8:], boBEUint64(b)}
}

func consumeUint64(b []byte) ([]byte, uint64) {
	p := __consumeUint64(b)
	return p.rest, p.v
}

type consumeUint32Pair struct {
	rest []byte
	v    uint32
}

func __consumeUint32(b []byte) consumeUint32Pair {
	return consumeUint32Pair{b[4:], boBEUint32(b)}
}

func consumeUint32(b []byte) ([]byte, uint32) {
	p := __consumeUint32(b)
	return p.rest, p.v
}

func (d *digest) CloneSinglePair() hash.ClonePair {
	// (*d) is not expressible in frontend/v1 (WS D); &digest{...} on a
	// composite literal is. Field-by-field copy (arrays are values, so
	// the copy is complete).
	return hash.ClonePair{C: &digest{h: d.h, x: d.x, nx: d.nx, len: d.len, is224: d.is224}}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is
// CloneSinglePair, served via the mrM8 desugar to hash.__hashClone).
// Re-test trigger: WS D lands.
func (d *digest) Clone() (hash.Cloner, error) {
	panic("crypto/sha256: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) Reset() {
	if !d.is224 {
		d.h[0] = init0
		d.h[1] = init1
		d.h[2] = init2
		d.h[3] = init3
		d.h[4] = init4
		d.h[5] = init5
		d.h[6] = init6
		d.h[7] = init7
	} else {
		d.h[0] = init0_224
		d.h[1] = init1_224
		d.h[2] = init2_224
		d.h[3] = init3_224
		d.h[4] = init4_224
		d.h[5] = init5_224
		d.h[6] = init6_224
		d.h[7] = init7_224
	}
	d.nx = 0
	d.len = 0
}

// New returns a new digest computing the SHA-256 hash.
func newDigest() *digest {
	d := new(digest)
	d.Reset()
	return d
}

// New224 returns a new digest computing the SHA-224 hash.
func newDigest224() *digest {
	d := new(digest)
	d.is224 = true
	d.Reset()
	return d
}

func (d *digest) Size() int {
	if !d.is224 {
		return size
	}
	return size224
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
		for n > maxAsmSize {
			block(d, p[:maxAsmSize])
			p = p[maxAsmSize:]
			n -= maxAsmSize
		}
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
	panic("crypto/sha256: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) Sum(in []byte) []byte {
	// Make a copy of d so that caller can keep writing and summing.
	// (*d) is not expressible in frontend/v1 (WS D); copy field by
	// field into a fresh composite literal (arrays are values, so the
	// copy is complete). d0 is a *digest so checkSum's pointer receiver
	// resolves without an implicit address-of.
	d0 := &digest{h: d.h, x: d.x, nx: d.nx, len: d.len, is224: d.is224}
	hash := d0.checkSum()
	if d0.is224 {
		return append(in, hash[:size224]...)
	}
	return append(in, hash[:]...)
}

func (d *digest) checkSum() [size]byte {
	len := d.len
	// Padding. Add a 1 bit and 0 bits until 56 bytes mod 64.
	var tmp [64 + 8]byte // padding + length buffer
	tmp[0] = 0x80
	var t uint64
	if len%64 < 56 {
		t = 56 - len%64
	} else {
		t = 64 + 56 - len%64
	}

	// Length in bits.
	len <<= 3
	padlen := tmp[:t+8]
	boBEPutUint64(padlen[t+0:], len)
	d.Write(padlen)

	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [size]byte

	boBEPutUint32(digest[0:], d.h[0])
	boBEPutUint32(digest[4:], d.h[1])
	boBEPutUint32(digest[8:], d.h[2])
	boBEPutUint32(digest[12:], d.h[3])
	boBEPutUint32(digest[16:], d.h[4])
	boBEPutUint32(digest[20:], d.h[5])
	boBEPutUint32(digest[24:], d.h[6])
	if !d.is224 {
		boBEPutUint32(digest[28:], d.h[7])
	}

	return digest
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// SHA256 block step.
// In its own file so that a faster assembly or C version
// can be substituted easily.

var _K = [...]uint32{
	0x428a2f98,
	0x71374491,
	0xb5c0fbcf,
	0xe9b5dba5,
	0x3956c25b,
	0x59f111f1,
	0x923f82a4,
	0xab1c5ed5,
	0xd807aa98,
	0x12835b01,
	0x243185be,
	0x550c7dc3,
	0x72be5d74,
	0x80deb1fe,
	0x9bdc06a7,
	0xc19bf174,
	0xe49b69c1,
	0xefbe4786,
	0x0fc19dc6,
	0x240ca1cc,
	0x2de92c6f,
	0x4a7484aa,
	0x5cb0a9dc,
	0x76f988da,
	0x983e5152,
	0xa831c66d,
	0xb00327c8,
	0xbf597fc7,
	0xc6e00bf3,
	0xd5a79147,
	0x06ca6351,
	0x14292967,
	0x27b70a85,
	0x2e1b2138,
	0x4d2c6dfc,
	0x53380d13,
	0x650a7354,
	0x766a0abb,
	0x81c2c92e,
	0x92722c85,
	0xa2bfe8a1,
	0xa81a664b,
	0xc24b8b70,
	0xc76c51a3,
	0xd192e819,
	0xd6990624,
	0xf40e3585,
	0x106aa070,
	0x19a4c116,
	0x1e376c08,
	0x2748774c,
	0x34b0bcb5,
	0x391c0cb3,
	0x4ed8aa4a,
	0x5b9cca4f,
	0x682e6ff3,
	0x748f82ee,
	0x78a5636f,
	0x84c87814,
	0x8cc70208,
	0x90befffa,
	0xa4506ceb,
	0xbef9a3f7,
	0xc67178f2,
}

func blockGeneric(dig *digest, p []byte) {
	var w [64]uint32
	h0, h1, h2, h3, h4, h5, h6, h7 := dig.h[0], dig.h[1], dig.h[2], dig.h[3], dig.h[4], dig.h[5], dig.h[6], dig.h[7]
	for len(p) >= chunk {
		a, b, c, d, e, f, g, h := h0, h1, h2, h3, h4, h5, h6, h7

		for i := 0; i < 64; i++ {
			if i < 16 {
				j := i * 4
				w[i] = uint32(p[j])<<24 | uint32(p[j+1])<<16 | uint32(p[j+2])<<8 | uint32(p[j+3])
			} else {
				v1 := w[i-2]
				t1 := (rotl32(v1, -17)) ^ (rotl32(v1, -19)) ^ (v1 >> 10)
				v2 := w[i-15]
				t2 := (rotl32(v2, -7)) ^ (rotl32(v2, -18)) ^ (v2 >> 3)
				w[i] = t1 + w[i-7] + t2 + w[i-16]
			}

			t1 := h + ((rotl32(e, -6)) ^ (rotl32(e, -11)) ^ (rotl32(e, -25))) + ((e & f) ^ (^e & g)) + _K[i] + w[i]

			t2 := ((rotl32(a, -2)) ^ (rotl32(a, -13)) ^ (rotl32(a, -22))) + ((a & b) ^ (a & c) ^ (b & c))

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
