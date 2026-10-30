// Retirement: see ../../docs/arch/synth-retirement-tracker.md#hashcrc64 for this package's root cause and retires-when trigger (hash/crc64).
package main

// P3-M9 (wave 3): synthetic `hash/crc64` package.
// Pure-Go port of the stdlib CRC-64. The stdlib's slicing-by-8 fast path
// is NOT ported: the simple byte-at-a-time table walk computes
// bit-identical values for every polynomial. MakeTable caches the ISO
// and ECMA tables (returning the same *Table on repeat calls, like the
// stdlib). The marshal state format (magic "crc\x02" + BE tableSum + BE
// crc) is ported with hand-rolled big-endian codecs.
// KAT: differential vs gc on the wave-3 hash/crc64 corpus (ISO, ECMA and
// custom-polynomial vectors, marshal round-trips, error strings).
const syntheticCrc64Source = `package crc64

import (
	"errors"
	"hash"
)

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package crc64 implements the 64-bit cyclic redundancy check, or CRC-64,
// checksum. See https://en.wikipedia.org/wiki/Cyclic_redundancy_check for
// information.
//
// P3-M9: pure-Go software port (no intrinsification, no slicing-by-8
// fast path; values are gc-identical).

// The size of a CRC-64 checksum in bytes.
const Size = 8

// Predefined polynomials.
const (
	// The ISO polynomial, defined in ISO 3309 and used in HDLC.
	ISO = 0xD800000000000000

	// The ECMA polynomial, defined in ECMA 182.
	ECMA = 0xC96C5795D7870F42
)

// Table is a 256-word table representing the polynomial for efficient processing.
type Table [256]uint64
// P3-M9: *Table values are opaque tokens (the frontend forbids index
// assignment on a pointer, WS E, slicing a pointer, WS E, pointer
// composite literals to non-struct, WS E, and & on identifiers, WS D,
// so tables cannot be built dynamically). Each token maps to a polynomial
// via polyOf; the CRC is computed bit-by-bit from the polynomial (value-
// identical to the table walk). Retest trigger: WS E/D land.
var isoTablePre = new(Table)
var ecmaTablePre = new(Table)

// customPolys maps tokens returned by MakeTable for non-standard
// polynomials to their polynomial.
var customPolys = map[*Table]uint64{}

// polyOf returns the polynomial for a *Table token.
func polyOf(tab *Table) uint64 {
	if tab == nil || tab == isoTablePre {
		return ISO
	}
	if tab == ecmaTablePre {
		return ECMA
	}
	if p, ok := customPolys[tab]; ok {
		return p
	}
	panic("hash/crc64: unknown Table token")
}

// returning the same *Table).
var isoTable = isoTablePre
var ecmaTable = ecmaTablePre

// MakeTable returns a [Table] constructed from the specified polynomial.
// The contents of this [Table] must not be modified.
func MakeTable(poly uint64) *Table {
	switch poly {
	case ISO:
		return isoTable
	case ECMA:
		return ecmaTable
	default:
		t := new(Table)
		customPolys[t] = poly
		return t
	}
}

// digest represents the partial evaluation of a checksum.
type digest struct {
	crc  uint64
	poly uint64
}

// New creates a new hash.Hash64 computing the CRC-64 checksum using the
// polynomial represented by the [Table]. Its Sum method will lay the
// value out in big-endian byte order. The returned Hash64 also
// implements [encoding.BinaryMarshaler] and [encoding.BinaryUnmarshaler] to
// marshal and unmarshal the internal state of the hash.
func New(tab *Table) hash.Hash64 { return &digest{crc: 0, poly: polyOf(tab)} }

func (d *digest) Size() int { return Size }

func (d *digest) BlockSize() int { return 1 }

func (d *digest) Reset() { d.crc = 0 }

const (
	magic         = "crc\x02"
	marshaledSize = len(magic) + 8 + 8
)

// beAppendUint64 appends v in big-endian byte order (hand-rolled:
// the synthetic tree has no internal/byteorder).
func beAppendUint64(b []byte, v uint64) []byte {
	return append(b, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func beUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

// AppendSingle is the single-result form of AppendBinary (the v1
// frontend cannot emit or call multi-result functions (WS D); the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
// Re-test trigger: WS D lands.
func (d *digest) AppendSingle(b []byte) []byte {
	b = append(b, magic...)
	b = beAppendUint64(b, tableSum(d.poly))
	b = beAppendUint64(b, d.crc)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
// Re-test trigger: WS D lands.
func (d *digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend). Re-test trigger: WS D lands.
func (d *digest) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/crc64: AppendBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal). Re-test trigger: WS D lands.
func (d *digest) MarshalBinary() ([]byte, error) {
	panic("hash/crc64: MarshalBinary: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic) || string(b[:len(magic)]) != magic {
		return errors.New("hash/crc64: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("hash/crc64: invalid hash state size")
	}
	if tableSum(d.poly) != beUint64(b[4:]) {
		return errors.New("hash/crc64: tables do not match")
	}
	d.crc = beUint64(b[12:])
	return nil
}

// CloneSinglePair is the single-result form of Clone (the v1 frontend
// cannot emit or call multi-result functions (WS D); the real logic
// lives here, served via the mrM8 desugar to hash.__hashClone).
// Re-test trigger: WS D lands.
func (d *digest) CloneSinglePair() hash.ClonePair {
	// P3-M9: composite literal (the frontend forbids &ident in
	// synthetic bodies, WS D).
	return hash.ClonePair{C: &digest{crc: d.crc, poly: d.poly}, Err: nil}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is
// CloneSinglePair, served via the mrM8 desugar to hash.__hashClone).
// Re-test trigger: WS D lands.
func (d *digest) Clone() (hash.Cloner, error) {
	panic("hash/crc64: Clone: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

// update is the textbook byte-at-a-time reflected CRC-64 update; the
// stdlib's slicing-by-8 fast path computes the same value.
// updateBitByBit computes the CRC-64 of p starting from crc, using the
// given polynomial, without a lookup table (value-identical to the table
// walk; the frontend cannot build tables dynamically).
func updateBitByBit(crc uint64, poly uint64, p []byte) uint64 {
	crc = ^crc
	for _, v := range p {
		crc ^= uint64(v)
		for i := 0; i < 8; i++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ poly
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func update(crc uint64, tab *Table, p []byte) uint64 {
	return updateBitByBit(crc, polyOf(tab), p)
}

// Update returns the result of adding the bytes in p to the crc.
func Update(crc uint64, tab *Table, p []byte) uint64 {
	return update(crc, tab, p)
}

// P3-M9: package-local multi-result helpers (see hash/adler32). The v1
// frontend cannot emit or call multi-result functions (WS D), so
// Write/Clone/MarshalBinary/AppendBinary are served through the mrM8
// desugar. These helpers take any (not hash.Hash) to avoid an interface
// conversion that would lose the dynamic type's method set.
// Re-test trigger: WS D lands.
type writePair struct {
	n   int
	err error
}

type clonePair struct {
	c   hash.Cloner
	err error
}

type bytesPair struct {
	b   []byte
	err error
}

func __crc64Write(d *digest, p []byte) writePair {
	return writePair{d.WriteSingle(p), nil}
}

func __crc64Clone(d *digest) clonePair {
	cp := d.CloneSinglePair()
	return clonePair{cp.C, cp.Err}
}

func __crc64Marshal(d *digest) bytesPair {
	return bytesPair{d.MarshalSingle(), nil}
}

func __crc64Append(d *digest, b []byte) bytesPair {
	return bytesPair{d.AppendSingle(b), nil}
}

// WriteSingle is the single-result form of Write (the v1 frontend cannot
// emit or call multi-result functions (WS D); the real logic lives here,
// served via the mrM8 desugar to hash.__hashWrite, which delegates to
// this method through the hash.singleWriter interface. Re-test trigger:
// WS D lands.
func (d *digest) WriteSingle(p []byte) int {
	d.crc = updateBitByBit(d.crc, d.poly, p)
	return len(p)
}

// Write implements hash.Hash (fail-fast stub; real logic is WriteSingle,
// served via the mrM8 desugar to hash.__hashWrite). Re-test trigger:
// WS D lands.
func (d *digest) Write(p []byte) (n int, err error) {
	panic("hash/crc64: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
}

func (d *digest) Sum64() uint64 { return d.crc }

func (d *digest) Sum(in []byte) []byte {
	s := d.Sum64()
	return append(in, byte(s>>56), byte(s>>48), byte(s>>40), byte(s>>32),
		byte(s>>24), byte(s>>16), byte(s>>8), byte(s))
}

// Checksum returns the CRC-64 checksum of data
// using the polynomial represented by the [Table].
func Checksum(data []byte, tab *Table) uint64 { return update(0, tab, data) }

// tableSum returns the ISO checksum of table t.
// tableSum returns the ISO checksum of the table for the given polynomial.
// (The table is generated entry-by-entry from the poly; no *Table needed.)
func tableSum(poly uint64) uint64 {
	var a [2048]byte
	b := a[:0]
	for i := 0; i < 256; i++ {
		crc := uint64(i)
		for j := 0; j < 8; j++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ poly
			} else {
				crc >>= 1
			}
		}
		b = beAppendUint64(b, crc)
	}
	return Checksum(b, MakeTable(ISO))
}
`
