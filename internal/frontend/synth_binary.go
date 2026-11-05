// P3-M7 (wave 1): synthetic `encoding/binary` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/binary")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (binary.go, varint.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): the ByteOrder/AppendByteOrder
// interfaces, bigEndian/littleEndian/nativeEndian with all methods,
// BigEndian/LittleEndian/NativeEndian vars, MaxVarintLen consts,
// AppendUvarint/PutUvarint/AppendVarint/PutVarint, Size, and the
// streaming API Read/Write/ReadUvarint/ReadVarint are real Go, adapted
// from the gc 1.27.1 implementation. NativeEndian follows the M6
// build-tag strategy: the synthetic bakes the linux/amd64-selected
// variant (native_endian_little.go), per GG-0023. Size implements gc's
// type-switch exactly (scalars, pointers, slices, complex, nil); the
// reflect fallback for arrays/structs/named types fails fast — fail
// fast, never silently.
//
// Multi-result members (Uvarint, Varint, ReadUvarint, ReadVarint) keep
// exact gc signatures with fail-fast bodies for type-checking; their
// real implementations live in the single-result __binaryUvarint/
// __binaryVarint/__binaryReadUvarint/__binaryReadVarint helpers, and
// the exporter desugars N-target call sites to them (Appendix M
// pattern, mrdesugar_m5.go). Any other call position fails fast with
// the named M7 scope error.
//
// Read/Write scope (P3-M8, review-1105-binary-read): the fast path is
// real — gc's intDataSize shapes (bool/int/uint/float of fixed width,
// pointers and slices thereof) decode/encode over the synthetic core,
// with gc's exact semantics including the io.ReadFull EOF /
// ErrUnexpectedEOF mapping and the exact "invalid type" error for
// plain scalar values passed by value. Structs, arrays, named types,
// complex values and int/uint/uintptr need gc's reflect-based
// decoder/encoder (unavailable to v1 synthetic packages): they fail
// fast with the named M7 scope error — never silently. Re-test trigger:
// when the reflect-based codec lands (same trigger as
// Append/Encode/Decode below), port gc's decoder/encoder/dataSize and
// drop the scope panic. Consequence until then: debug/elf imports and
// type-checks (its binary.Read call sites resolve), but reading an ELF
// section header — a struct — hits the loud scope error at runtime.
//
// Out-of-scope members keep exact gc signatures with fail-fast bodies:
// Append/Encode/Decode need reflect (unavailable to v1 synthetic
// packages); re-test when reflect lands.
package main

// syntheticBinarySource is the P3-M7 synthetic `encoding/binary`
// package: exact gc 1.27.1 signatures for the in-scope API; real-Go
// byte-order, varint and streaming core (fast path); fail-fast bodies
// for the multi-result members (desugared) and the reflect-based codec
// (out of scope).
const syntheticBinarySource = `package binary

import (
	"errors"
	"io"
	"math"
)

// A ByteOrder specifies how to convert byte slices into 16-, 32-, or
// 64-bit unsigned integers.
type ByteOrder interface {
	Uint16([]byte) uint16
	Uint32([]byte) uint32
	Uint64([]byte) uint64
	PutUint16([]byte, uint16)
	PutUint32([]byte, uint32)
	PutUint64([]byte, uint64)
	String() string
}

// AppendByteOrder specifies how to append 16-, 32-, or 64-bit unsigned
// integers into a byte slice.
type AppendByteOrder interface {
	AppendUint16([]byte, uint16) []byte
	AppendUint32([]byte, uint32) []byte
	AppendUint64([]byte, uint64) []byte
	String() string
}

// LittleEndian is the little-endian implementation of ByteOrder and
// AppendByteOrder.
var LittleEndian littleEndian

// BigEndian is the big-endian implementation of ByteOrder and
// AppendByteOrder.
var BigEndian bigEndian

type littleEndian struct{}

func (littleEndian) Uint16(b []byte) uint16 {
	_ = b[1]
	return uint16(b[0]) | uint16(b[1])<<8
}

func (littleEndian) PutUint16(b []byte, v uint16) {
	_ = b[1]
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func (littleEndian) AppendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

func (littleEndian) Uint32(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func (littleEndian) PutUint32(b []byte, v uint32) {
	_ = b[3]
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func (littleEndian) AppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func (littleEndian) Uint64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func (littleEndian) PutUint64(b []byte, v uint64) {
	_ = b[7]
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}

func (littleEndian) AppendUint64(b []byte, v uint64) []byte {
	return append(b,
		byte(v),
		byte(v>>8),
		byte(v>>16),
		byte(v>>24),
		byte(v>>32),
		byte(v>>40),
		byte(v>>48),
		byte(v>>56),
	)
}

func (littleEndian) String() string { return "LittleEndian" }

func (littleEndian) GoString() string { return "binary.LittleEndian" }

type bigEndian struct{}

func (bigEndian) Uint16(b []byte) uint16 {
	_ = b[1]
	return uint16(b[1]) | uint16(b[0])<<8
}

func (bigEndian) PutUint16(b []byte, v uint16) {
	_ = b[1]
	b[0] = byte(v >> 8)
	b[1] = byte(v)
}

func (bigEndian) AppendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func (bigEndian) Uint32(b []byte) uint32 {
	_ = b[3]
	return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24
}

func (bigEndian) PutUint32(b []byte, v uint32) {
	_ = b[3]
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func (bigEndian) AppendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (bigEndian) Uint64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}

func (bigEndian) PutUint64(b []byte, v uint64) {
	_ = b[7]
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
}

func (bigEndian) AppendUint64(b []byte, v uint64) []byte {
	return append(b,
		byte(v>>56),
		byte(v>>48),
		byte(v>>40),
		byte(v>>32),
		byte(v>>24),
		byte(v>>16),
		byte(v>>8),
		byte(v),
	)
}

func (bigEndian) String() string { return "BigEndian" }

func (bigEndian) GoString() string { return "binary.BigEndian" }

// nativeEndian is the M6 build-tag strategy made concrete: gc selects
// native_endian_little.go on linux/amd64 (GG-0023), so the synthetic
// bakes the little-endian variant.
type nativeEndian struct {
	littleEndian
}

// NativeEndian is the native-endian implementation of ByteOrder and
// AppendByteOrder (little-endian on linux/amd64, per the M6 build-tag
// selection).
var NativeEndian nativeEndian

func (nativeEndian) String() string { return "NativeEndian" }

func (nativeEndian) GoString() string { return "binary.NativeEndian" }

// MaxVarintLenN is the maximum length of a varint-encoded N-bit integer.
const (
	MaxVarintLen16 = 3
	MaxVarintLen32 = 5
	MaxVarintLen64 = 10
)

// AppendUvarint appends the varint-encoded form of x to buf.
func AppendUvarint(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = append(buf, byte(x)|0x80)
		x >>= 7
	}
	return append(buf, byte(x))
}

// PutUvarint encodes a uint64 into buf and returns the number of bytes
// written. If the buffer is too small, PutUvarint will panic.
func PutUvarint(buf []byte, x uint64) int {
	i := 0
	for x >= 0x80 {
		buf[i] = byte(x) | 0x80
		x >>= 7
		i++
	}
	buf[i] = byte(x)
	return i + 1
}

// binaryUvarintPair is the Appendix M single-result carrier for Uvarint.
type binaryUvarintPair struct {
	x uint64
	n int
}

// __binaryUvarint is the Appendix M desugar target behind Uvarint: the
// real decode logic (gc 1.27.1 semantics: n == 0 means buf too small,
// n < 0 means overflow with -n bytes read).
func __binaryUvarint(buf []byte) binaryUvarintPair {
	var x uint64
	var s uint
	for i, b := range buf {
		if i == MaxVarintLen64 {
			return binaryUvarintPair{n: -(i + 1)}
		}
		if b < 0x80 {
			if i == MaxVarintLen64-1 && b > 1 {
				return binaryUvarintPair{n: -(i + 1)}
			}
			return binaryUvarintPair{x: x | uint64(b)<<s, n: i + 1}
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return binaryUvarintPair{}
}

// Uvarint keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Uvarint(buf []byte) (uint64, int) {
	panic("encoding/binary: Uvarint: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// AppendVarint appends the varint-encoded form of x to buf.
func AppendVarint(buf []byte, x int64) []byte {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return AppendUvarint(buf, ux)
}

// PutVarint encodes an int64 into buf and returns the number of bytes
// written. If the buffer is too small, PutVarint will panic.
func PutVarint(buf []byte, x int64) int {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return PutUvarint(buf, ux)
}

// binaryVarintPair is the Appendix M single-result carrier for Varint.
type binaryVarintPair struct {
	x int64
	n int
}

// __binaryVarint is the Appendix M desugar target behind Varint.
func __binaryVarint(buf []byte) binaryVarintPair {
	pr := __binaryUvarint(buf)
	x := int64(pr.x >> 1)
	if pr.x&1 != 0 {
		x = ^x
	}
	return binaryVarintPair{x: x, n: pr.n}
}

// Varint keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Varint(buf []byte) (int64, int) {
	panic("encoding/binary: Varint: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

var errOverflow = errors.New("binary: varint overflows a 64-bit integer")

// binaryReadUvarintPair is the Appendix M single-result carrier for ReadUvarint.
type binaryReadUvarintPair struct {
	x   uint64
	err error
}

// __binaryReadUvarint is the Appendix M desugar target behind ReadUvarint:
// the real decode logic (gc 1.27.1 varint.go semantics: the error is
// io.EOF only if no bytes were read; io.ErrUnexpectedEOF after a partial
// read; errOverflow past MaxVarintLen64 or on a too-large final byte).
func __binaryReadUvarint(r io.ByteReader) binaryReadUvarintPair {
	var x uint64
	var s uint
	for i := 0; i < MaxVarintLen64; i++ {
		b, err := r.ReadByte()
		if err != nil {
			if i > 0 && err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return binaryReadUvarintPair{x, err}
		}
		if b < 0x80 {
			if i == MaxVarintLen64-1 && b > 1 {
				return binaryReadUvarintPair{x, errOverflow}
			}
			return binaryReadUvarintPair{x | uint64(b)<<s, nil}
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return binaryReadUvarintPair{x, errOverflow}
}

// ReadUvarint keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m5.go),
// every other position fails fast. Body never runs.
func ReadUvarint(r io.ByteReader) (uint64, error) {
	panic("encoding/binary: ReadUvarint: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// binaryReadVarintPair is the Appendix M single-result carrier for ReadVarint.
type binaryReadVarintPair struct {
	x   int64
	err error
}

// __binaryReadVarint is the Appendix M desugar target behind ReadVarint.
func __binaryReadVarint(r io.ByteReader) binaryReadVarintPair {
	pr := __binaryReadUvarint(r)
	x := int64(pr.x >> 1)
	if pr.x&1 != 0 {
		x = ^x
	}
	return binaryReadVarintPair{x, pr.err}
}

// ReadVarint keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m5.go),
// every other position fails fast. Body never runs.
func ReadVarint(r io.ByteReader) (int64, error) {
	panic("encoding/binary: ReadVarint: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// Append keeps the exact gc 1.27.1 signature for type-checking. A real
// implementation needs reflect (arbitrary struct/array walking) —
// unavailable to v1 synthetic packages. Body fails fast; re-test when
// reflect lands.
func Append(buf []byte, order ByteOrder, data any) ([]byte, error) {
	panic("encoding/binary: Append: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Encode keeps the exact gc 1.27.1 signature for type-checking. A real
// implementation needs reflect (arbitrary struct/array walking) —
// unavailable to v1 synthetic packages. Body fails fast; re-test when
// reflect lands.
func Encode(buf []byte, order ByteOrder, data any) (int, error) {
	panic("encoding/binary: Encode: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Decode keeps the exact gc 1.27.1 signature for type-checking. A real
// implementation needs reflect (arbitrary struct/array walking) —
// unavailable to v1 synthetic packages. Body fails fast; re-test when
// reflect lands.
func Decode(buf []byte, order ByteOrder, data any) (int, error) {
	panic("encoding/binary: Decode: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// Size returns the number of bytes the binary encoding of v would take
// up. This is gc's type-switch exactly (scalars, pointers, slices,
// complex, nil); the reflect fallback for arrays, structs and named
// types fails fast — fail fast, never silently.
func Size(v any) int {
	switch data := v.(type) {
	case nil:
		return -1
	case bool, int8, uint8:
		return 1
	case *bool:
		if data == nil {
			return -1
		}
		return 1
	case *int8:
		if data == nil {
			return -1
		}
		return 1
	case *uint8:
		if data == nil {
			return -1
		}
		return 1
	case []bool:
		return len(data)
	case []int8:
		return len(data)
	case []uint8:
		return len(data)
	case int16, uint16:
		return 2
	case *int16:
		if data == nil {
			return -1
		}
		return 2
	case *uint16:
		if data == nil {
			return -1
		}
		return 2
	case []int16:
		return 2 * len(data)
	case []uint16:
		return 2 * len(data)
	case int32, uint32:
		return 4
	case *int32:
		if data == nil {
			return -1
		}
		return 4
	case *uint32:
		if data == nil {
			return -1
		}
		return 4
	case []int32:
		return 4 * len(data)
	case []uint32:
		return 4 * len(data)
	case int64, uint64:
		return 8
	case *int64:
		if data == nil {
			return -1
		}
		return 8
	case *uint64:
		if data == nil {
			return -1
		}
		return 8
	case []int64:
		return 8 * len(data)
	case []uint64:
		return 8 * len(data)
	case float32:
		return 4
	case *float32:
		if data == nil {
			return -1
		}
		return 4
	case float64:
		return 8
	case *float64:
		if data == nil {
			return -1
		}
		return 8
	case []float32:
		return 4 * len(data)
	case []float64:
		return 8 * len(data)
	case complex64:
		return 8
	case *complex64:
		if data == nil {
			return -1
		}
		return 8
	case complex128:
		return 16
	case *complex128:
		if data == nil {
			return -1
		}
		return 16
	case []complex64:
		return 8 * len(data)
	case []complex128:
		return 16 * len(data)
	case int, uint, uintptr:
		// gc's sizeof has no case for Int/Uint/Uintptr (platform-
		// dependent width is not a fixed size here) → dataSize -1.
		return -1
	case *int, *uint, *uintptr:
		return -1
	case []int, []uint, []uintptr:
		return -1
	}
	panic("encoding/binary: Size: not in M7 scope (needs reflect for arrays, structs and named types; re-test when reflect lands)")
}

// intDataSize returns the size of the data required to represent the data when encoded,
// and optionally a byte slice containing the encoded data if no conversion is necessary.
// It returns zero, nil if the type cannot be implemented by the fast path in Read or Write.
// (gc 1.27.1 binary.go, verbatim.)
func intDataSize(data any) (int, []byte) {
	switch data := data.(type) {
	case bool, int8, uint8, *bool, *int8, *uint8:
		return 1, nil
	case []bool:
		return len(data), nil
	case []int8:
		return len(data), nil
	case []uint8:
		return len(data), data
	case int16, uint16, *int16, *uint16:
		return 2, nil
	case []int16:
		return 2 * len(data), nil
	case []uint16:
		return 2 * len(data), nil
	case int32, uint32, *int32, *uint32:
		return 4, nil
	case []int32:
		return 4 * len(data), nil
	case []uint32:
		return 4 * len(data), nil
	case int64, uint64, *int64, *uint64:
		return 8, nil
	case []int64:
		return 8 * len(data), nil
	case []uint64:
		return 8 * len(data), nil
	case float32, *float32:
		return 4, nil
	case float64, *float64:
		return 8, nil
	case []float32:
		return 4 * len(data), nil
	case []float64:
		return 8 * len(data), nil
	}
	return 0, nil
}

// decodeFast decodes bs into data (gc 1.27.1 binary.go, verbatim). It
// handles pointers to and slices of fixed-width scalars; every other
// shape returns false and the caller falls through to the reflect
// fallback (Read) or the scope error (this package).
func decodeFast(bs []byte, order ByteOrder, data any) bool {
	switch data := data.(type) {
	case *bool:
		*data = bs[0] != 0
	case *int8:
		*data = int8(bs[0])
	case *uint8:
		*data = bs[0]
	case *int16:
		*data = int16(order.Uint16(bs))
	case *uint16:
		*data = order.Uint16(bs)
	case *int32:
		*data = int32(order.Uint32(bs))
	case *uint32:
		*data = order.Uint32(bs)
	case *int64:
		*data = int64(order.Uint64(bs))
	case *uint64:
		*data = order.Uint64(bs)
	case *float32:
		*data = math.Float32frombits(order.Uint32(bs))
	case *float64:
		*data = math.Float64frombits(order.Uint64(bs))
	case []bool:
		for i, x := range bs { // Easier to loop over the input for 8-bit values.
			data[i] = x != 0
		}
	case []int8:
		for i, x := range bs {
			data[i] = int8(x)
		}
	case []uint8:
		copy(data, bs)
	case []int16:
		for i := range data {
			data[i] = int16(order.Uint16(bs[2*i:]))
		}
	case []uint16:
		for i := range data {
			data[i] = order.Uint16(bs[2*i:])
		}
	case []int32:
		for i := range data {
			data[i] = int32(order.Uint32(bs[4*i:]))
		}
	case []uint32:
		for i := range data {
			data[i] = order.Uint32(bs[4*i:])
		}
	case []int64:
		for i := range data {
			data[i] = int64(order.Uint64(bs[8*i:]))
		}
	case []uint64:
		for i := range data {
			data[i] = order.Uint64(bs[8*i:])
		}
	case []float32:
		for i := range data {
			data[i] = math.Float32frombits(order.Uint32(bs[4*i:]))
		}
	case []float64:
		for i := range data {
			data[i] = math.Float64frombits(order.Uint64(bs[8*i:]))
		}
	default:
		return false
	}
	return true
}

// encodeFast encodes data into bs (gc 1.27.1 binary.go, verbatim). Unlike
// decodeFast it handles plain scalar values as well as pointers and
// slices, so Write of a plain value writes the value's bytes.
func encodeFast(bs []byte, order ByteOrder, data any) {
	switch v := data.(type) {
	case *bool:
		if *v {
			bs[0] = 1
		} else {
			bs[0] = 0
		}
	case bool:
		if v {
			bs[0] = 1
		} else {
			bs[0] = 0
		}
	case []bool:
		for i, x := range v {
			if x {
				bs[i] = 1
			} else {
				bs[i] = 0
			}
		}
	case *int8:
		bs[0] = byte(*v)
	case int8:
		bs[0] = byte(v)
	case []int8:
		for i, x := range v {
			bs[i] = byte(x)
		}
	case *uint8:
		bs[0] = *v
	case uint8:
		bs[0] = v
	case []uint8:
		copy(bs, v)
	case *int16:
		order.PutUint16(bs, uint16(*v))
	case int16:
		order.PutUint16(bs, uint16(v))
	case []int16:
		for i, x := range v {
			order.PutUint16(bs[2*i:], uint16(x))
		}
	case *uint16:
		order.PutUint16(bs, *v)
	case uint16:
		order.PutUint16(bs, v)
	case []uint16:
		for i, x := range v {
			order.PutUint16(bs[2*i:], x)
		}
	case *int32:
		order.PutUint32(bs, uint32(*v))
	case int32:
		order.PutUint32(bs, uint32(v))
	case []int32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], uint32(x))
		}
	case *uint32:
		order.PutUint32(bs, *v)
	case uint32:
		order.PutUint32(bs, v)
	case []uint32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], x)
		}
	case *int64:
		order.PutUint64(bs, uint64(*v))
	case int64:
		order.PutUint64(bs, uint64(v))
	case []int64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], uint64(x))
		}
	case *uint64:
		order.PutUint64(bs, *v)
	case uint64:
		order.PutUint64(bs, v)
	case []uint64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], x)
		}
	case *float32:
		order.PutUint32(bs, math.Float32bits(*v))
	case float32:
		order.PutUint32(bs, math.Float32bits(v))
	case []float32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], math.Float32bits(x))
		}
	case *float64:
		order.PutUint64(bs, math.Float64bits(*v))
	case float64:
		order.PutUint64(bs, math.Float64bits(v))
	case []float64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], math.Float64bits(x))
		}
	}
}

// binaryInvalidTypeError renders gc's exact "invalid type" error for the
// one shape the fast path sizes but cannot decode: a plain scalar value
// passed by value (not a pointer). decodeFast has no case for these, so
// gc's reflect fallback rejects them; the type is enumerable here (the
// intDataSize shapes), so the message stays exact without reflect.
func binaryInvalidTypeError(data any) error {
	switch data.(type) {
	case bool:
		return errors.New("binary.Read: invalid type bool")
	case int8:
		return errors.New("binary.Read: invalid type int8")
	case uint8:
		return errors.New("binary.Read: invalid type uint8")
	case int16:
		return errors.New("binary.Read: invalid type int16")
	case uint16:
		return errors.New("binary.Read: invalid type uint16")
	case int32:
		return errors.New("binary.Read: invalid type int32")
	case uint32:
		return errors.New("binary.Read: invalid type uint32")
	case int64:
		return errors.New("binary.Read: invalid type int64")
	case uint64:
		return errors.New("binary.Read: invalid type uint64")
	case float32:
		return errors.New("binary.Read: invalid type float32")
	case float64:
		return errors.New("binary.Read: invalid type float64")
	}
	return errors.New("binary.Read: invalid type")
}

// Read reads structured binary data from r into data.
// Data must be a pointer to a fixed-size value or a slice
// of fixed-size values.
// Bytes read from r are decoded using the specified byte order
// and written to successive fields of the data.
// When decoding boolean values, a zero byte is decoded as false, and
// any other non-zero byte is decoded as true.
// When reading into structs, the field data for fields with
// blank (_) field names is skipped; i.e., blank field names
// may be used for padding.
// When reading into a struct, all non-blank fields must be exported
// or Read may panic.
//
// The error is [io.EOF] only if no bytes were read.
// If an [io.EOF] happens after reading some but not all the bytes,
// Read returns [io.ErrUnexpectedEOF].
//
// Fast-path scope (P3-M8): scalars and slices of fixed-width scalars
// decode for real; structs, arrays, named types, complex values and
// int/uint/uintptr need the reflect-based decoder and fail fast with the
// named M7 scope error (see the file header). Plain scalar values
// passed by value keep gc's exact "invalid type" error.
func Read(r io.Reader, order ByteOrder, data any) error {
	// Fast path for basic types and slices.
	if n, _ := intDataSize(data); n != 0 {
		bs := make([]byte, n)
		if _, err := io.ReadFull(r, bs); err != nil {
			return err
		}

		if decodeFast(bs, order, data) {
			return nil
		}

		// intDataSize sized the data but decodeFast has no case for
		// it: a plain scalar value, not a pointer — gc's reflect
		// fallback rejects it with the invalid-type error.
		return binaryInvalidTypeError(data)
	}

	// Structs, arrays, named types, complex, int/uint/uintptr: gc
	// decodes these through the reflect-based decoder — unavailable
	// to v1 synthetic packages. Fail fast, never silently.
	panic("encoding/binary: Read: not in M7 scope (needs reflect for structs, arrays and named types; re-test when reflect lands)")
}

// Write writes the binary representation of data into w.
// Data must be a fixed-size value or a slice of fixed-size
// values, or a pointer to such data.
// Boolean values encode as one byte: 1 for true, and 0 for false.
// Bytes written to w are encoded using the specified byte order
// and read from successive fields of the data.
// When writing structs, zero values are written for fields
// with blank (_) field names.
//
// Fast-path scope (P3-M8): scalars and slices of fixed-width scalars
// encode for real; structs, arrays, named types, complex values and
// int/uint/uintptr need the reflect-based encoder and fail fast with
// the named M7 scope error (see the file header).
func Write(w io.Writer, order ByteOrder, data any) error {
	// Fast path for basic types and slices.
	if n, bs := intDataSize(data); n != 0 {
		if bs == nil {
			bs = make([]byte, n)
			encodeFast(bs, order, data)
		}

		_, err := w.Write(bs)
		return err
	}

	// Structs, arrays, named types, complex, int/uint/uintptr: gc
	// encodes these through the reflect-based encoder — unavailable
	// to v1 synthetic packages. Fail fast, never silently.
	panic("encoding/binary: Write: not in M7 scope (needs reflect for structs, arrays and named types; re-test when reflect lands)")
}
`
