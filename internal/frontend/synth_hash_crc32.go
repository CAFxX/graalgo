package main

// P3-M9 (wave 3): synthetic `hash/crc32` package.
// Pure-Go port of the stdlib CRC-32. The stdlib's arch-specific and
// slicing-by-8 fast paths are NOT ported: every update goes through the
// simple byte-at-a-time table walk, which computes bit-identical values
// for every polynomial (all fast paths are value-preserving
// optimizations). IEEETable is a package-level var built by
// simpleMakeTable(IEEE), exactly like the stdlib, so MakeTable(IEEE)
// returns the same *Table as the exported IEEETable; the Castagnoli
// table is cached the same way. The marshal state format (magic
// "crc\x01" + BE tableSum + BE crc) is ported with hand-rolled
// big-endian codecs.
// KAT: differential vs gc on the wave-3 hash/crc32 corpus (IEEE,
// Castagnoli, Koopman and custom-polynomial vectors, marshal
// round-trips, error strings).
const syntheticCrc32Source = `package crc32

import (
	"errors"
	"hash"
)

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package crc32 implements the 32-bit cyclic redundancy check, or CRC-32,
// checksum. See https://en.wikipedia.org/wiki/Cyclic_redundancy_check for
// information.
//
// Polynomials are represented in LSB-first form also known as reversed representation.
//
// See https://en.wikipedia.org/wiki/Mathematics_of_cyclic_redundancy_checks#Reversed_representations_and_reciprocal_polynomials
// for information.
//
// P3-M9: pure-Go software port (no intrinsification, no arch-specific or
// slicing-by-8 fast paths; values are gc-identical).

// The size of a CRC-32 checksum in bytes.
const Size = 4

// Predefined polynomials.
const (
	// IEEE is by far and away the most common CRC-32 polynomial.
	// Used by ethernet (IEEE 802.3), v.42, fddi, gzip, zip, png, ...
	IEEE = 0xedb88320

	// Castagnoli's polynomial, used in iSCSI.
	// Has better error detection characteristics than IEEE.
	// https://dx.doi.org/10.1109/26.231911
	Castagnoli = 0x82f63b78

	// Koopman's polynomial.
	// Also has better error detection characteristics than IEEE.
	// https://dx.doi.org/10.1109/DSN.2002.1028931
	Koopman = 0xeb31d82e
)

// Table is a 256-word table representing the polynomial for efficient processing.
type Table [256]uint32

// P3-M9: *Table values are opaque tokens (the frontend forbids index
// assignment on a pointer, WS E, slicing a pointer, WS E, pointer
// composite literals to non-struct, WS E, and & on identifiers,
// so tables cannot be built dynamically). Each token maps to a polynomial
// via polyOf; the CRC is computed bit-by-bit from the polynomial (value-
// identical to the table walk). Retest trigger: WS E/D land.
var IEEETable = new(Table)
var castagnoliTable = new(Table)
var koopmanTable = new(Table)

// customPolys maps tokens returned by MakeTable for non-standard
// polynomials to their polynomial.
var customPolys = map[*Table]uint64{}

// polyOf returns the polynomial for a *Table token.
func polyOf(tab *Table) uint64 {
	if tab == nil || tab == IEEETable {
		return IEEE
	}
	if tab == castagnoliTable {
		return Castagnoli
	}
	if tab == koopmanTable {
		return Koopman
	}
	if p, ok := customPolys[tab]; ok {
		return p
	}
	panic("hash/crc32: unknown Table token")
}

// IEEETable is the table for the [IEEE] polynomial.
// (declared above as an opaque token)

// MakeTable returns a [Table] constructed from the specified polynomial.
// The contents of this [Table] must not be modified.
func MakeTable(poly uint64) *Table {
	switch poly {
	case IEEE:
		return IEEETable
	case Castagnoli:
		return castagnoliTable
	case Koopman:
		return koopmanTable
	default:
		t := new(Table)
		customPolys[t] = poly
		return t
	}
}

// digest represents the partial evaluation of a checksum.
type digest struct {
	crc  uint32
	poly uint64
}

// New creates a new [hash.Hash32] computing the CRC-32 checksum using the
// polynomial represented by the [Table]. Its Sum method will lay the
// value out in big-endian byte order. The returned Hash32 also
// implements [encoding.BinaryMarshaler] and [encoding.BinaryUnmarshaler] to
// marshal and unmarshal the internal state of the hash.
func New(tab *Table) hash.Hash32 {
	return &digest{crc: 0, poly: polyOf(tab)}
}

// NewIEEE creates a new [hash.Hash32] computing the CRC-32 checksum using
// the [IEEE] polynomial. Its Sum method will lay the value out in
// big-endian byte order. The returned Hash32 also implements
// [encoding.BinaryMarshaler] and [encoding.BinaryUnmarshaler] to marshal
// and unmarshal the internal state of the hash.
func NewIEEE() hash.Hash32 { return New(IEEETable) }

func (d *digest) Size() int { return Size }

func (d *digest) BlockSize() int { return 1 }

func (d *digest) Reset() { d.crc = 0 }

const (
	magic         = "crc\x01"
	marshaledSize = len(magic) + 4 + 4
)

// beAppendUint32 appends v in big-endian byte order (hand-rolled:
// the synthetic tree has no internal/byteorder).
func beAppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func beUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// AppendSingle is the single-result form of AppendBinary (the v1
// the real
// logic lives here, served via the mrM8 desugar to hash.__binaryAppend.
func (d *digest) AppendSingle(b []byte) []byte {
	b = append(b, magic...)
	b = beAppendUint32(b, tableSum(d.poly))
	b = beAppendUint32(b, d.crc)
	return b
}

// MarshalSingle is the single-result form of MarshalBinary (see
// AppendSingle). Served via the mrM8 desugar to hash.__binaryMarshal.
func (d *digest) MarshalSingle() []byte {
	return d.AppendSingle(make([]byte, 0, marshaledSize))
}

// AppendBinary implements encoding.BinaryAppender (fail-fast stub; real
// logic is AppendSingle, served via the mrM8 desugar to
// hash.__binaryAppend).
func (d *digest) AppendBinary(b []byte) ([]byte, error) {
	panic("hash/crc32: AppendBinary: unreachable (call sites desugar to __crc32Append")
}

// MarshalBinary implements encoding.BinaryMarshaler (fail-fast stub;
// real logic is MarshalSingle, served via the mrM8 desugar to
// hash.__binaryMarshal).
func (d *digest) MarshalBinary() ([]byte, error) {
	panic("hash/crc32: MarshalBinary: unreachable (call sites desugar to __crc32Marshal")
}

func (d *digest) UnmarshalBinary(b []byte) error {
	if len(b) < len(magic) || string(b[:len(magic)]) != magic {
		return errors.New("hash/crc32: invalid hash state identifier")
	}
	if len(b) != marshaledSize {
		return errors.New("hash/crc32: invalid hash state size")
	}
	if tableSum(d.poly) != beUint32(b[4:]) {
		return errors.New("hash/crc32: tables do not match")
	}
	d.crc = beUint32(b[8:])
	return nil
}

// CloneSinglePair is the single-result form of Clone (the v1 frontend
// cannot emit or call multi-result functions ; the real logic
// lives here, served via the mrM8 desugar to hash.__hashClone).
func (d *digest) CloneSinglePair() hash.ClonePair {
	// P3-M9: composite literal (the frontend forbids &ident in
	// synthetic bodies).
	return hash.ClonePair{C: &digest{crc: d.crc, poly: d.poly}, Err: nil}
}

// Clone implements hash.Cloner (fail-fast stub; real logic is
// CloneSinglePair, served via the mrM8 desugar to hash.__hashClone).
func (d *digest) Clone() (hash.Cloner, error) {
	panic("hash/crc32: Clone: unreachable (call sites desugar to __crc32Clone")
}

// simpleUpdate is the textbook byte-at-a-time reflected CRC update. All
// of the stdlib's fast paths (slicing-by-8, arch-specific) compute the
// same value; the software port uses only this one.
// updateBitByBit computes the CRC-32 of p starting from crc, using the
// given polynomial, without a lookup table (value-identical to the table
// walk; the frontend cannot build tables dynamically).
func updateBitByBit(crc uint32, poly uint64, p []byte) uint32 {
	crc = ^crc
	for _, v := range p {
		crc ^= uint32(v)
		for i := 0; i < 8; i++ {
			if crc&1 == 1 {
				crc = uint32((uint64(crc) >> 1) ^ poly)
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func simpleUpdate(crc uint32, tab *Table, p []byte) uint32 {
	return updateBitByBit(crc, polyOf(tab), p)
}

// Update returns the result of adding the bytes in p to the crc.
func Update(crc uint32, tab *Table, p []byte) uint32 {
	return simpleUpdate(crc, tab, p)
}

// P3-M9: package-local multi-result helpers (see hash/adler32). The v1
// frontend cannot emit or call multi-result functions , so
// Write/Clone/MarshalBinary/AppendBinary are served through the mrM8
// desugar. These helpers take any (not hash.Hash) to avoid an interface
// conversion that would lose the dynamic type's method set.
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

func __crc32Write(d *digest, p []byte) writePair {
	return writePair{d.WriteSingle(p), nil}
}

func __crc32Clone(d *digest) clonePair {
	cp := d.CloneSinglePair()
	return clonePair{cp.C, cp.Err}
}

func __crc32Marshal(d *digest) bytesPair {
	return bytesPair{d.MarshalSingle(), nil}
}

func __crc32Append(d *digest, b []byte) bytesPair {
	return bytesPair{d.AppendSingle(b), nil}
}

// WriteSingle is the single-result form of Write (the real logic lives here,
// served via the mrM8 desugar to hash.__hashWrite, which delegates to
// this method through the hash.singleWriter interface. Re-test trigger:
func (d *digest) WriteSingle(p []byte) int {
	d.crc = updateBitByBit(d.crc, d.poly, p)
	return len(p)
}

// Write implements hash.Hash (fail-fast stub; real logic is WriteSingle,
// served via the mrM8 desugar to hash.__hashWrite). Re-test trigger:
func (d *digest) Write(p []byte) (n int, err error) {
	panic("hash/crc32: Write: unreachable (call sites desugar to __crc32Write")
}

func (d *digest) Sum32() uint32 { return d.crc }

func (d *digest) Sum(in []byte) []byte {
	s := d.Sum32()
	return append(in, byte(s>>24), byte(s>>16), byte(s>>8), byte(s))
}

// Checksum returns the CRC-32 checksum of data
// using the polynomial represented by the [Table].
func Checksum(data []byte, tab *Table) uint32 { return Update(0, tab, data) }

// ChecksumIEEE returns the CRC-32 checksum of data
// using the [IEEE] polynomial.
func ChecksumIEEE(data []byte) uint32 {
	return simpleUpdate(0, IEEETable, data)
}

// tableSum returns the IEEE checksum of table t.
// tableSum returns the IEEE checksum of the table for the given polynomial.
// (The table is generated entry-by-entry from the poly; no *Table needed.)
func tableSum(poly uint64) uint32 {
	var a [1024]byte
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
		b = beAppendUint32(b, uint32(crc))
	}
	return ChecksumIEEE(b)
}
`
