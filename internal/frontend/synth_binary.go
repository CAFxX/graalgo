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
// AppendUvarint/PutUvarint/AppendVarint/PutVarint, and Size are real Go,
// adapted from the gc 1.27.1 implementation (io- and reflect-free).
// NativeEndian follows the M6 build-tag strategy: the synthetic bakes
// the linux/amd64-selected variant (native_endian_little.go), per
// GG-0023. Size implements gc's type-switch exactly (scalars, pointers,
// slices, complex, nil); the reflect fallback for arrays/structs/named
// types fails fast — fail fast, never silently.
//
// Multi-result members (Uvarint, Varint) keep exact gc signatures with
// fail-fast bodies for type-checking; their real implementations live
// in the single-result __binaryUvarint/__binaryVarint helpers, and the
// exporter desugars N-target call sites to them (Appendix M pattern,
// mrdesugar_m7.go). Any other call position fails fast with the named
// M7 scope error.
//
// Out-of-scope members keep exact gc signatures with fail-fast bodies:
// Append/Encode/Decode need reflect (unavailable to v1 synthetic
// packages); re-test when reflect lands.
//
// Omitted (io and reflect are not in M7 scope — NOT OK per the wave-1
// import rules, so the exact gc signatures cannot even be declared):
//   func Read(r io.Reader, order ByteOrder, data any) error
//   func Write(w io.Writer, order ByteOrder, data any) error
//   func ReadUvarint(r io.ByteReader) (uint64, error)
//   func ReadVarint(r io.ByteReader) (int64, error)
// Re-test trigger: when io lands (P3-M8), declare these with exact gc
// signatures and implement them over the synthetic core. This omission
// is deliberate and loud — never silent (the p3frontend drift test
// asserts the omission list).
package main

// syntheticBinarySource is the P3-M7 synthetic `encoding/binary`
// package: exact gc 1.27.1 signatures for the in-scope API; real-Go
// byte-order and varint core; fail-fast bodies for the multi-result
// members (desugared), the reflect-based codec (out of scope), and the
// io-dependent streaming API (omitted).
const syntheticBinarySource = `package binary

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
`
