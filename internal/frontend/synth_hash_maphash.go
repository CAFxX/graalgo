// SYNTHETIC (Principle 4) — hash/maphash: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 (wave 3): synthetic `hash/maphash` package.
//
// VALUE DIVERGENCE (documented, deliberate): the stdlib's maphash
// funnels every input through runtime.memhash (//go:linkname into the
// Go runtime; AES-based on amd64), which no pure-Go synthetic can
// reproduce bit-exactly. This synthetic therefore implements the FULL
// maphash API over its own pure-Go seeded 64-bit hash (splitmix64
// finalizer mixing, 128-byte write buffer à la the stdlib so Write /
// WriteByte / WriteString are chunking-invariant). For a fixed seed the
// values are deterministic within a graalgo process, but they are NOT
// equal to gc's values for the same seed. The wave-3 corpus pins only
// STRUCTURAL properties (chunking invariance, Write/WriteByte/
// WriteString equivalence, seed sensitivity, Reset/Clone semantics,
// Bytes/String/Hash consistency, zero-Seed panics) — never exact hash
// values. MakeSeed draws real entropy from the synthetic crypto/rand
// (graalgo.os.getrandom intrinsic, differential in wave 2).
//
// The generic surface (Hasher, ComparableHasher, Comparable,
// WriteComparable) is implemented for basic kinds via a type switch;
// composite kinds fail fast with a clear panic (hashing them needs the
// runtime typehash, which has no synthetic equivalent).
//
// KAT: differential vs gc on the wave-3 hash/maphash corpus
// (structural properties only, per the divergence note above).
const syntheticMaphashSource = `package maphash

import (
	"crypto/rand"
	"hash"
	"math"
)

// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package maphash provides hash functions on byte sequences and comparable values.
// It also defines [Hasher], the interface between a hash function and a hash table.
//
// These hash functions are intended to be used to implement hash
// tables, Bloom filters, and other data structures that need to map
// arbitrary strings or byte sequences to a uniform distribution on
// unsigned 64-bit integers.
//
// Each different instance of a hash table or data structure should use its own [Seed].
//
// The hash functions are not cryptographically secure.
// (See crypto/sha256 and crypto/sha512 for cryptographic use.)
//
// P3-M9: pure-Go software port (no intrinsification). VALUE DIVERGENCE:
// values for a fixed seed are deterministic but NOT gc-identical (the
// stdlib uses the runtime memhash intrinsic). See the header above.

// A Seed is a random value that selects the specific hash function
// computed by a [Hash]. If two Hashes use the same Seeds, they
// will compute the same hash values for any given input.
// If two Hashes use different Seeds, they are very likely to compute
// distinct hash values for any given input.
//
// A Seed must be initialized by calling [MakeSeed].
// The zero seed is uninitialized and not valid for use with [Hash]'s SetSeed method.
//
// Each Seed value is local to a single process and cannot be serialized
// or otherwise recreated in a different process.
type Seed struct {
	s uint64
}

// bufSize is the size of the Hash write buffer.
// The buffer ensures that writes depend only on the sequence of bytes,
// not on the sequence of WriteByte/Write/WriteString calls.
const bufSize = 128

// mix64 is the splitmix64 finalizer.
func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func le64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// MakeSeed returns a random Seed.
func MakeSeed() Seed {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("maphash: MakeSeed: " + err.Error())
	}
	s := le64(b[:])
	if s == 0 {
		// The zero seed is reserved as "uninitialized"; remap the
		// (2^-64-probable) draw to a fixed nonzero value.
		s = 0x9e3779b97f4a7c15
	}
	return Seed{s: s}
}

// A Hash computes a seeded hash of a byte sequence.
//
// The zero Hash is a valid Hash ready to use.
// A zero Hash chooses a random seed for itself during
// the first call to a Reset, Write, Seed, Clone, or Sum64 method.
// For control over the seed, use SetSeed.
//
// The computed hash values depend only on the initial seed and
// the sequence of bytes provided to the Hash object, not on the way
// in which the bytes are provided. For example, the three sequences
//
//	h.Write([]byte{'f','o','o'})
//	h.WriteByte('f'); h.WriteByte('o'); h.WriteByte('o')
//	h.WriteString("foo")
//
// all have the same effect.
//
// Hashes are intended to be collision-resistant, even for situations
// where an adversary controls the byte sequences being hashed.
//
// A Hash is not safe for concurrent use by multiple goroutines, but a Seed is.
// If multiple goroutines must compute the same seeded hash,
// each can declare its own Hash and call SetSeed with a common Seed.
type Hash struct {
	seed  uint64      // initial seed; 0 = uninitialized
	state uint64      // current hash of all flushed bytes
	buf   [bufSize]byte // unflushed byte buffer
	n     int         // number of unflushed bytes
	total uint64      // number of flushed bytes
}

func (h *Hash) initSeed() {
	if h.seed == 0 {
		s := MakeSeed()
		h.seed = s.s
		h.state = s.s
	}
}

// absorb mixes one full buffer into the state.
func (h *Hash) absorb() {
	for i := 0; i < bufSize; i += 8 {
		h.state = mix64(h.state ^ le64(h.buf[i:i+8]))
	}
	h.total += bufSize
	h.n = 0
}

// WriteByte adds a single byte to the hash.
func (h *Hash) WriteByte(b byte) error {
	h.initSeed()
	if h.n == bufSize {
		h.absorb()
	}
	h.buf[h.n] = b
	h.n++
	return nil
}

// maphashWritePair is the single-result form of Write/WriteString's
// results (the v1 frontend cannot emit or call multi-result functions
// (WS D)). Served via the mrM8 desugar to __maphashWrite/
// __maphashWriteString. Re-test trigger: WS D lands.
type maphashWritePair struct {
	n   int
	err error
}

// __maphashWrite serves h.Write(p) through the mrM8 desugar, delegating
// to the single-result WriteSingle. Re-test trigger: WS D lands.
func __maphashWrite(h *Hash, p []byte) maphashWritePair {
	return maphashWritePair{h.WriteSingle(p), nil}
}

// __maphashWriteString serves h.WriteString(s) through the mrM8 desugar,
// delegating to the single-result WriteStringSingle. Re-test trigger:
// WS D lands.
func __maphashWriteString(h *Hash, s string) maphashWritePair {
	return maphashWritePair{h.WriteStringSingle(s), nil}
}

// WriteSingle is the single-result form of Write (the v1 frontend cannot
// emit or call multi-result functions (WS D); the real logic lives here,
// served via the mrM8 desugar to __maphashWrite). Re-test trigger:
// WS D lands.
func (h *Hash) WriteSingle(b []byte) int {
	h.initSeed()
	n := len(b)
	for len(b) > 0 {
		if h.n == bufSize {
			h.absorb()
		}
		c := copy(h.buf[h.n:bufSize], b)
		h.n += c
		b = b[c:]
	}
	return n
}

// Write adds b to the hash (fail-fast stub; real logic is WriteSingle,
// served via the mrM8 desugar to __maphashWrite). Re-test trigger:
// WS D lands.
func (h *Hash) Write(b []byte) (int, error) {
	panic("hash/maphash: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// WriteStringSingle is the single-result form of WriteString (see
// WriteSingle). Served via the mrM8 desugar to __maphashWriteString.
// Re-test trigger: WS D lands.
func (h *Hash) WriteStringSingle(s string) int {
	h.initSeed()
	b := []byte(s)
	for len(b) > 0 {
		if h.n == bufSize {
			h.absorb()
		}
		c := copy(h.buf[h.n:bufSize], b)
		h.n += c
		b = b[c:]
	}
	return len(s)
}

// WriteString adds s to the hash (fail-fast stub; real logic is
// WriteStringSingle, served via the mrM8 desugar to
// __maphashWriteString). Re-test trigger: WS D lands.
func (h *Hash) WriteString(s string) (int, error) {
	panic("hash/maphash: WriteString: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Seed returns the seed used to initialize the hash.
func (h *Hash) Seed() Seed {
	h.initSeed()
	return Seed{s: h.seed}
}

// SetSeed sets the seed used to initialize the hash.
// The seed must have been created by MakeSeed; the zero Seed panics.
func (h *Hash) SetSeed(seed Seed) {
	if seed.s == 0 {
		panic("maphash: use of uninitialized Seed")
	}
	h.seed = seed.s
	h.state = seed.s
	h.n = 0
	h.total = 0
}

// Reset resets the hash to its initial state (keeping the seed).
func (h *Hash) Reset() {
	h.initSeed()
	h.state = h.seed
	h.n = 0
	h.total = 0
}

// Sum64 returns the 64-bit hash. It does not change the hash state.
func (h *Hash) Sum64() uint64 {
	h.initSeed()
	st := h.state
	// Absorb the tail (zero-padded to a full block) without mutating h.
	var blk [bufSize]byte
	copy(blk[:], h.buf[:h.n])
	for i := 0; i < bufSize; i += 8 {
		st = mix64(st ^ le64(blk[i:i+8]))
	}
	st ^= h.total + uint64(h.n)
	return mix64(st)
}

// Sum appends the current hash to b and returns the resulting slice.
// It does not change the underlying hash state.
func (h *Hash) Sum(b []byte) []byte {
	s := h.Sum64()
	// P3-M9: gc's maphash Sum appends in little-endian (verified vs gc 1.27.1).
	return append(b, byte(s), byte(s>>8), byte(s>>16), byte(s>>24),
		byte(s>>32), byte(s>>40), byte(s>>48), byte(s>>56))
}

// Size returns the number of bytes Sum will return.
func (h *Hash) Size() int { return 8 }

// BlockSize returns h's block size.
func (h *Hash) BlockSize() int { return bufSize }

// maphashClonePair is the single-result form of Clone's results (the v1
// frontend cannot emit or call multi-result functions (WS D)). Served via
// the mrM8 desugar to __maphashClone. Re-test trigger: WS D lands.
type maphashClonePair struct {
	c   hash.Cloner
	err error
}

// __maphashClone serves h.Clone() through the mrM8 desugar, delegating
// to the single-result CloneSinglePair. Re-test trigger: WS D lands.
func __maphashClone(h *Hash) maphashClonePair {
	return h.CloneSinglePair()
}

// CloneSinglePair is the single-result form of Clone (see WriteSingle).
// Served via the mrM8 desugar to __maphashClone. Re-test trigger:
// WS D lands.
func (h *Hash) CloneSinglePair() maphashClonePair {
	h.initSeed()
	// P3-M9: composite literal (the frontend forbids &ident in
	// synthetic bodies, WS D).
	return maphashClonePair{&Hash{seed: h.seed, state: h.state, buf: h.buf, n: h.n, total: h.total}, nil}
}

// Clone returns a copy of h with equivalent and independent state
// (fail-fast stub; real logic is CloneSinglePair, served via the mrM8
// desugar to __maphashClone). Re-test trigger: WS D lands.
func (h *Hash) Clone() (hash.Cloner, error) {
	panic("hash/maphash: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// Bytes returns the hash of b with the given seed.
//
// Bytes is equivalent to, but more convenient and efficient than:
//
//	var h Hash
//	h.SetSeed(seed)
//	h.Write(b)
//	return h.Sum64()
func Bytes(seed Seed, b []byte) uint64 {
	if seed.s == 0 {
		panic("maphash: use of uninitialized Seed")
	}
	h := new(Hash)
	h.SetSeed(seed)
	h.Write(b)
	return h.Sum64()
}

// String returns the hash of s with the given seed.
//
// String is equivalent to, but more convenient and efficient than:
//
//	var h Hash
//	h.SetSeed(seed)
//	h.WriteString(s)
//	return h.Sum64()
func String(seed Seed, s string) uint64 {
	if seed.s == 0 {
		panic("maphash: use of uninitialized Seed")
	}
	h := new(Hash)
	h.SetSeed(seed)
	h.WriteString(s)
	return h.Sum64()
}

// Hasher is the interface between a hash function and a hash table.
type Hasher[T any] interface {
	// Hash hashes v, writing the hash to h.
	Hash(h *Hash, v T)
	// Equal reports whether x and y are equal.
	Equal(x, y T) bool
}

// ComparableHasher is a Hasher for comparable types.
type ComparableHasher[T comparable] struct{}

// Equal reports whether x and y are equal.
func (ComparableHasher[T]) Equal(x, y T) bool { return x == y }

// Hash hashes v, writing the hash to h.
func (ComparableHasher[T]) Hash(h *Hash, v T) { WriteComparable(h, v) }

// Comparable returns the hash of v with the given seed.
func Comparable[T comparable](seed Seed, v T) uint64 {
	if seed.s == 0 {
		panic("maphash: use of uninitialized Seed")
	}
	var h = new(Hash)
	h.SetSeed(seed)
	WriteComparable(h, v)
	return h.Sum64()
}

// writeUint64 writes v in little-endian order, prefixed by a kind tag so
// distinct basic kinds do not share encodings.
func writeTagged(h *Hash, tag byte, v uint64) {
	h.WriteByte(tag)
	var b [8]byte
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
	h.Write(b[:])
}

// WriteComparable hashes x, writing the hash to h. Basic kinds
// (bool, all int/uint widths, uintptr, floats, complex, string) are
// supported; any other comparable kind fails fast: hashing it would
// need the runtime typehash, which has no synthetic equivalent.
func WriteComparable[T comparable](h *Hash, x T) {
	switch v := any(x).(type) {
	case bool:
		if v {
			writeTagged(h, 'b', 1)
		} else {
			writeTagged(h, 'b', 0)
		}
	case int:
		writeTagged(h, 'i', uint64(v))
	case int8:
		writeTagged(h, '1', uint64(int64(v)))
	case int16:
		writeTagged(h, '2', uint64(int64(v)))
	case int32:
		writeTagged(h, '4', uint64(int64(v)))
	case int64:
		writeTagged(h, '8', uint64(v))
	case uint:
		writeTagged(h, 'u', uint64(v))
	case uint8:
		writeTagged(h, 'c', uint64(v))
	case uint16:
		writeTagged(h, 's', uint64(v))
	case uint32:
		writeTagged(h, 'l', uint64(v))
	case uint64:
		writeTagged(h, 'q', uint64(v))
	case uintptr:
		writeTagged(h, 'p', uint64(v))
	case float32:
		writeTagged(h, 'f', uint64(math.Float32bits(v)))
	case float64:
		writeTagged(h, 'd', math.Float64bits(v))
	case complex64:
		writeTagged(h, 'g', uint64(math.Float32bits(real(v))))
		writeTagged(h, 'G', uint64(math.Float32bits(imag(v))))
	case complex128:
		writeTagged(h, 'h', math.Float64bits(real(v)))
		writeTagged(h, 'H', math.Float64bits(imag(v)))
	case string:
		h.WriteByte('t')
		h.WriteString(v)
	default:
		panic("maphash: WriteComparable: unsupported type (needs runtime typehash)")
	}
}
`
