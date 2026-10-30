// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptointernalfips140sha256 for this package's root cause and retires-when trigger (crypto/internal/fips140/sha256).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/fips140/sha256' package.
const syntheticCryptoFips140Sha256Source = `// P3-M9 tier-5: synthetic 'crypto/internal/fips140/sha256'.
// go1.27.1 vendored pure-Go sources (sha256block_noasm.go selected
// for purego; amd64 asm excluded). Synthetic-only.
package sha256

import (
	"crypto/internal/fips140"
	"crypto/internal/fips140deps/byteorder"
	"errors"
	"hash"
	"math/bits"
)


// ---- vendored from crypto/internal/fips140/sha256/sha256.go ----




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

// Digest is a SHA-224 or SHA-256 [hash.Hash] implementation.
type Digest struct {
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

type __mrDigest_MarshalBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrDigest_MarshalBinary(d *Digest) __mrDigest_MarshalBinaryPair {
	__fw0, __fw1 := d.AppendBinary(make([]byte, 0, marshaledSize))
	return __mrDigest_MarshalBinaryPair{__fw0, __fw1}
}

func (d *Digest) MarshalBinary() ([]byte, error) {
	panic("crypto/internal/fips140/sha256: MarshalBinary: multi-result desugar")
}

type __mrDigest_AppendBinaryPair struct {
	v0 []byte
	v1 error
}

func __mrDigest_AppendBinary(d *Digest, b []byte) __mrDigest_AppendBinaryPair {
	if d.is224 {
		b = append(b, magic224...)
	} else {
		b = append(b, magic256...)
	}
	b = byteorder.BEAppendUint32(b, d.h[0])
	b = byteorder.BEAppendUint32(b, d.h[1])
	b = byteorder.BEAppendUint32(b, d.h[2])
	b = byteorder.BEAppendUint32(b, d.h[3])
	b = byteorder.BEAppendUint32(b, d.h[4])
	b = byteorder.BEAppendUint32(b, d.h[5])
	b = byteorder.BEAppendUint32(b, d.h[6])
	b = byteorder.BEAppendUint32(b, d.h[7])
	b = append(b, d.x[:d.nx]...)
	b = append(b, make([]byte, len(d.x)-d.nx)...)
	b = byteorder.BEAppendUint64(b, d.len)
	return __mrDigest_AppendBinaryPair{b, nil}
}

func (d *Digest) AppendBinary(b []byte) ([]byte, error) {
	panic("crypto/internal/fips140/sha256: AppendBinary: multi-result desugar")
}

func (d *Digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic224) || (d.is224 && string(b[:len(magic224)]) != magic224) || (!d.is224 && string(b[:len(magic256)]) != magic256) {
		return errors.New("crypto/sha256: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("crypto/sha256: invalid hash state size")
	}
	b = b[len(magic224):]
	b, d.h[0] = consumeUint32(b)
	b, d.h[1] = consumeUint32(b)
	b, d.h[2] = consumeUint32(b)
	b, d.h[3] = consumeUint32(b)
	b, d.h[4] = consumeUint32(b)
	b, d.h[5] = consumeUint32(b)
	b, d.h[6] = consumeUint32(b)
	b, d.h[7] = consumeUint32(b)
	b = b[copy(d.x[:], b):]
	b, d.len = consumeUint64(b)
	d.nx = int(d.len % chunk)
	return nil
}

type __mrconsumeUint64Pair struct {
	v0 []byte
	v1 uint64
}

func __mrconsumeUint64(b []byte) __mrconsumeUint64Pair {
	return __mrconsumeUint64Pair{b[8:], byteorder.BEUint64(b)}
}

func consumeUint64(b []byte) ([]byte, uint64) {
	panic("crypto/internal/fips140/sha256: consumeUint64: multi-result desugar")
}

type __mrconsumeUint32Pair struct {
	v0 []byte
	v1 uint32
}

func __mrconsumeUint32(b []byte) __mrconsumeUint32Pair {
	return __mrconsumeUint32Pair{b[4:], byteorder.BEUint32(b)}
}

func consumeUint32(b []byte) ([]byte, uint32) {
	panic("crypto/internal/fips140/sha256: consumeUint32: multi-result desugar")
}

type __mrDigest_ClonePair struct {
	v0 hash.Cloner
	v1 error
}

func __mrDigest_Clone(d *Digest) __mrDigest_ClonePair {
	r := *d
	return __mrDigest_ClonePair{&r, nil}
}

func (d *Digest) Clone() (hash.Cloner, error) {
	panic("crypto/internal/fips140/sha256: Clone: multi-result desugar")
}

func (d *Digest) Reset() {
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

// New returns a new Digest computing the SHA-256 hash.
func New() *Digest {
	d := new(Digest)
	d.Reset()
	return d
}

// New224 returns a new Digest computing the SHA-224 hash.
func New224() *Digest {
	d := new(Digest)
	d.is224 = true
	d.Reset()
	return d
}

func (d *Digest) Size() int {
	if !d.is224 {
		return size
	}
	return size224
}

func (d *Digest) BlockSize() int { return blockSize }

type __mrDigest_WritePair struct {
	nn int
	err error
}

func __mrDigest_Write(d *Digest, p []byte) __mrDigest_WritePair {
	var nn int
	var err error

	nn = len(p)
	d.len += uint64(nn)
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
	return __mrDigest_WritePair{nn, err}
}

func (d *Digest) Write(p []byte) (nn int, err error) {
	panic("crypto/internal/fips140/sha256: Write: multi-result desugar")
}

func (d *Digest) Sum(in []byte) []byte {
	fips140.RecordApproved()
	// Make a copy of d so that caller can keep writing and summing.
	d0 := *d
	hash := d0.checkSum()
	if d0.is224 {
		return append(in, hash[:size224]...)
	}
	return append(in, hash[:]...)
}

func (d *Digest) checkSum() [size]byte {
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
	byteorder.BEPutUint64(padlen[t+0:], len)
	d.Write(padlen)

	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [size]byte

	byteorder.BEPutUint32(digest[0:], d.h[0])
	byteorder.BEPutUint32(digest[4:], d.h[1])
	byteorder.BEPutUint32(digest[8:], d.h[2])
	byteorder.BEPutUint32(digest[12:], d.h[3])
	byteorder.BEPutUint32(digest[16:], d.h[4])
	byteorder.BEPutUint32(digest[20:], d.h[5])
	byteorder.BEPutUint32(digest[24:], d.h[6])
	if !d.is224 {
		byteorder.BEPutUint32(digest[28:], d.h[7])
	}

	return digest
}


// ---- vendored from crypto/internal/fips140/sha256/sha256block.go ----



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

func blockGeneric(dig *Digest, p []byte) {
	var w [64]uint32
	h0, h1, h2, h3, h4, h5, h6, h7 := dig.h[0], dig.h[1], dig.h[2], dig.h[3], dig.h[4], dig.h[5], dig.h[6], dig.h[7]
	for len(p) >= chunk {
		a, b, c, d, e, f, g, h := h0, h1, h2, h3, h4, h5, h6, h7

		for i := range 64 {
			if i < 16 {
				j := i * 4
				w[i] = uint32(p[j])<<24 | uint32(p[j+1])<<16 | uint32(p[j+2])<<8 | uint32(p[j+3])
			} else {
				v1 := w[i-2]
				t1 := (bits.RotateLeft32(v1, -17)) ^ (bits.RotateLeft32(v1, -19)) ^ (v1 >> 10)
				v2 := w[i-15]
				t2 := (bits.RotateLeft32(v2, -7)) ^ (bits.RotateLeft32(v2, -18)) ^ (v2 >> 3)
				w[i] = t1 + w[i-7] + t2 + w[i-16]
			}

			t1 := h + ((bits.RotateLeft32(e, -6)) ^ (bits.RotateLeft32(e, -11)) ^ (bits.RotateLeft32(e, -25))) + ((e & f) ^ (^e & g)) + _K[i] + w[i]

			t2 := ((bits.RotateLeft32(a, -2)) ^ (bits.RotateLeft32(a, -13)) ^ (bits.RotateLeft32(a, -22))) + ((a & b) ^ (a & c) ^ (b & c))

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


// ---- vendored from crypto/internal/fips140/sha256/sha256block_noasm.go ----


func block(dig *Digest, p []byte) {
	blockGeneric(dig, p)
}

`
