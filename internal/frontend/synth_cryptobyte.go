// Retirement: see ../../docs/arch/synth-retirement-tracker.md#golangorgxcryptocryptobyte for this package's root cause and retires-when trigger (golang.org/x/crypto/cryptobyte).
package main

// P3-M9 tier-5: synthetic 'golang.org/x/crypto/cryptobyte' package.
const syntheticCryptobyteSource = `// P3-M9 tier-5: synthetic .
// Vendored from x/crypto v0.41.0 (BSD-3-Clause). Pure Go.
// Synthetic-only.
package cryptobyte

import (
	encoding_asn1 "encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"time"

	"golang.org/x/crypto/cryptobyte/asn1"
)
// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cryptobyte contains types that help with parsing and constructing
// length-prefixed, binary messages, including ASN.1 DER. (The asn1 subpackage
// contains useful ASN.1 constants.)
//
// The String type is for parsing. It wraps a []byte slice and provides helper
// functions for consuming structures, value by value.
//
// The Builder type is for constructing messages. It providers helper functions
// for appending values and also for appending length-prefixed submessages –
// without having to worry about calculating the length prefix ahead of time.
//
// See the documentation and examples for the Builder and String types to get
// started.

// String represents a string of bytes. It provides methods for parsing
// fixed-length and length-prefixed values from it.
type String []byte

// read advances a String by n bytes and returns them. If less than n bytes
// remain, it returns nil.
func (s *String) read(n int) []byte {
	if len(*s) < n || n < 0 {
		return nil
	}
	v := (*s)[:n]
	*s = (*s)[n:]
	return v
}

// Skip advances the String by n byte and reports whether it was successful.
func (s *String) Skip(n int) bool {
	return s.read(n) != nil
}

// ReadUint8 decodes an 8-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint8(out *uint8) bool {
	v := s.read(1)
	if v == nil {
		return false
	}
	*out = uint8(v[0])
	return true
}

// ReadUint16 decodes a big-endian, 16-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint16(out *uint16) bool {
	v := s.read(2)
	if v == nil {
		return false
	}
	*out = uint16(v[0])<<8 | uint16(v[1])
	return true
}

// ReadUint24 decodes a big-endian, 24-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint24(out *uint32) bool {
	v := s.read(3)
	if v == nil {
		return false
	}
	*out = uint32(v[0])<<16 | uint32(v[1])<<8 | uint32(v[2])
	return true
}

// ReadUint32 decodes a big-endian, 32-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint32(out *uint32) bool {
	v := s.read(4)
	if v == nil {
		return false
	}
	*out = uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
	return true
}

// ReadUint48 decodes a big-endian, 48-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint48(out *uint64) bool {
	v := s.read(6)
	if v == nil {
		return false
	}
	*out = uint64(v[0])<<40 | uint64(v[1])<<32 | uint64(v[2])<<24 | uint64(v[3])<<16 | uint64(v[4])<<8 | uint64(v[5])
	return true
}

// ReadUint64 decodes a big-endian, 64-bit value into out and advances over it.
// It reports whether the read was successful.
func (s *String) ReadUint64(out *uint64) bool {
	v := s.read(8)
	if v == nil {
		return false
	}
	*out = uint64(v[0])<<56 | uint64(v[1])<<48 | uint64(v[2])<<40 | uint64(v[3])<<32 | uint64(v[4])<<24 | uint64(v[5])<<16 | uint64(v[6])<<8 | uint64(v[7])
	return true
}

func (s *String) readUnsigned(out *uint32, length int) bool {
	v := s.read(length)
	if v == nil {
		return false
	}
	var result uint32
	for i := 0; i < length; i++ {
		result <<= 8
		result |= uint32(v[i])
	}
	*out = result
	return true
}

func (s *String) readLengthPrefixed(lenLen int, outChild *String) bool {
	lenBytes := s.read(lenLen)
	if lenBytes == nil {
		return false
	}
	var length uint32
	for _, b := range lenBytes {
		length = length << 8
		length = length | uint32(b)
	}
	v := s.read(int(length))
	if v == nil {
		return false
	}
	*outChild = v
	return true
}

// ReadUint8LengthPrefixed reads the content of an 8-bit length-prefixed value
// into out and advances over it. It reports whether the read was successful.
func (s *String) ReadUint8LengthPrefixed(out *String) bool {
	return s.readLengthPrefixed(1, out)
}

// ReadUint16LengthPrefixed reads the content of a big-endian, 16-bit
// length-prefixed value into out and advances over it. It reports whether the
// read was successful.
func (s *String) ReadUint16LengthPrefixed(out *String) bool {
	return s.readLengthPrefixed(2, out)
}

// ReadUint24LengthPrefixed reads the content of a big-endian, 24-bit
// length-prefixed value into out and advances over it. It reports whether
// the read was successful.
func (s *String) ReadUint24LengthPrefixed(out *String) bool {
	return s.readLengthPrefixed(3, out)
}

// ReadBytes reads n bytes into out and advances over them. It reports
// whether the read was successful.
func (s *String) ReadBytes(out *[]byte, n int) bool {
	v := s.read(n)
	if v == nil {
		return false
	}
	*out = v
	return true
}

// CopyBytes copies len(out) bytes into out and advances over them. It reports
// whether the copy operation was successful
func (s *String) CopyBytes(out []byte) bool {
	n := len(out)
	v := s.read(n)
	if v == nil {
		return false
	}
	return copy(out, v) == n
}

// Empty reports whether the string does not contain any bytes.
func (s String) Empty() bool {
	return len(s) == 0
}
// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.




// A Builder builds byte strings from fixed-length and length-prefixed values.
// Builders either allocate space as needed, or are ‘fixed’, which means that
// they write into a given buffer and produce an error if it's exhausted.
//
// The zero value is a usable Builder that allocates space as needed.
//
// Simple values are marshaled and appended to a Builder using methods on the
// Builder. Length-prefixed values are marshaled by providing a
// BuilderContinuation, which is a function that writes the inner contents of
// the value to a given Builder. See the documentation for BuilderContinuation
// for details.
type Builder struct {
	err            error
	result         []byte
	fixedSize      bool
	child          *Builder
	offset         int
	pendingLenLen  int
	pendingIsASN1  bool
	inContinuation *bool
}

// NewBuilder creates a Builder that appends its output to the given buffer.
// Like append(), the slice will be reallocated if its capacity is exceeded.
// Use Bytes to get the final buffer.
func NewBuilder(buffer []byte) *Builder {
	return &Builder{
		result: buffer,
	}
}

// NewFixedBuilder creates a Builder that appends its output into the given
// buffer. This builder does not reallocate the output buffer. Writes that
// would exceed the buffer's capacity are treated as an error.
func NewFixedBuilder(buffer []byte) *Builder {
	return &Builder{
		result:    buffer,
		fixedSize: true,
	}
}

// SetError sets the value to be returned as the error from Bytes. Writes
// performed after calling SetError are ignored.
func (b *Builder) SetError(err error) {
	b.err = err
}

// Bytes returns the bytes written by the builder or an error if one has
// occurred during building.
type __mrBuilder_BytesPair struct {
	v0 []byte
	v1 error
}

func __mrBuilder_Bytes(b *Builder) __mrBuilder_BytesPair {
	if b.err != nil {
		return __mrBuilder_BytesPair{nil, b.err}
	}
	return __mrBuilder_BytesPair{b.result[b.offset:], nil}
}

func (b *Builder) Bytes() ([]byte, error) {
	panic("golang.org/x/crypto/cryptobyte: Bytes: multi-result desugar")
}

// BytesOrPanic returns the bytes written by the builder or panics if an error
// has occurred during building.
func (b *Builder) BytesOrPanic() []byte {
	if b.err != nil {
		panic(b.err)
	}
	return b.result[b.offset:]
}

// AddUint8 appends an 8-bit value to the byte string.
func (b *Builder) AddUint8(v uint8) {
	b.add(byte(v))
}

// AddUint16 appends a big-endian, 16-bit value to the byte string.
func (b *Builder) AddUint16(v uint16) {
	b.add(byte(v>>8), byte(v))
}

// AddUint24 appends a big-endian, 24-bit value to the byte string. The highest
// byte of the 32-bit input value is silently truncated.
func (b *Builder) AddUint24(v uint32) {
	b.add(byte(v>>16), byte(v>>8), byte(v))
}

// AddUint32 appends a big-endian, 32-bit value to the byte string.
func (b *Builder) AddUint32(v uint32) {
	b.add(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// AddUint48 appends a big-endian, 48-bit value to the byte string.
func (b *Builder) AddUint48(v uint64) {
	b.add(byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// AddUint64 appends a big-endian, 64-bit value to the byte string.
func (b *Builder) AddUint64(v uint64) {
	b.add(byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// AddBytes appends a sequence of bytes to the byte string.
func (b *Builder) AddBytes(v []byte) {
	b.add(v...)
}

// BuilderContinuation is a continuation-passing interface for building
// length-prefixed byte sequences. Builder methods for length-prefixed
// sequences (AddUint8LengthPrefixed etc) will invoke the BuilderContinuation
// supplied to them. The child builder passed to the continuation can be used
// to build the content of the length-prefixed sequence. For example:
//
//	parent := cryptobyte.NewBuilder()
//	parent.AddUint8LengthPrefixed(func (child *Builder) {
//	  child.AddUint8(42)
//	  child.AddUint8LengthPrefixed(func (grandchild *Builder) {
//	    grandchild.AddUint8(5)
//	  })
//	})
//
// It is an error to write more bytes to the child than allowed by the reserved
// length prefix. After the continuation returns, the child must be considered
// invalid, i.e. users must not store any copies or references of the child
// that outlive the continuation.
//
// If the continuation panics with a value of type BuildError then the inner
// error will be returned as the error from Bytes. If the child panics
// otherwise then Bytes will repanic with the same value.
type BuilderContinuation func(child *Builder)

// BuildError wraps an error. If a BuilderContinuation panics with this value,
// the panic will be recovered and the inner error will be returned from
// Builder.Bytes.
type BuildError struct {
	Err error
}

// AddUint8LengthPrefixed adds a 8-bit length-prefixed byte sequence.
func (b *Builder) AddUint8LengthPrefixed(f BuilderContinuation) {
	b.addLengthPrefixed(1, false, f)
}

// AddUint16LengthPrefixed adds a big-endian, 16-bit length-prefixed byte sequence.
func (b *Builder) AddUint16LengthPrefixed(f BuilderContinuation) {
	b.addLengthPrefixed(2, false, f)
}

// AddUint24LengthPrefixed adds a big-endian, 24-bit length-prefixed byte sequence.
func (b *Builder) AddUint24LengthPrefixed(f BuilderContinuation) {
	b.addLengthPrefixed(3, false, f)
}

// AddUint32LengthPrefixed adds a big-endian, 32-bit length-prefixed byte sequence.
func (b *Builder) AddUint32LengthPrefixed(f BuilderContinuation) {
	b.addLengthPrefixed(4, false, f)
}

func (b *Builder) callContinuation(f BuilderContinuation, arg *Builder) {
	if !*b.inContinuation {
		*b.inContinuation = true

		defer func() {
			*b.inContinuation = false

			r := recover()
			if r == nil {
				return
			}

			if buildError, ok := r.(BuildError); ok {
				b.err = buildError.Err
			} else {
				panic(r)
			}
		}()
	}

	f(arg)
}

func (b *Builder) addLengthPrefixed(lenLen int, isASN1 bool, f BuilderContinuation) {
	// Subsequent writes can be ignored if the builder has encountered an error.
	if b.err != nil {
		return
	}

	offset := len(b.result)
	b.add(make([]byte, lenLen)...)

	if b.inContinuation == nil {
		b.inContinuation = new(bool)
	}

	b.child = &Builder{
		result:         b.result,
		fixedSize:      b.fixedSize,
		offset:         offset,
		pendingLenLen:  lenLen,
		pendingIsASN1:  isASN1,
		inContinuation: b.inContinuation,
	}

	b.callContinuation(f, b.child)
	b.flushChild()
	if b.child != nil {
		panic("cryptobyte: internal error")
	}
}

func (b *Builder) flushChild() {
	if b.child == nil {
		return
	}
	b.child.flushChild()
	child := b.child
	b.child = nil

	if child.err != nil {
		b.err = child.err
		return
	}

	length := len(child.result) - child.pendingLenLen - child.offset

	if length < 0 {
		panic("cryptobyte: internal error") // result unexpectedly shrunk
	}

	if child.pendingIsASN1 {
		// For ASN.1, we reserved a single byte for the length. If that turned out
		// to be incorrect, we have to move the contents along in order to make
		// space.
		if child.pendingLenLen != 1 {
			panic("cryptobyte: internal error")
		}
		var lenLen, lenByte uint8
		if int64(length) > 0xfffffffe {
			b.err = errors.New("pending ASN.1 child too long")
			return
		} else if length > 0xffffff {
			lenLen = 5
			lenByte = 0x80 | 4
		} else if length > 0xffff {
			lenLen = 4
			lenByte = 0x80 | 3
		} else if length > 0xff {
			lenLen = 3
			lenByte = 0x80 | 2
		} else if length > 0x7f {
			lenLen = 2
			lenByte = 0x80 | 1
		} else {
			lenLen = 1
			lenByte = uint8(length)
			length = 0
		}

		// Insert the initial length byte, make space for successive length bytes,
		// and adjust the offset.
		child.result[child.offset] = lenByte
		extraBytes := int(lenLen - 1)
		if extraBytes != 0 {
			child.add(make([]byte, extraBytes)...)
			childStart := child.offset + child.pendingLenLen
			copy(child.result[childStart+extraBytes:], child.result[childStart:])
		}
		child.offset++
		child.pendingLenLen = extraBytes
	}

	l := length
	for i := child.pendingLenLen - 1; i >= 0; i-- {
		child.result[child.offset+i] = uint8(l)
		l >>= 8
	}
	if l != 0 {
		b.err = fmt.Errorf("cryptobyte: pending child length %d exceeds %d-byte length prefix", length, child.pendingLenLen)
		return
	}

	if b.fixedSize && &b.result[0] != &child.result[0] {
		panic("cryptobyte: BuilderContinuation reallocated a fixed-size buffer")
	}

	b.result = child.result
}

func (b *Builder) add(bytes ...byte) {
	if b.err != nil {
		return
	}
	if b.child != nil {
		panic("cryptobyte: attempted write while child is pending")
	}
	if len(b.result)+len(bytes) < len(bytes) {
		b.err = errors.New("cryptobyte: length overflow")
	}
	if b.fixedSize && len(b.result)+len(bytes) > cap(b.result) {
		b.err = errors.New("cryptobyte: Builder is exceeding its fixed-size buffer")
		return
	}
	b.result = append(b.result, bytes...)
}

// Unwrite rolls back non-negative n bytes written directly to the Builder.
// An attempt by a child builder passed to a continuation to unwrite bytes
// from its parent will panic.
func (b *Builder) Unwrite(n int) {
	if b.err != nil {
		return
	}
	if b.child != nil {
		panic("cryptobyte: attempted unwrite while child is pending")
	}
	length := len(b.result) - b.pendingLenLen - b.offset
	if length < 0 {
		panic("cryptobyte: internal error")
	}
	if n < 0 {
		panic("cryptobyte: attempted to unwrite negative number of bytes")
	}
	if n > length {
		panic("cryptobyte: attempted to unwrite more than was written")
	}
	b.result = b.result[:len(b.result)-n]
}

// A MarshalingValue marshals itself into a Builder.
type MarshalingValue interface {
	// Marshal is called by Builder.AddValue. It receives a pointer to a builder
	// to marshal itself into. It may return an error that occurred during
	// marshaling, such as unset or invalid values.
	Marshal(b *Builder) error
}

// AddValue calls Marshal on v, passing a pointer to the builder to append to.
// If Marshal returns an error, it is set on the Builder so that subsequent
// appends don't have an effect.
func (b *Builder) AddValue(v MarshalingValue) {
	err := v.Marshal(b)
	if err != nil {
		b.err = err
	}
}


// P3-M9 (tier-5b): ASN.1 Builder/String methods, vendored from
// go1.27.1 src/vendor/golang.org/x/crypto/cryptobyte/asn1.go.
// The v0.41.0 module source used for the base package lacks them, but
// std crypto/ecdsa (and the synthetic ecdsa source) needs AddASN1,
// ReadASN1, ReadASN1Integer. ReadASN1Integer is simplified: only the
// *big.Int case is implemented (the only one used by the tier-5
// sources); other types panic (fail-fast, honest).

// AddASN1 appends a DER-encoded ASN.1 object to the Builder.
func (b *Builder) AddASN1(tag asn1.Tag, f BuilderContinuation) {
	if b.err != nil {
		return
	}
	if tag&0x1f == 0x1f {
		b.err = asne("cryptobyte: high-tag number identifier octets not supported")
		return
	}
	b.AddUint8(uint8(tag))
	b.addLengthPrefixed(1, true, f)
}

func asne(s string) error {
	return &asn1Error{s}
}

type asn1Error struct{ s string }

func (e *asn1Error) Error() string { return e.s }

// ReadASN1 reads the contents of a DER-encoded ASN.1 element (not
// including tag and length bytes) into out, and advances. The element
// must match the given tag. It reports whether the read was successful.
func (s *String) ReadASN1(out *String, tag asn1.Tag) bool {
	var t asn1.Tag
	if !s.ReadAnyASN1(out, &t) || t != tag {
		return false
	}
	return true
}

// ReadASN1Integer decodes an ASN.1 INTEGER and converts it to out.
// Simplified from go1.27.1: only *big.Int and *[]byte are implemented
// (the cases used by the tier-5 sources); other types panic.
func (s *String) ReadASN1Integer(out interface{}) bool {
	switch out := out.(type) {
	case *big.Int:
		return s.readASN1BigInt(out)
	case *[]byte:
		return s.readASN1Bytes(out)
	default:
		panic("cryptobyte: ReadASN1Integer: unsupported type")
	}
}

func (s *String) readASN1Bytes(out *[]byte) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.INTEGER) || !checkASN1Integer(bytes) {
		return false
	}
	if bytes[0]&0x80 == 0x80 {
		return false
	}
	for len(bytes) > 1 && bytes[0] == 0 {
		bytes = bytes[1:]
	}
	*out = bytes
	return true
}

var bigOne = big.NewInt(1)

func (s *String) readASN1BigInt(out *big.Int) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.INTEGER) || !checkASN1Integer(bytes) {
		return false
	}
	if bytes[0]&0x80 == 0x80 {
		neg := make([]byte, len(bytes))
		for i, b := range bytes {
			neg[i] = ^b
		}
		out.SetBytes(neg)
		out.Add(out, bigOne)
		out.Neg(out)
	} else {
		out.SetBytes(bytes)
	}
	return true
}

func checkASN1Integer(bytes []byte) bool {
	if len(bytes) == 0 {
		return false
	}
	if len(bytes) == 1 {
		return true
	}
	if bytes[0] == 0x00 && bytes[1]&0x80 == 0x00 {
		return false
	}
	if bytes[0] == 0xff && bytes[1]&0x80 == 0x80 {
		return false
	}
	return true
}

// ReadAnyASN1 reads a DER-encoded ASN.1 element (not including tag and
// length bytes) into out, and advances. It reports whether the read was
// successful, and sets outTag to the tag of the element.
func (s *String) ReadAnyASN1(out *String, outTag *asn1.Tag) bool {
	return s.readASN1(out, outTag, true)
}

// ReadAnyASN1Element reads the contents of a DER-encoded ASN.1 element
// (including tag and length bytes) into out, sets outTag to its tag, and
// advances. It reports whether the read was successful.
//
// Tags greater than 30 are not supported (i.e. low-tag-number format only).
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadAnyASN1Element(out *String, outTag *asn1.Tag) bool {
	return s.readASN1(out, outTag, false /* include header */)
}

// ReadASN1BitString decodes an ASN.1 BIT STRING into out and advances.
// It reports whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1BitString(out *encoding_asn1.BitString) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.BIT_STRING) || len(bytes) == 0 ||
		len(bytes)*8/8 != len(bytes) {
		return false
	}

	paddingBits := bytes[0]
	bytes = bytes[1:]
	if paddingBits > 7 ||
		len(bytes) == 0 && paddingBits != 0 ||
		len(bytes) > 0 && bytes[len(bytes)-1]&(1<<paddingBits-1) != 0 {
		return false
	}

	out.BitLength = len(bytes)*8 - int(paddingBits)
	out.Bytes = bytes
	return true
}

// ReadASN1Boolean decodes an ASN.1 BOOLEAN and converts it to a boolean
// representation into out and advances. It reports whether the read
// was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1Boolean(out *bool) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.BOOLEAN) || len(bytes) != 1 {
		return false
	}

	switch bytes[0] {
	case 0:
		*out = false
	case 0xff:
		*out = true
	default:
		return false
	}

	return true
}

// ReadASN1Enum decodes an ASN.1 ENUMERATION into out and advances. It reports
// whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1Enum(out *int) bool {
	var bytes String
	var i int64
	if !s.ReadASN1(&bytes, asn1.ENUM) || !checkASN1Integer(bytes) || !asn1Signed(&i, bytes) {
		return false
	}
	if int64(int(i)) != i {
		return false
	}
	*out = int(i)
	return true
}

// asn1Signed converts a DER-encoded signed integer into out and reports
// whether the conversion was successful.
// From x/crypto v0.41.0.
func asn1Signed(out *int64, n []byte) bool {
	length := len(n)
	if length > 8 {
		return false
	}
	for i := 0; i < length; i++ {
		*out <<= 8
		*out |= int64(n[i])
	}
	// Shift up and down in order to sign extend the result.
	*out <<= 64 - uint8(length)*8
	*out >>= 64 - uint8(length)*8
	return true
}

const generalizedTimeFormatStr = "20060102150405Z0700"
const defaultUTCTimeFormatStr = "060102150405Z0700"

// ReadASN1GeneralizedTime decodes an ASN.1 GENERALIZEDTIME into out and
// advances. It reports whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1GeneralizedTime(out *time.Time) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.GeneralizedTime) {
		return false
	}
	t := string(bytes)
	res, err := time.Parse(generalizedTimeFormatStr, t)
	if err != nil {
		return false
	}
	if serialized := res.Format(generalizedTimeFormatStr); serialized != t {
		return false
	}
	*out = res
	return true
}

// ReadASN1UTCTime decodes an ASN.1 UTCTime into out and advances.
// It reports whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1UTCTime(out *time.Time) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.UTCTime) {
		return false
	}
	t := string(bytes)

	formatStr := defaultUTCTimeFormatStr
	var err error
	res, err := time.Parse(formatStr, t)
	if err != nil {
		// Fallback to minute precision if we can't parse second
		// precision. If we are following X.509 or X.690 we shouldn't
		// support this, but we do.
		formatStr = "0601021504Z0700"
		res, err = time.Parse(formatStr, t)
	}
	if err != nil {
		return false
	}

	if serialized := res.Format(formatStr); serialized != t {
		return false
	}

	if res.Year() >= 2050 {
		// UTCTime interprets the low order digits 50-99 as 1950-99.
		// This only applies to its use in the X.509 profile.
		// See https://tools.ietf.org/html/rfc5280#section-4.1.2.5.1
		res = res.AddDate(-100, 0, 0)
	}
	*out = res
	return true
}

// ReadASN1Int64WithTag decodes an ASN.1 INTEGER with the given tag into out
// and advances. It reports whether the read was successful and resulted in a
// value that can be represented in an int64.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1Int64WithTag(out *int64, tag asn1.Tag) bool {
	var bytes String
	return s.ReadASN1(&bytes, tag) && checkASN1Integer(bytes) && asn1Signed(out, bytes)
}

// ReadASN1ObjectIdentifier decodes an ASN.1 OBJECT IDENTIFIER into out and
// advances. It reports whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1ObjectIdentifier(out *encoding_asn1.ObjectIdentifier) bool {
	var bytes String
	if !s.ReadASN1(&bytes, asn1.OBJECT_IDENTIFIER) || len(bytes) == 0 {
		return false
	}

	// In the worst case, we get two elements from the first byte (which is
	// encoded differently) and then every varint is a single byte long.
	components := make([]int, len(bytes)+1)

	// The first varint is 40*value1 + value2:
	// According to this packing, value1 can take the values 0, 1 and 2 only.
	// When value1 = 0 or value1 = 1, then value2 is <= 39. When value1 = 2,
	// then there are no restrictions on value2.
	var v int
	if !bytes.readBase128Int(&v) {
		return false
	}
	if v < 80 {
		components[0] = v / 40
		components[1] = v % 40
	} else {
		components[0] = 2
		components[1] = v - 80
	}

	i := 2
	for ; len(bytes) > 0; i++ {
		if !bytes.readBase128Int(&v) {
			return false
		}
		components[i] = v
	}
	*out = components[:i]
	return true
}

// readBase128Int reads a base-128 encoded integer from s into out.
// From x/crypto v0.41.0.
func (s *String) readBase128Int(out *int) bool {
	ret := 0
	for i := 0; len(*s) > 0; i++ {
		if i == 5 {
			return false
		}
		// Avoid overflowing int on a 32-bit platform.
		// We don't want different behavior based on the architecture.
		if ret >= 1<<(31-7) {
			return false
		}
		ret <<= 7
		b := s.read(1)[0]

		// ITU-T X.690, section 8.19.2:
		// The subidentifier shall be encoded in the fewest possible octets,
		// that is, the leading octet of the subidentifier shall not have the value 0x80.
		if i == 0 && b == 0x80 {
			return false
		}

		ret |= int(b & 0x7f)
		if b&0x80 == 0 {
			*out = ret
			return true
		}
	}
	return false
}

// AddASN1ObjectIdentifier appends an ASN.1 OBJECT IDENTIFIER to the Builder.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (b *Builder) AddASN1ObjectIdentifier(oid encoding_asn1.ObjectIdentifier) {
	b.AddASN1(asn1.OBJECT_IDENTIFIER, func(b *Builder) {
		if !isValidOID(oid) {
			b.err = fmt.Errorf("cryptobyte: invalid OID: %v", oid)
			return
		}

		b.addBase128Int(int64(oid[0])*40 + int64(oid[1]))
		for _, v := range oid[2:] {
			b.addBase128Int(int64(v))
		}
	})
}

// addBase128Int appends a base-128 encoded integer to the Builder.
// From x/crypto v0.41.0.
func (b *Builder) addBase128Int(n int64) {
	var length int
	if n == 0 {
		length = 1
	} else {
		for i := n; i > 0; i >>= 7 {
			length++
		}
	}

	for i := length - 1; i >= 0; i-- {
		o := byte(n >> uint(i*7))
		o &= 0x7f
		if i != 0 {
			o |= 0x80
		}
		b.AddUint8(o)
	}
}

// isValidOID reports whether oid is a syntactically valid OBJECT IDENTIFIER.
// From x/crypto v0.41.0.
func isValidOID(oid encoding_asn1.ObjectIdentifier) bool {
	if len(oid) < 2 {
		return false
	}

	if oid[0] > 2 || (oid[0] <= 1 && oid[1] >= 40) {
		return false
	}

	for _, v := range oid {
		if v < 0 {
			return false
		}
	}
	return true
}

// ReadASN1Element reads the contents of a DER-encoded ASN.1 element (including
// tag and length bytes) into out, and advances. The element must match the
// given tag. It reports whether the read was successful.
//
// Tags greater than 30 are not supported (i.e. low-tag-number format only).
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadASN1Element(out *String, tag asn1.Tag) bool {
	var t asn1.Tag
	if !s.ReadAnyASN1Element(out, &t) || t != tag {
		return false
	}
	return true
}

// PeekASN1Tag reports whether the next ASN.1 value on the string starts with
// the given tag.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s String) PeekASN1Tag(tag asn1.Tag) bool {
	if len(s) == 0 {
		return false
	}
	return asn1.Tag(s[0]) == tag
}

// SkipASN1 reads and discards an ASN.1 element with the given tag. It
// reports whether the operation was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) SkipASN1(tag asn1.Tag) bool {
	var unused String
	return s.ReadASN1(&unused, tag)
}

// ReadOptionalASN1 attempts to read the contents of a DER-encoded ASN.1
// element (not including tag and length bytes) tagged with the given tag into
// out. It stores whether an element with the tag was found in outPresent,
// unless outPresent is nil. It reports whether the read was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadOptionalASN1(out *String, outPresent *bool, tag asn1.Tag) bool {
	present := s.PeekASN1Tag(tag)
	if outPresent != nil {
		*outPresent = present
	}
	if present && !s.ReadASN1(out, tag) {
		return false
	}
	return true
}

// ReadOptionalASN1Integer attempts to read an optional ASN.1 INTEGER explicitly
// tagged with tag into out and advances. If no element with a matching tag is
// present, it writes defaultValue into out instead. Otherwise, it behaves like
// ReadASN1Integer.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) ReadOptionalASN1Integer(out interface{}, tag asn1.Tag, defaultValue interface{}) bool {
	var present bool
	var i String
	if !s.ReadOptionalASN1(&i, &present, tag) {
		return false
	}
	if !present {
		switch out.(type) {
		case *int, *int8, *int16, *int32, *int64,
			*uint, *uint8, *uint16, *uint32, *uint64, *[]byte:
			reflect.ValueOf(out).Elem().Set(reflect.ValueOf(defaultValue))
		case *big.Int:
			if defaultValue, ok := defaultValue.(*big.Int); ok {
				out.(*big.Int).Set(defaultValue)
			} else {
				panic("out points to big.Int, but defaultValue does not")
			}
		default:
			panic("invalid integer type")
		}
		return true
	}
	if !i.ReadASN1Integer(out) || !i.Empty() {
		return false
	}
	return true
}

// SkipOptionalASN1 advances s over an ASN.1 element with the given tag, or
// else leaves s unchanged. It reports whether the operation was successful.
// From x/crypto v0.41.0 (required by real crypto/x509 from disk).
func (s *String) SkipOptionalASN1(tag asn1.Tag) bool {
	if !s.PeekASN1Tag(tag) {
		return true
	}
	var unused String
	return s.ReadASN1(&unused, tag)
}

func (s *String) readASN1(out *String, outTag *asn1.Tag, skipHeader bool) bool {
	if len(*s) < 2 {
		return false
	}
	tag, lenByte := (*s)[0], (*s)[1]
	if tag&0x1f == 0x1f {
		return false
	}
	if outTag != nil {
		*outTag = asn1.Tag(tag)
	}
	var length, headerLen uint32
	if lenByte&0x80 == 0 {
		length = uint32(lenByte) + 2
		headerLen = 2
	} else {
		lenLen := lenByte & 0x7f
		var len32 uint32
		if lenLen == 0 || lenLen > 4 || len(*s) < int(2+lenLen) {
			return false
		}
		lenBytes := String((*s)[2 : 2+lenLen])
		if !lenBytes.readUnsigned(&len32, int(lenLen)) {
			return false
		}
		if len32 < 128 {
			return false
		}
		length = len32 + 2 + uint32(lenLen)
		headerLen = 2 + uint32(lenLen)
	}
	if uint32(len(*s)) < length {
		return false
	}
	if skipHeader {
		*out = (*s)[headerLen:length]
	} else {
		*out = (*s)[:length]
	}
	*s = (*s)[length:]
	return true
}

`
