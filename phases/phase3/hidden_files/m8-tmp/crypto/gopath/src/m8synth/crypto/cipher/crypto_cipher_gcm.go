package cipher

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

import (
	"m8synth/crypto/subtle"
	"errors"
)

// P3-M8: GCM via newGCMFallback + ported GHASH (software only, no AES
// fast-paths to avoid the cipher->aes import cycle; the generic fallback is
// algorithm-identical under purego).
// KAT: differential vs gc (AES-GCM test vectors).

const (
	gcmBlockSize         = 16
	gcmStandardNonceSize = 12
	gcmMinimumTagSize    = 12
	gcmTagSize           = 16
)

// NewGCM returns the given 128-bit, block cipher wrapped in Galois Counter
// Mode with the standard nonce length.
//
// P3-M8: fail-fast stub (exact gc signature). Real callers go through the
// mrM8 desugar to __newGCM. Re-test trigger: WS D lands.
func NewGCM(cipher Block) (AEAD, error) {
	pr := __newGCM(cipher)
	return pr.aead, pr.err
}

// NewGCMWithNonceSize returns the given 128-bit, block cipher wrapped in
// Galois Counter Mode with the given nonce size.
//
// P3-M8: fail-fast stub. Re-test trigger: WS D lands.
func NewGCMWithNonceSize(cipher Block, size int) (AEAD, error) {
	pr := __newGCMWithNonceSize(cipher, size)
	return pr.aead, pr.err
}

// NewGCMWithTagSize returns the given 128-bit, block cipher wrapped in
// Galois Counter Mode with the given tag size.
//
// P3-M8: fail-fast stub. Re-test trigger: WS D lands.
func NewGCMWithTagSize(cipher Block, tagSize int) (AEAD, error) {
	pr := __newGCMWithTagSize(cipher, tagSize)
	return pr.aead, pr.err
}

// newGCMAEADPair is the single-result form of NewGCM's results.
type newGCMAEADPair struct {
	aead AEAD
	err  error
}

// __newGCM serves NewGCM through the mrM8 desugar.
func __newGCM(cipher Block) newGCMAEADPair {
	aead, err := newGCM(cipher, gcmStandardNonceSize, gcmTagSize)
	return newGCMAEADPair{aead, err}
}

// __newGCMWithNonceSize serves NewGCMWithNonceSize through the mrM8 desugar.
func __newGCMWithNonceSize(cipher Block, size int) newGCMAEADPair {
	aead, err := newGCM(cipher, size, gcmTagSize)
	return newGCMAEADPair{aead, err}
}

// __newGCMWithTagSize serves NewGCMWithTagSize through the mrM8 desugar.
func __newGCMWithTagSize(cipher Block, tagSize int) newGCMAEADPair {
	aead, err := newGCM(cipher, gcmStandardNonceSize, tagSize)
	return newGCMAEADPair{aead, err}
}

func newGCM(cipher Block, nonceSize, tagSize int) (AEAD, error) {
	return newGCMFallback(cipher, nonceSize, tagSize)
}

// NewGCMWithRandomNonce returns the given 128-bit, block cipher wrapped in
// Galois Counter Mode with a random nonce.
//
// P3-M8: fail-fast at construction (no entropy; re-test: syscall entropy).
func NewGCMWithRandomNonce(cipher Block) (AEAD, error) {
	panic("crypto/cipher: NewGCMWithRandomNonce: no entropy (re-test: syscall entropy)")
}

func newGCMFallback(cipher Block, nonceSize, tagSize int) (AEAD, error) {
	if tagSize < gcmMinimumTagSize || tagSize > gcmBlockSize {
		return nil, errors.New("cipher: incorrect tag size given to GCM")
	}
	if nonceSize <= 0 {
		return nil, errors.New("cipher: the nonce can't have zero length")
	}
	if cipher.BlockSize() != gcmBlockSize {
		return nil, errors.New("cipher: NewGCM requires 128-bit block cipher")
	}
	return &gcmFallback{cipher: cipher, nonceSize: nonceSize, tagSize: tagSize}, nil
}

type gcmFallback struct {
	cipher    Block
	nonceSize int
	tagSize   int
}

func (g *gcmFallback) NonceSize() int { return g.nonceSize }
func (g *gcmFallback) Overhead() int  { return g.tagSize }

func (g *gcmFallback) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != g.nonceSize {
		panic("crypto/cipher: incorrect nonce length given to GCM")
	}
	if g.nonceSize == 0 {
		panic("crypto/cipher: incorrect GCM nonce size")
	}
	if uint64(len(plaintext)) > uint64((1<<32)-2)*gcmBlockSize {
		panic("crypto/cipher: message too large for GCM")
	}

	ret, out := sliceForAppend(dst, len(plaintext)+g.tagSize)
	if inexactOverlap(out, plaintext) {
		panic("crypto/cipher: invalid buffer overlap of output and input")
	}
	if anyOverlap(out, additionalData) {
		panic("crypto/cipher: invalid buffer overlap of output and additional data")
	}

	var H, counter, tagMask [gcmBlockSize]byte
	g.cipher.Encrypt(H[:], H[:])
	deriveCounter(&H, &counter, nonce)
	gcmCounterCryptGeneric(g.cipher, tagMask[:], tagMask[:], &counter)

	gcmCounterCryptGeneric(g.cipher, out, plaintext, &counter)

	var tag [gcmTagSize]byte
	gcmAuth(tag[:], &H, &tagMask, out[:len(plaintext)], additionalData)
	copy(out[len(plaintext):], tag[:])

	return ret
}

var errOpen = errors.New("cipher: message authentication failed")

// aeadOpenPair is the single-result form of AEAD.Open's results.
type aeadOpenPair struct {
	out []byte
	err error
}

// __aeadOpen serves AEAD.Open through the mrM8 desugar.
func __aeadOpen(aead AEAD, dst, nonce, ciphertext, additionalData []byte) aeadOpenPair {
	if o, ok := aead.(interface {
		OpenSingle(dst, nonce, ciphertext, additionalData []byte) aeadOpenPair
	}); ok {
		return o.OpenSingle(dst, nonce, ciphertext, additionalData)
	}
	panic("crypto/cipher: Open on non-synthetic AEAD (needs WS D; re-test when it lands)")
}

func (g *gcmFallback) OpenSingle(dst, nonce, ciphertext, additionalData []byte) aeadOpenPair {
	out, err := g.open(dst, nonce, ciphertext, additionalData)
	return aeadOpenPair{out, err}
}

// Open decrypts and authenticates ciphertext, authenticates additionalData.
func (g *gcmFallback) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	pr := g.OpenSingle(dst, nonce, ciphertext, additionalData)
	return pr.out, pr.err
}

func (g *gcmFallback) open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != g.nonceSize {
		panic("crypto/cipher: incorrect nonce length given to GCM")
	}
	if g.tagSize < gcmMinimumTagSize {
		panic("crypto/cipher: incorrect GCM tag size")
	}

	if len(ciphertext) < g.tagSize {
		return nil, errOpen
	}
	if uint64(len(ciphertext)) > uint64((1<<32)-2)*gcmBlockSize+uint64(g.tagSize) {
		return nil, errOpen
	}

	ret, out := sliceForAppend(dst, len(ciphertext)-g.tagSize)
	if inexactOverlap(out, ciphertext) {
		panic("crypto/cipher: invalid buffer overlap of output and input")
	}
	if anyOverlap(out, additionalData) {
		panic("crypto/cipher: invalid buffer overlap of output and additional data")
	}

	var H, counter, tagMask [gcmBlockSize]byte
	g.cipher.Encrypt(H[:], H[:])
	deriveCounter(&H, &counter, nonce)
	gcmCounterCryptGeneric(g.cipher, tagMask[:], tagMask[:], &counter)

	tag := ciphertext[len(ciphertext)-g.tagSize:]
	ciphertext = ciphertext[:len(ciphertext)-g.tagSize]

	var expectedTag [gcmTagSize]byte
	gcmAuth(expectedTag[:], &H, &tagMask, ciphertext, additionalData)
	if subtle.ConstantTimeCompare(expectedTag[:g.tagSize], tag) != 1 {
		for i := range out {
			out[i] = 0
		}
		return nil, errOpen
	}

	gcmCounterCryptGeneric(g.cipher, out, ciphertext, &counter)

	return ret, nil
}

func deriveCounter(H, counter *[gcmBlockSize]byte, nonce []byte) {
	if len(nonce) == gcmStandardNonceSize {
		copy(counter[:], nonce)
		counter[gcmBlockSize-1] = 1
	} else {
		lenBlock := make([]byte, 16)
		boBEPutUint64(lenBlock[8:], uint64(len(nonce))*8)
		J := ghashGHASH(H, nonce, lenBlock)
		copy(counter[:], J)
	}
}

func gcmCounterCryptGeneric(b Block, out, src []byte, counter *[gcmBlockSize]byte) {
	var mask [gcmBlockSize]byte
	for len(src) >= gcmBlockSize {
		b.Encrypt(mask[:], counter[:])
		gcmInc32(counter)
		// XORBytes with exact overlap check; out/src are disjoint here by
		// construction (checked by caller), so use direct loop.
		for i := 0; i < gcmBlockSize; i++ {
			out[i] = src[i] ^ mask[i]
		}
		out = out[gcmBlockSize:]
		src = src[gcmBlockSize:]
	}
	if len(src) > 0 {
		b.Encrypt(mask[:], counter[:])
		gcmInc32(counter)
		for i := 0; i < len(src); i++ {
			out[i] = src[i] ^ mask[i]
		}
	}
}

func gcmInc32(counterBlock *[gcmBlockSize]byte) {
	ctr := counterBlock[len(counterBlock)-4:]
	boBEPutUint32(ctr, boBEUint32(ctr)+1)
}

func gcmAuth(out []byte, H, tagMask *[gcmBlockSize]byte, ciphertext, additionalData []byte) {
	lenBlock := make([]byte, 16)
	boBEPutUint64(lenBlock[:8], uint64(len(additionalData))*8)
	boBEPutUint64(lenBlock[8:], uint64(len(ciphertext))*8)
	S := ghashGHASH(H, additionalData, ciphertext, lenBlock)
	for i := 0; i < len(out); i++ {
		out[i] = S[i] ^ tagMask[i]
	}
}

func sliceForAppend(in []byte, n int) (head, tail []byte) {
	if total := len(in) + n; cap(in) >= total {
		head = in[:total]
	} else {
		head = make([]byte, total)
		copy(head, in)
	}
	tail = head[len(in):]
	return
}

// GHASH (ported from crypto/internal/fips140/aes/gcm/ghash.go).
// P3-M8: range-over-func rewritten as plain loop; clear() rewritten.

func ghashMul(x, y uint32) uint64 {
	var xm, ym [4]uint32
	var z [4]uint64
	for i := 0; i < 4; i++ {
		xm[i] = x & (0x11111111 << uint(i))
		ym[i] = y & (0x11111111 << uint(i))
	}
	for i := 0; i < 4; i++ {
		z[i] = (uint64(xm[0]) * uint64(ym[i])) ^ (uint64(xm[1]) * uint64(ym[(i+3)%4])) ^ (uint64(xm[2]) * uint64(ym[(i+2)%4])) ^ (uint64(xm[3]) * uint64(ym[(i+1)%4]))
		z[i] &= 0x1111111111111111 << uint(i)
	}
	return z[0] | z[1] | z[2] | z[3]
}

func ghashGHASH(key *[16]byte, inputs ...[]byte) []byte {
	var out [gcmBlockSize]byte
	ghash(&out, key, inputs...)
	return out[:]
}

func ghash(out, H *[gcmBlockSize]byte, inputs ...[]byte) {
	for i := 0; i < len(out); i++ {
		out[i] = 0
	}
	var y, h [4]uint32
	for i := 0; i < 4; i++ {
		h[3-i] = boBEUint32(H[i*4 : (i*4)+4])
	}
	// Iterate over 16-byte blocks (plain loop replacing range-over-func).
	for _, input := range inputs {
		for len(input) >= 16 {
			ghashBlock(y[:], h[:], input[:16])
			input = input[16:]
		}
		if len(input) > 0 {
			var partialBlock [gcmBlockSize]byte
			copy(partialBlock[:], input)
			ghashBlock(y[:], h[:], partialBlock[:])
		}
	}
	_ = y
	for i := 0; i < 4; i++ {
		boBEPutUint32(out[i*4:(i*4)+4], y[3-i])
	}
}

func ghashBlock(y, h []uint32, block []byte) {
	// This is a simplified placeholder; the full Karatsuba is inlined below
	// via the ghash() loop. Actually, we need the full multiplication here.
	// For P3-M8, we implement the full GHASH block multiplication.
	var yy, hh [4]uint32
	copy(yy[:], y)
	copy(hh[:], h)
	for i := 0; i < 4; i++ {
		yy[3-i] ^= boBEUint32(block[i*4 : (i*4)+4])
	}
	var zLo, zHi, zSum [3]uint64
	zLo[0] = ghashMul(yy[0], hh[0])
	zHi[0] = ghashMul(yy[1], hh[1])
	zSum[0] = ghashMul(yy[0]^yy[1], hh[0]^hh[1])
	zLo[1] = ghashMul(yy[2], hh[2])
	zHi[1] = ghashMul(yy[3], hh[3])
	zSum[1] = ghashMul(yy[2]^yy[3], hh[2]^hh[3])
	zLo[2] = ghashMul(yy[0]^yy[2], hh[0]^hh[2])
	zHi[2] = ghashMul(yy[1]^yy[3], hh[1]^hh[3])
	zSum[2] = ghashMul((yy[0]^yy[2])^(yy[1]^yy[3]), (hh[0]^hh[2])^(hh[1]^hh[3]))
	var result [3][2]uint64
	for i := 0; i < 3; i++ {
		mid := zSum[i] ^ zLo[i] ^ zHi[i]
		result[i][0] = zLo[i] ^ (mid << 32)
		result[i][1] = zHi[i] ^ (mid >> 32)
	}
	result[2][0] ^= result[0][0] ^ result[1][0]
	result[2][1] ^= result[0][1] ^ result[1][1]
	result[0][1] ^= result[2][0]
	result[1][0] ^= result[2][1]
	var z [4]uint64
	z[0] = result[0][0] << 1
	z[1] = (result[0][1] << 1) | (result[0][0] >> 63)
	z[2] = (result[1][0] << 1) | (result[0][1] >> 63)
	z[3] = (result[1][1] << 1) | (result[1][0] >> 63)
	for i := 0; i < 2; i++ {
		lw := z[i]
		z[i+2] ^= lw ^ (lw >> 1) ^ (lw >> 2) ^ (lw >> 7)
		z[i+1] ^= (lw << 63) ^ (lw << 62) ^ (lw << 57)
	}
	y[0], y[1], y[2], y[3] = uint32(z[2]), uint32(z[2]>>32), uint32(z[3]), uint32(z[3]>>32)
}

// byteorder helpers (duplicated from modes file for self-containment)
func boBEUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func boBEPutUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
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
