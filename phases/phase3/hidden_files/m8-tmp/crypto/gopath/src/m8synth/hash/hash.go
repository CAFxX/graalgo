package hash

import (
	"io"
)

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hash provides interfaces for hash functions.

// Hash is the common interface implemented by all hash functions.
//
// Hash implementations in the standard library (e.g. [hash/crc32] and
// [crypto/sha256]) implement the [encoding.BinaryMarshaler], [encoding.BinaryAppender],
// [encoding.BinaryUnmarshaler] and [Cloner] interfaces. Marshaling a hash implementation
// allows its internal state to be saved and used for additional processing
// later, without having to re-write the data previously written to the hash.
// The hash state may contain portions of the input in its original form,
// which users are expected to handle for any possible security implications.
//
// Compatibility: Any future changes to hash or crypto packages will endeavor
// to maintain compatibility with state encoded using previous versions.
// That is, any released versions of the packages should be able to
// decode data written with any previously released version,
// subject to issues such as security fixes.
// See the Go compatibility document for background: https://golang.org/doc/go1compat
type Hash interface {
	// Write (via the embedded io.Writer interface) adds more data to the running hash.
	// It never returns an error.
	io.Writer

	// Sum appends the current hash to b and returns the resulting slice.
	// It does not change the underlying hash state.
	Sum(b []byte) []byte

	// Reset resets the Hash to its initial state.
	Reset()

	// Size returns the number of bytes Sum will return.
	Size() int

	// BlockSize returns the hash's underlying block size.
	// The Write method must be able to accept any amount
	// of data, but it may operate more efficiently if all writes
	// are a multiple of the block size.
	BlockSize() int
}

// Hash32 is the common interface implemented by all 32-bit hash functions.
type Hash32 interface {
	Hash
	Sum32() uint32
}

// Hash64 is the common interface implemented by all 64-bit hash functions.
type Hash64 interface {
	Hash
	Sum64() uint64
}

// A Cloner is a hash function whose state can be cloned, returning a value with
// equivalent and independent state.
//
// All [Hash] implementations in the standard library implement this interface,
// unless GOFIPS140=v1.0.0 is set.
//
// If a hash can only determine at runtime if it can be cloned (e.g. if it wraps
// another hash), Clone may return an error wrapping [errors.ErrUnsupported].
// Otherwise, Clone must always return a nil error.
type Cloner interface {
	Hash
	Clone() (Cloner, error)
}

// XOF (extendable output function) is a hash function with arbitrary or unlimited output length.
type XOF interface {
	// Write absorbs more data into the XOF's state. It panics if called
	// after Read.
	io.Writer

	// Read reads more output from the XOF. It may return io.EOF if there
	// is a limit to the XOF output length.
	io.Reader

	// Reset resets the XOF to its initial state.
	Reset()

	// BlockSize returns the XOF's underlying block size.
	// The Write method must be able to accept any amount
	// of data, but it may operate more efficiently if all writes
	// are a multiple of the block size.
	BlockSize() int
}

// ClonePair is the result of cloning a hash. It is exported only so that
// synthetic hash implementations in other packages (crypto/hmac) can
// implement hash cloning with single-result methods: the v1 frontend
// cannot emit or call multi-result functions (WS D), so Clone is served
// through the mrM8 desugar (see m8-staging/crypto/03-mrdesugar.patch)
// and the real logic lives in cloneSinglePair. Not part of the gc hash
// API. Re-test trigger: WS D lands.
type ClonePair struct {
	C   Cloner
	Err error
}

type hashWritePair struct {
	n   int
	err error
}

type hashReadPair struct {
	n   int
	err error
}

// __hashWrite serves h.Write(p) through the mrM8 desugar. The digest's
// public Write has the exact gc signature with a fail-fast body; the
// real logic is the single-result WriteSingle, which every synthetic
// digest implements. Custom (non-synthetic) hash.Hash implementations
// fail fast here: there is no v1-expressible way to call their
// multi-result Write (re-test: WS D).
func __hashWrite(h Hash, p []byte) hashWritePair {
	if w, ok := h.(interface{ WriteSingle([]byte) int }); ok {
		return hashWritePair{w.WriteSingle(p), nil}
	}
	panic("hash: Write on non-synthetic hash.Hash implementation (needs WS D; re-test when it lands)")
}

// __hashRead serves x.Read(p) for hash.XOF through the mrM8 desugar.
func __hashRead(x XOF, p []byte) hashReadPair {
	if r, ok := x.(interface{ readSingle([]byte) int }); ok {
		return hashReadPair{r.readSingle(p), nil}
	}
	panic("hash: Read on non-synthetic hash.XOF implementation (needs WS D; re-test when it lands)")
}

// __hashClone serves h.Clone() through the mrM8 desugar.
func __hashClone(h Hash) ClonePair {
	if c, ok := h.(interface{ cloneSinglePair() ClonePair }); ok {
		return c.cloneSinglePair()
	}
	panic("hash: Clone on non-synthetic hash.Hash implementation (needs WS D; re-test when it lands)")
}

// binaryMarshalPair is the single-result form of MarshalBinary's results.
type binaryMarshalPair struct {
	b   []byte
	err error
}

// binaryAppendPair is the single-result form of AppendBinary's results.
type binaryAppendPair struct {
	b   []byte
	err error
}

// __binaryMarshal serves m.MarshalBinary() through the mrM8 desugar
// (table entry pkgPath "encoding", helperPkg "m8synth/hash"; see
// m8-staging/crypto/03-mrdesugar.patch). The marshaler is referenced
// structurally so this package does not import encoding.
// Re-test trigger: WS D lands.
func __binaryMarshal(m interface {
	MarshalSingle() []byte
}) binaryMarshalPair {
	return binaryMarshalPair{m.MarshalSingle(), nil}
}

// __binaryAppend serves m.AppendBinary(b) through the mrM8 desugar
// (table entry pkgPath "encoding", helperPkg "m8synth/hash").
func __binaryAppend(m interface {
	AppendSingle([]byte) []byte
}, b []byte) binaryAppendPair {
	return binaryAppendPair{m.AppendSingle(b), nil}
}
