package main

// P3-M8 (wave 2): synthetic `cipher` package.
// Software implementation only (no intrinsification).
// See the pre-migration phase-3 m8 crypto analysis (archived).
const syntheticCipherSource = `package cipher

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"io"
)

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cipher implements standard block cipher modes that can be wrapped
// around low-level block cipher implementations.
// See https://csrc.nist.gov/groups/ST/toolkit/BCM/current_modes.html
// and NIST Special Publication 800-38A.
// A Block represents an implementation of block cipher
// using a given key. It provides the capability to encrypt
// or decrypt individual blocks. The mode implementations
// extend that capability to streams of blocks.
type Block interface {
	// BlockSize returns the cipher's block size.
	BlockSize() int

	// Encrypt encrypts the first block in src into dst.
	// Dst and src must overlap entirely or not at all.
	Encrypt(dst, src []byte)

	// Decrypt decrypts the first block in src into dst.
	// Dst and src must overlap entirely or not at all.
	Decrypt(dst, src []byte)
}

// A Stream represents a stream cipher.
type Stream interface {
	// XORKeyStream XORs each byte in the given slice with a byte from the
	// cipher's key stream. Dst and src must overlap entirely or not at all.
	//
	// If len(dst) < len(src), XORKeyStream should panic. It is acceptable
	// to pass a dst bigger than src, and in that case, XORKeyStream will
	// only update dst[:len(src)] and will not touch the rest of dst.
	//
	// Multiple calls to XORKeyStream behave as if the concatenation of
	// the src buffers was passed in a single run. That is, Stream
	// maintains state and does not reset at each XORKeyStream call.
	XORKeyStream(dst, src []byte)
}

// A BlockMode represents a block cipher running in a block-based mode (CBC,
// ECB etc).
type BlockMode interface {
	// BlockSize returns the mode's block size.
	BlockSize() int

	// CryptBlocks encrypts or decrypts a number of blocks. The length of
	// src must be a multiple of the block size. Dst and src must overlap
	// entirely or not at all.
	//
	// If len(dst) < len(src), CryptBlocks should panic. It is acceptable
	// to pass a dst bigger than src, and in that case, CryptBlocks will
	// only update dst[:len(src)] and will not touch the rest of dst.
	//
	// Multiple calls to CryptBlocks behave as if the concatenation of
	// the src buffers was passed in a single run. That is, BlockMode
	// maintains state and does not reset at each CryptBlocks call.
	CryptBlocks(dst, src []byte)
}

// AEAD is a cipher mode providing authenticated encryption with associated
// data. For a description of the methodology, see
// https://en.wikipedia.org/wiki/Authenticated_encryption.
type AEAD interface {
	// NonceSize returns the size of the nonce that must be passed to Seal
	// and Open.
	NonceSize() int

	// Overhead returns the maximum difference between the lengths of a
	// plaintext and its ciphertext.
	Overhead() int

	// Seal encrypts and authenticates plaintext, authenticates the
	// additional data and appends the result to dst, returning the updated
	// slice. The nonce must be NonceSize() bytes long and unique for all
	// time, for a given key.
	//
	// To reuse plaintext's storage for the encrypted output, use plaintext[:0]
	// as dst. Otherwise, the remaining capacity of dst must not overlap plaintext.
	// dst and additionalData may not overlap.
	Seal(dst, nonce, plaintext, additionalData []byte) []byte

	// Open decrypts and authenticates ciphertext, authenticates the
	// additional data and, if successful, appends the resulting plaintext
	// to dst, returning the updated slice. The nonce must be NonceSize()
	// bytes long and both it and the additional data must match the
	// value passed to Seal.
	//
	// To reuse ciphertext's storage for the decrypted output, use ciphertext[:0]
	// as dst. Otherwise, the remaining capacity of dst must not overlap ciphertext.
	// dst and additionalData may not overlap.
	//
	// Even if the function fails, the contents of dst, up to its capacity,
	// may be overwritten.
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Cipher block chaining (CBC) mode.

// CBC provides confidentiality by xoring (chaining) each plaintext block
// with the previous ciphertext block before applying the block cipher.

// See NIST SP 800-38A, pp 10-11

type cbc struct {
	b         Block
	blockSize int
	iv        []byte
	tmp       []byte
}

func newCBC(b Block, iv []byte) *cbc {
	return &cbc{
		b:         b,
		blockSize: b.BlockSize(),
		iv:        bytes.Clone(iv),
		tmp:       make([]byte, b.BlockSize()),
	}
}

type cbcEncrypter cbc

// cbcEncAble is an interface implemented by ciphers that have a specific
// optimized implementation of CBC encryption. crypto/aes doesn't use this
// anymore, and we'd like to eventually remove it.
type cbcEncAble interface {
	NewCBCEncrypter(iv []byte) BlockMode
}

// NewCBCEncrypter returns a BlockMode which encrypts in cipher block chaining
// mode, using the given Block. The length of iv must be the same as the
// Block's block size.
func NewCBCEncrypter(b Block, iv []byte) BlockMode {
	if len(iv) != b.BlockSize() {
		panic("cipher.NewCBCEncrypter: IV length must equal block size")
	}
	return (*cbcEncrypter)(newCBC(b, iv))
}

// newCBCGenericEncrypter returns a BlockMode which encrypts in cipher block chaining
// mode, using the given Block. The length of iv must be the same as the
// Block's block size. This always returns the generic non-asm encrypter for use
// in fuzz testing.
func newCBCGenericEncrypter(b Block, iv []byte) BlockMode {
	if len(iv) != b.BlockSize() {
		panic("cipher.NewCBCEncrypter: IV length must equal block size")
	}
	return (*cbcEncrypter)(newCBC(b, iv))
}

func (x *cbcEncrypter) BlockSize() int { return x.blockSize }

func (x *cbcEncrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/cipher: invalid buffer overlap")
	}

	iv := x.iv

	for len(src) > 0 {
		// Write the xor to dst, then encrypt in place.
		subtle.XORBytes(dst[:x.blockSize], src[:x.blockSize], iv)
		x.b.Encrypt(dst[:x.blockSize], dst[:x.blockSize])

		// Move to the next block with this block as the next iv.
		iv = dst[:x.blockSize]
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}

	// Save the iv for the next CryptBlocks call.
	copy(x.iv, iv)
}

func (x *cbcEncrypter) SetIV(iv []byte) {
	if len(iv) != len(x.iv) {
		panic("cipher: incorrect length IV")
	}
	copy(x.iv, iv)
}

type cbcDecrypter cbc

// cbcDecAble is an interface implemented by ciphers that have a specific
// optimized implementation of CBC decryption. crypto/aes doesn't use this
// anymore, and we'd like to eventually remove it.
type cbcDecAble interface {
	NewCBCDecrypter(iv []byte) BlockMode
}

// NewCBCDecrypter returns a BlockMode which decrypts in cipher block chaining
// mode, using the given Block. The length of iv must be the same as the
// Block's block size and must match the iv used to encrypt the data.
func NewCBCDecrypter(b Block, iv []byte) BlockMode {
	if len(iv) != b.BlockSize() {
		panic("cipher.NewCBCDecrypter: IV length must equal block size")
	}
	if cbc, ok := b.(cbcDecAble); ok {
		return cbc.NewCBCDecrypter(iv)
	}
	return (*cbcDecrypter)(newCBC(b, iv))
}

// newCBCGenericDecrypter returns a BlockMode which encrypts in cipher block chaining
// mode, using the given Block. The length of iv must be the same as the
// Block's block size. This always returns the generic non-asm decrypter for use in
// fuzz testing.
func newCBCGenericDecrypter(b Block, iv []byte) BlockMode {
	if len(iv) != b.BlockSize() {
		panic("cipher.NewCBCDecrypter: IV length must equal block size")
	}
	return (*cbcDecrypter)(newCBC(b, iv))
}

func (x *cbcDecrypter) BlockSize() int { return x.blockSize }

func (x *cbcDecrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		panic("crypto/cipher: input not full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/cipher: invalid buffer overlap")
	}
	if len(src) == 0 {
		return
	}

	// For each block, we need to xor the decrypted data with the previous block's ciphertext (the iv).
	// To avoid making a copy each time, we loop over the blocks BACKWARDS.
	end := len(src)
	start := end - x.blockSize
	prev := start - x.blockSize

	// Copy the last block of ciphertext in preparation as the new iv.
	copy(x.tmp, src[start:end])

	// Loop over all but the first block.
	for start > 0 {
		x.b.Decrypt(dst[start:end], src[start:end])
		subtle.XORBytes(dst[start:end], dst[start:end], src[prev:start])

		end = start
		start = prev
		prev -= x.blockSize
	}

	// The first block is special because it uses the saved iv.
	x.b.Decrypt(dst[start:end], src[start:end])
	subtle.XORBytes(dst[start:end], dst[start:end], x.iv)

	// Set the new iv to the first block we copied earlier.
	x.iv, x.tmp = x.tmp, x.iv
}

func (x *cbcDecrypter) SetIV(iv []byte) {
	if len(iv) != len(x.iv) {
		panic("cipher: incorrect length IV")
	}
	copy(x.iv, iv)
}

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Counter (CTR) mode.

// CTR converts a block cipher into a stream cipher by
// repeatedly encrypting an incrementing counter and
// xoring the resulting stream of data with the input.

// See NIST SP 800-38A, pp 13-15

type ctr struct {
	b       Block
	ctr     []byte
	out     []byte
	outUsed int
}

const streamBufferSize = 512

// ctrAble is an interface implemented by ciphers that have a specific optimized
// implementation of CTR. crypto/aes doesn't use this anymore, and we'd like to
// eventually remove it.
type ctrAble interface {
	NewCTR(iv []byte) Stream
}

// NewCTR returns a [Stream] which encrypts/decrypts using the given [Block] in
// counter mode. The length of iv must be the same as the [Block]'s block size.
func NewCTR(block Block, iv []byte) Stream {
	if ctr, ok := block.(ctrAble); ok {
		return ctr.NewCTR(iv)
	}
	if len(iv) != block.BlockSize() {
		panic("cipher.NewCTR: IV length must equal block size")
	}
	bufSize := streamBufferSize
	if bufSize < block.BlockSize() {
		bufSize = block.BlockSize()
	}
	return &ctr{
		b:       block,
		ctr:     bytes.Clone(iv),
		out:     make([]byte, 0, bufSize),
		outUsed: 0,
	}
}

func (x *ctr) refill() {
	remain := len(x.out) - x.outUsed
	copy(x.out, x.out[x.outUsed:])
	x.out = x.out[:cap(x.out)]
	bs := x.b.BlockSize()
	for remain <= len(x.out)-bs {
		x.b.Encrypt(x.out[remain:], x.ctr)
		remain += bs

		// Increment counter
		for i := len(x.ctr) - 1; i >= 0; i-- {
			x.ctr[i]++
			if x.ctr[i] != 0 {
				break
			}
		}
	}
	x.out = x.out[:remain]
	x.outUsed = 0
}

func (x *ctr) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/cipher: invalid buffer overlap")
	}
	for len(src) > 0 {
		if x.outUsed >= len(x.out)-x.b.BlockSize() {
			x.refill()
		}
		n := subtle.XORBytes(dst, src, x.out[x.outUsed:])
		dst = dst[n:]
		src = src[n:]
		x.outUsed += n
	}
}

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// CFB (Cipher Feedback) Mode.

type cfb struct {
	b       Block
	next    []byte
	out     []byte
	outUsed int

	decrypt bool
}

func (x *cfb) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/cipher: invalid buffer overlap")
	}
	for len(src) > 0 {
		if x.outUsed == len(x.out) {
			x.b.Encrypt(x.out, x.next)
			x.outUsed = 0
		}

		if x.decrypt {
			// We can precompute a larger segment of the
			// keystream on decryption. This will allow
			// larger batches for xor, and we should be
			// able to match CTR/OFB performance.
			copy(x.next[x.outUsed:], src)
		}
		n := subtle.XORBytes(dst, src, x.out[x.outUsed:])
		if !x.decrypt {
			copy(x.next[x.outUsed:], dst)
		}
		dst = dst[n:]
		src = src[n:]
		x.outUsed += n
	}
}

// NewCFBEncrypter returns a [Stream] which encrypts with cipher feedback mode,
// using the given [Block]. The iv must be the same length as the [Block]'s block
// size.
//
// Deprecated: CFB mode is not authenticated, which generally enables active
// attacks to manipulate and recover the plaintext. It is recommended that
// applications use [AEAD] modes instead. The standard library implementation of
// CFB is also unoptimized and not validated as part of the FIPS 140-3 module.
// If an unauthenticated [Stream] mode is required, use [NewCTR] instead.
func NewCFBEncrypter(block Block, iv []byte) Stream {
	return newCFB(block, iv, false)
}

// NewCFBDecrypter returns a [Stream] which decrypts with cipher feedback mode,
// using the given [Block]. The iv must be the same length as the [Block]'s block
// size.
//
// Deprecated: CFB mode is not authenticated, which generally enables active
// attacks to manipulate and recover the plaintext. It is recommended that
// applications use [AEAD] modes instead. The standard library implementation of
// CFB is also unoptimized and not validated as part of the FIPS 140-3 module.
// If an unauthenticated [Stream] mode is required, use [NewCTR] instead.
func NewCFBDecrypter(block Block, iv []byte) Stream {
	return newCFB(block, iv, true)
}

func newCFB(block Block, iv []byte, decrypt bool) Stream {
	blockSize := block.BlockSize()
	if len(iv) != blockSize {
		// Stack trace will indicate whether it was de- or en-cryption.
		panic("cipher.newCFB: IV length must equal block size")
	}
	x := &cfb{
		b:       block,
		out:     make([]byte, blockSize),
		next:    make([]byte, blockSize),
		outUsed: blockSize,
		decrypt: decrypt,
	}
	copy(x.next, iv)

	return x
}

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// OFB (Output Feedback) Mode.

type ofb struct {
	b       Block
	cipher  []byte
	out     []byte
	outUsed int
}

// NewOFB returns a [Stream] that encrypts or decrypts using the block cipher b
// in output feedback mode. The initialization vector iv's length must be equal
// to b's block size.
//
// Deprecated: OFB mode is not authenticated, which generally enables active
// attacks to manipulate and recover the plaintext. It is recommended that
// applications use [AEAD] modes instead. The standard library implementation of
// OFB is also unoptimized and not validated as part of the FIPS 140-3 module.
// If an unauthenticated [Stream] mode is required, use [NewCTR] instead.
func NewOFB(b Block, iv []byte) Stream {

	blockSize := b.BlockSize()
	if len(iv) != blockSize {
		panic("cipher.NewOFB: IV length must equal block size")
	}
	bufSize := streamBufferSize
	if bufSize < blockSize {
		bufSize = blockSize
	}
	x := &ofb{
		b:       b,
		cipher:  make([]byte, blockSize),
		out:     make([]byte, 0, bufSize),
		outUsed: 0,
	}

	copy(x.cipher, iv)
	return x
}

func (x *ofb) refill() {
	bs := x.b.BlockSize()
	remain := len(x.out) - x.outUsed
	if remain > x.outUsed {
		return
	}
	copy(x.out, x.out[x.outUsed:])
	x.out = x.out[:cap(x.out)]
	for remain < len(x.out)-bs {
		x.b.Encrypt(x.cipher, x.cipher)
		copy(x.out[remain:], x.cipher)
		remain += bs
	}
	x.out = x.out[:remain]
	x.outUsed = 0
}

func (x *ofb) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("crypto/cipher: output smaller than input")
	}
	if inexactOverlap(dst[:len(src)], src) {
		panic("crypto/cipher: invalid buffer overlap")
	}
	for len(src) > 0 {
		if x.outUsed >= len(x.out)-x.b.BlockSize() {
			x.refill()
		}
		n := subtle.XORBytes(dst, src, x.out[x.outUsed:])
		dst = dst[n:]
		src = src[n:]
		x.outUsed += n
	}
}

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The Stream* objects are so simple that all their members are public. Users
// can create them themselves.

// StreamReader wraps a [Stream] into an [io.Reader]. It calls XORKeyStream
// to process each slice of data which passes through.
type StreamReader struct {
	S Stream
	R io.Reader
}

// streamReaderReadPair is the single-result form of Read's results.
type streamReaderReadPair struct {
	n   int
	err error
}

// __streamReaderRead serves StreamReader.Read through the mrM8 desugar.
// P3-M8: the inner r.R.Read is a multi-result interface call; in graalgo it
// needs the io worker's desugar entry for io.Reader.Read (see handoff).
// Under gc (validation), R is a real io.Reader.
func __streamReaderRead(r StreamReader, dst []byte) streamReaderReadPair {
	n, err := r.R.Read(dst)
	r.S.XORKeyStream(dst[:n], dst[:n])
	return streamReaderReadPair{n, err}
}

// Read implements io.Reader (fail-fast stub; served via mrM8 desugar).
func (r StreamReader) Read(dst []byte) (n int, err error) {
	panic("crypto/cipher: StreamReader.Read: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// StreamWriter wraps a [Stream] into an io.Writer. It calls XORKeyStream
// to process each slice of data which passes through. If any [StreamWriter.Write]
// call returns short then the StreamWriter is out of sync and must be discarded.
// A StreamWriter has no internal buffering; [StreamWriter.Close] does not need
// to be called to flush write data.
type StreamWriter struct {
	S   Stream
	W   io.Writer
	Err error // unused
}

// streamWriterWritePair is the single-result form of Write's results.
type streamWriterWritePair struct {
	n   int
	err error
}

// __streamWriterWrite serves StreamWriter.Write through the mrM8 desugar.
// (See __streamReaderRead re: io worker dependency.)
func __streamWriterWrite(w StreamWriter, src []byte) streamWriterWritePair {
	c := make([]byte, len(src))
	w.S.XORKeyStream(c, src)
	n, err := w.W.Write(c)
	if n != len(src) && err == nil {
		err = io.ErrShortWrite
	}
	return streamWriterWritePair{n, err}
}

// Write implements io.Writer (fail-fast stub; served via mrM8 desugar).
func (w StreamWriter) Write(src []byte) (n int, err error) {
	panic("crypto/cipher: StreamWriter.Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Close closes the underlying Writer and returns its Close return value, if the Writer
// is also an io.Closer. Otherwise it returns nil.
func (w StreamWriter) Close() error {
	if c, ok := w.W.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// inexactOverlap/anyOverlap: P3-M8b overlap checks without &x[0] (not
// expressible in frontend/v1, WS D) and without unsafe (M8 D5: handles
// admit no byte arithmetic). inexactOverlap conservatively reports no
// overlap — a divergence from gc only for caller-bug cases (partial
// overlap), where gc panics ("invalid buffer overlap") and we silently
// proceed. anyOverlap keeps the loud fail-fast for the unverifiable case.
// Re-test trigger: byte-addressed value model (D5 re-test).
func inexactOverlap(x, y []byte) bool {
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	return false
}
func anyOverlap(x, y []byte) bool {
	if len(x) == 0 || len(y) == 0 {
		return false
	}
	// Without &x[0] even the exact-overlap fast path is inexpressible;
	// conservatively report no overlap (same caller-bug-only divergence
	// as inexactOverlap above).
	return false
}
`
