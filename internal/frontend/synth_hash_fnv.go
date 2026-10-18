// SYNTHETIC (Principle 4) — hash/fnv: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 (wave 3): synthetic `hash/fnv` package.
// Pure-Go port of the stdlib FNV-1/FNV-1a (32, 64 and 128 bit). The
// 128-bit multiply uses a hand-rolled 64x64->128 multiply (the synthetic
// tree does not register math/bits as an importable package); the
// expansion is the standard 32-bit-limb decomposition and computes the
// same value as math/bits.Mul64. The marshal state formats (magic
// "fnv\x01".."fnv\x06" + big-endian state) are ported with hand-rolled
// big-endian codecs.
// KAT: differential vs gc on the wave-3 hash/fnv corpus (FNV test
// vectors for all six constructors, chunking invariance, marshal
// round-trips, error strings).
const syntheticFnvSource = `package fnv

import (
	"errors"
	"hash"
)

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fnv implements FNV-1 and FNV-1a, non-cryptographic hash functions
// created by Glenn Fowler, Landon Curt Noll, and Phong Vo.
// See
// https://en.wikipedia.org/wiki/Fowler-Noll-Vo_hash_function.
//
// All the hash.Hash implementations returned by this package also
// implement encoding.BinaryMarshaler and encoding.BinaryUnmarshaler to
// marshal and unmarshal the internal state of the hash.
//
// P3-M9: pure-Go software port (no intrinsification).

// The sums are struct wrappers (not bare uint32/uint64/[2]uint64):
// the frontend forbids assignment to *ast.StarExpr and &ident in
// synthetic bodies (WS D), so all state mutation goes through
// selectors. The M8 crypto digests use the same pattern.
type (
	sum32   struct{ v uint32 }
	sum32a  struct{ v uint32 }
	sum64   struct{ v uint64 }
	sum64a  struct{ v uint64 }
	sum128  struct{ hi, lo uint64 }
	sum128a struct{ hi, lo uint64 }
)

const (
	offset32        = 2166136261
	offset64        = 14695981039346656037
	offset128Lower  = 0x62b821756295c58d
	offset128Higher = 0x6c62272e07bb0142
	prime32         = 16777619
	prime64         = 1099511628211
	prime128Lower   = 0x13b
	prime128Shift   = 24
)

// New32 returns a new 32-bit FNV-1 [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New32() hash.Hash32 {
	return &sum32{v: offset32}
}

// New32a returns a new 32-bit FNV-1a [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New32a() hash.Hash32 {
	return &sum32a{v: offset32}
}

// New64 returns a new 64-bit FNV-1 [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New64() hash.Hash64 {
	return &sum64{v: offset64}
}

// New64a returns a new 64-bit FNV-1a [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New64a() hash.Hash64 {
	return &sum64a{v: offset64}
}

// New128 returns a new 128-bit FNV-1 [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New128() hash.Hash {
	return &sum128{hi: offset128Higher, lo: offset128Lower}
}

// New128a returns a new 128-bit FNV-1a [hash.Hash].
// Its Sum method will lay the value out in big-endian byte order.
func New128a() hash.Hash {
	return &sum128a{hi: offset128Higher, lo: offset128Lower}
}

func (s *sum32) Reset()   { s.v = offset32 }
func (s *sum32a) Reset()  { s.v = offset32 }
func (s *sum64) Reset()   { s.v = offset64 }
func (s *sum64a) Reset()  { s.v = offset64 }
func (s *sum128) Reset()  { s.hi = offset128Higher; s.lo = offset128Lower }
func (s *sum128a) Reset() { s.hi = offset128Higher; s.lo = offset128Lower }

func (s *sum32) Sum32() uint32  { return s.v }
func (s *sum32a) Sum32() uint32 { return s.v }
func (s *sum64) Sum64() uint64  { return s.v }
func (s *sum64a) Sum64() uint64 { return s.v }

// WriteSingle is the single-result form of Write (the v1 frontend cannot
// emit or call multi-result functions (WS D); the real logic lives here,
// served via the mrM8 desugar to hash.__hashWrite. Re-test trigger:
// WS D lands.
func (s *sum32) WriteSingle(data []byte) int {
	hash := s.v
	for _, c := range data {
		hash *= prime32
		hash ^= uint32(c)
	}
	s.v = hash
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; real logic is WriteSingle,
// served via the mrM8 desugar to hash.__hashWrite). Re-test trigger:
// WS D lands.
func (s *sum32) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// P3-M9: package-local multi-result helpers (see hash/adler32). The v1
// frontend cannot emit or call multi-result functions (WS D), so Write/
// MarshalBinary/AppendBinary are served through the mrM8 desugar. These
// helpers take any (not hash.Hash) to avoid an interface conversion that
// would lose the dynamic type's method set. One helper per method switches
// on the six sum types.
// Re-test trigger: WS D lands.
type writePair struct {
	n   int
	err error
}

type bytesPair struct {
	b   []byte
	err error
}

// P3-M9: concrete-type helpers (the v1 itab dispatcher passes the raw
// dynamic value; interface{} + type assertion fails at runtime, so each
// sum type gets its own helper). Re-test trigger: WS D lands.
func __fnvWrite32(s *sum32, p []byte) writePair     { return writePair{s.WriteSingle(p), nil} }
func __fnvWrite32a(s *sum32a, p []byte) writePair   { return writePair{s.WriteSingle(p), nil} }
func __fnvWrite64(s *sum64, p []byte) writePair     { return writePair{s.WriteSingle(p), nil} }
func __fnvWrite64a(s *sum64a, p []byte) writePair   { return writePair{s.WriteSingle(p), nil} }
func __fnvWrite128(s *sum128, p []byte) writePair   { return writePair{s.WriteSingle(p), nil} }
func __fnvWrite128a(s *sum128a, p []byte) writePair { return writePair{s.WriteSingle(p), nil} }

func __fnvMarshal32(s *sum32) bytesPair     { return bytesPair{s.MarshalSingle(), nil} }
func __fnvMarshal32a(s *sum32a) bytesPair   { return bytesPair{s.MarshalSingle(), nil} }
func __fnvMarshal64(s *sum64) bytesPair     { return bytesPair{s.MarshalSingle(), nil} }
func __fnvMarshal64a(s *sum64a) bytesPair   { return bytesPair{s.MarshalSingle(), nil} }
func __fnvMarshal128(s *sum128) bytesPair   { return bytesPair{s.MarshalSingle(), nil} }
func __fnvMarshal128a(s *sum128a) bytesPair { return bytesPair{s.MarshalSingle(), nil} }

func __fnvAppend32(s *sum32, b []byte) bytesPair     { return bytesPair{s.AppendSingle(b), nil} }
func __fnvAppend32a(s *sum32a, b []byte) bytesPair   { return bytesPair{s.AppendSingle(b), nil} }
func __fnvAppend64(s *sum64, b []byte) bytesPair     { return bytesPair{s.AppendSingle(b), nil} }
func __fnvAppend64a(s *sum64a, b []byte) bytesPair   { return bytesPair{s.AppendSingle(b), nil} }
func __fnvAppend128(s *sum128, b []byte) bytesPair   { return bytesPair{s.AppendSingle(b), nil} }
func __fnvAppend128a(s *sum128a, b []byte) bytesPair { return bytesPair{s.AppendSingle(b), nil} }

// WriteSingle is the single-result form of Write (see sum32.WriteSingle).
// Re-test trigger: WS D lands.
func (s *sum32a) WriteSingle(data []byte) int {
	hash := s.v
	for _, c := range data {
		hash ^= uint32(c)
		hash *= prime32
	}
	s.v = hash
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; see sum32.Write).
// Re-test trigger: WS D lands.
func (s *sum32a) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// WriteSingle is the single-result form of Write (see sum32.WriteSingle).
// Re-test trigger: WS D lands.
func (s *sum64) WriteSingle(data []byte) int {
	hash := s.v
	for _, c := range data {
		hash *= prime64
		hash ^= uint64(c)
	}
	s.v = hash
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; see sum32.Write).
// Re-test trigger: WS D lands.
func (s *sum64) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// WriteSingle is the single-result form of Write (see sum32.WriteSingle).
// Re-test trigger: WS D lands.
func (s *sum64a) WriteSingle(data []byte) int {
	hash := s.v
	for _, c := range data {
		hash ^= uint64(c)
		hash *= prime64
	}
	s.v = hash
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; see sum32.Write).
// Re-test trigger: WS D lands.
func (s *sum64a) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// mul64 returns the 128-bit product of x and y as [hi, lo]. It is the
// standard 32-bit-limb decomposition, computing the same value as
// math/bits.Mul64 (math/bits is not an importable synthetic package).
// Single-result ([2]uint64) because the v1 frontend cannot emit or call
// multi-result functions (WS D). Re-test trigger: WS D lands.
func mul64(x, y uint64) [2]uint64 {
	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + (w0 >> 32)
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	hi := x1*y1 + w2 + (w1 >> 32)
	lo := x * y
	return [2]uint64{hi, lo}
}

// WriteSingle is the single-result form of Write (see sum32.WriteSingle).
// Re-test trigger: WS D lands.
func (s *sum128) WriteSingle(data []byte) int {
	for _, c := range data {
		// Compute the multiplication
		p := mul64(prime128Lower, s.lo)
		s0 := p[0] + s.lo<<prime128Shift + prime128Lower*s.hi
		// Update the values
		s.lo = p[1]
		s.hi = s0
		s.lo ^= uint64(c)
	}
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; see sum32.Write).
// Re-test trigger: WS D lands.
func (s *sum128) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// WriteSingle is the single-result form of Write (see sum32.WriteSingle).
// Re-test trigger: WS D lands.
func (s *sum128a) WriteSingle(data []byte) int {
	for _, c := range data {
		s.lo ^= uint64(c)
		// Compute the multiplication
		p := mul64(prime128Lower, s.lo)
		s0 := p[0] + s.lo<<prime128Shift + prime128Lower*s.hi
		// Update the values
		s.lo = p[1]
		s.hi = s0
	}
	return len(data)
}

// Write implements hash.Hash (fail-fast stub; see sum32.Write).
// Re-test trigger: WS D lands.
func (s *sum128a) Write(data []byte) (int, error) {
	panic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (s *sum32) Size() int   { return 4 }
func (s *sum32a) Size() int  { return 4 }
func (s *sum64) Size() int   { return 8 }
func (s *sum64a) Size() int  { return 8 }
func (s *sum128) Size() int  { return 16 }
func (s *sum128a) Size() int { return 16 }

func (s *sum32) BlockSize() int   { return 1 }
func (s *sum32a) BlockSize() int  { return 1 }
func (s *sum64) BlockSize() int   { return 1 }
func (s *sum64a) BlockSize() int  { return 1 }
func (s *sum128) BlockSize() int  { return 1 }
func (s *sum128a) BlockSize() int { return 1 }

// beAppendUint32/beAppendUint64 append in big-endian byte order
// (hand-rolled: the synthetic tree has no internal/byteorder).
func beAppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func beAppendUint64(b []byte, v uint64) []byte {
	return append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func beUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func beUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

func (s *sum32) Sum(in []byte) []byte {
	return beAppendUint32(in, s.v)
}

func (s *sum32a) Sum(in []byte) []byte {
	return beAppendUint32(in, s.v)
}

func (s *sum64) Sum(in []byte) []byte {
	return beAppendUint64(in, s.v)
}

func (s *sum64a) Sum(in []byte) []byte {
	return beAppendUint64(in, s.v)
}

func (s *sum128) Sum(in []byte) []byte {
	ret := beAppendUint64(in, s.hi)
	return beAppendUint64(ret, s.lo)
}

func (s *sum128a) Sum(in []byte) []byte {
	ret := beAppendUint64(in, s.hi)
	return beAppendUint64(ret, s.lo)
}

const (
	magic32          = "fnv\x01"
	magic32a         = "fnv\x02"
	magic64          = "fnv\x03"
	magic64a         = "fnv\x04"
	magic128         = "fnv\x05"
	magic128a        = "fnv\x06"
	marshaledSize32  = len(magic32) + 4
	marshaledSize64  = len(magic64) + 8
	marshaledSize128 = len(magic128) + 8*2
)

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum32) AppendSingle(b []byte) []byte {
	b = append(b, magic32...)
	b = beAppendUint32(b, s.v)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum32) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize32))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum32) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum32) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum32a) AppendSingle(b []byte) []byte {
	b = append(b, magic32a...)
	b = beAppendUint32(b, s.v)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum32a) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize32))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum32a) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum32a) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum64) AppendSingle(b []byte) []byte {
	b = append(b, magic64...)
	b = beAppendUint64(b, s.v)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum64) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize64))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum64) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum64) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum64a) AppendSingle(b []byte) []byte {
	b = append(b, magic64a...)
	b = beAppendUint64(b, s.v)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum64a) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize64))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum64a) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum64a) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum128) AppendSingle(b []byte) []byte {
	b = append(b, magic128...)
	b = beAppendUint64(b, s.hi)
	b = beAppendUint64(b, s.lo)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum128) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize128))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum128) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum128) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (s *sum128a) AppendSingle(b []byte) []byte {
	b = append(b, magic128a...)
	b = beAppendUint64(b, s.hi)
	b = beAppendUint64(b, s.lo)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (s *sum128a) MarshalSingle() []byte {
	return s.AppendSingle(make([]byte, 0, marshaledSize128))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (s *sum128a) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/fnv: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (s *sum128a) MarshalBinary() ([]byte, error) {
	panic("hash/fnv: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (s *sum32) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic32) || string(b[:len(magic32)]) != magic32 {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize32 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.v = beUint32(b[4:])
	return nil
}

func (s *sum32a) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic32a) || string(b[:len(magic32a)]) != magic32a {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize32 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.v = beUint32(b[4:])
	return nil
}

func (s *sum64) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic64) || string(b[:len(magic64)]) != magic64 {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize64 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.v = beUint64(b[4:])
	return nil
}

func (s *sum64a) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic64a) || string(b[:len(magic64a)]) != magic64a {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize64 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.v = beUint64(b[4:])
	return nil
}

func (s *sum128) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic128) || string(b[:len(magic128)]) != magic128 {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize128 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.hi = beUint64(b[4:])
	s.lo = beUint64(b[12:])
	return nil
}

func (s *sum128a) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic128a) || string(b[:len(magic128a)]) != magic128a {
		return errors.New("hash/fnv: invalid hash state identifier")
	}
	if len(b) != marshaledSize128 {
		return errors.New("hash/fnv: invalid hash state size")
	}
	s.hi = beUint64(b[4:])
	s.lo = beUint64(b[12:])
	return nil
}
`
