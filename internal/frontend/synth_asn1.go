// P3-M7 (wave 1): synthetic `encoding/asn1` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/asn1")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (asn1.go, common.go,
// marshal.go). Only frontend/v1-supported constructs (no generics, no
// multi-result declarations with real bodies — WS D): the tag/class
// constants, BitString/Enumerated/Flag/ObjectIdentifier/RawContent/
// RawValue/StructuralError/SyntaxError types with their methods,
// NullBytes/NullRawValue vars are real Go, adapted from gc 1.27.1
// (the math/big, reflect, slices, strings, strconv, time, unicode,
// unicode/utf16, utf8 helpers used by gc are replaced with local
// equivalents).
//
// Marshal/Unmarshal cover the DER subset implementable without
// reflect, via type switches:
//
//	Marshal: nil (error), bool, Flag, signed ints, []byte, string
//	  (PrintableString/UTF8String), BitString, ObjectIdentifier,
//	  Enumerated, RawValue, RawContent.
//	Scalar types gc rejects without reflect (float32/64, complex64/128,
//	  uint..uintptr) return StructuralError{"unknown Go type: <T>"}
//	  exactly like gc, instead of panicking.
//	Unmarshal: *RawValue (full TLV), *bool, *int..*int64, *[]byte,
//	  *string (Printable/UTF8/IA5/Numeric/T61/BMP strings), *BitString,
//	  *ObjectIdentifier, *Enumerated, *Flag.
//
// Structs, slices (other than []byte), maps, big.Int, time.Time and
// named types need reflect (or math/big, or wall-clock decomposition)
// and fail fast — fail fast, never silently. MarshalWithParams and
// UnmarshalWithParams need reflect for params interpretation and fail
// fast.
//
// Multi-result member Marshal keeps its exact gc signature with a
// fail-fast body for type-checking; its real implementation lives in
// the single-result __asn1Marshal helper, and the exporter desugars
// 2-target call sites to it (Appendix M pattern, mrdesugar_m5.go).
// Unmarshal has no desugar: decoding stores through the recipient
// pointer, which v1 cannot express (WS D) — every call site fails fast
// with the named M7 scope error. Any other call position fails fast
// with the named M7 scope error.
package main

// syntheticAsn1Source is the P3-M7 synthetic `encoding/asn1` package:
// exact gc 1.27.1 signatures for the in-scope API; real-Go DER core for
// the reflect-free subset; fail-fast bodies for the multi-result
// members (desugared) and the reflect-dependent surface.
const syntheticAsn1Source = `package asn1

const (
	TagBoolean         = 1
	TagInteger         = 2
	TagBitString       = 3
	TagOctetString     = 4
	TagNull            = 5
	TagOID             = 6
	TagEnum            = 10
	TagUTF8String      = 12
	TagSequence        = 16
	TagSet             = 17
	TagNumericString   = 18
	TagPrintableString = 19
	TagT61String       = 20
	TagIA5String       = 22
	TagUTCTime         = 23
	TagGeneralizedTime = 24
	TagGeneralString   = 27
	TagBMPString       = 30
)

const (
	ClassUniversal       = 0
	ClassApplication     = 1
	ClassContextSpecific = 2
	ClassPrivate         = 3
)

// StructuralError is returned when the ASN.1 data is structurally valid
// but unexpected.
type StructuralError struct {
	Msg string
}

func (e StructuralError) Error() string { return "asn1: structure error: " + e.Msg }

// SyntaxError is returned when the ASN.1 data is syntactically invalid.
type SyntaxError struct {
	Msg string
}

func (e SyntaxError) Error() string { return "asn1: syntax error: " + e.Msg }

// A BitString is the structure to use when you want an ASN.1 BIT STRING
// type.
type BitString struct {
	Bytes     []byte // bits packed into bytes.
	BitLength int    // length in bits.
}

// At returns the bit at the given index. If the index is out of range it
// returns 0.
func (b BitString) At(i int) int {
	if i < 0 || i >= b.BitLength {
		return 0
	}
	x := i / 8
	y := 7 - uint(i%8)
	return int(b.Bytes[x]>>y) & 1
}

// RightAlign returns a slice where the padding bits are at the
// beginning. The slice may share memory with the BitString.
func (b BitString) RightAlign() []byte {
	shift := uint(8 - (b.BitLength % 8))
	if shift == 8 || len(b.Bytes) == 0 {
		return b.Bytes
	}

	a := make([]byte, len(b.Bytes))
	a[0] = b.Bytes[0] >> shift
	for i := 1; i < len(b.Bytes); i++ {
		a[i] = b.Bytes[i-1] << (8 - shift)
		a[i] |= b.Bytes[i] >> shift
	}

	return a
}

// An ObjectIdentifier represents an ASN.1 OBJECT IDENTIFIER.
type ObjectIdentifier []int

// Equal reports whether oi and other represent the same identifier.
func (oi ObjectIdentifier) Equal(other ObjectIdentifier) bool {
	if len(oi) != len(other) {
		return false
	}
	for i := range oi {
		if oi[i] != other[i] {
			return false
		}
	}
	return true
}

// asn1FormatInt formats v in base 10 (local replacement for the
// strconv.AppendInt used by gc's ObjectIdentifier.String).
func asn1FormatInt(v int) string {
	neg := v < 0
	u := v
	if neg {
		u = -u
	}
	var arr [20]byte
	w := len(arr)
	if u == 0 {
		w--
		arr[w] = '0'
	} else {
		for u > 0 {
			w--
			arr[w] = byte(u%10) + '0'
			u /= 10
		}
	}
	if neg {
		w--
		arr[w] = '-'
	}
	return string(arr[w:])
}

func (oi ObjectIdentifier) String() string {
	var s []byte
	for i, v := range oi {
		if i > 0 {
			s = append(s, '.')
		}
		s = append(s, asn1FormatInt(v)...)
	}
	return string(s)
}

// An Enumerated is represented as a plain int.
type Enumerated int

// A Flag accepts any data and is set to true if present.
type Flag bool

// A RawValue represents an undecoded ASN.1 object.
type RawValue struct {
	Class, Tag int
	IsCompound bool
	Bytes      []byte
	FullBytes  []byte // includes the tag and length
}

// RawContent is used to signal that the undecoded, DER data needs to be
// preserved for a struct.
type RawContent []byte

// NullRawValue is a RawValue with its Tag set to the ASN.1 NULL type tag (5).
var NullRawValue = RawValue{Tag: TagNull}

// NullBytes contains bytes representing the DER-encoded ASN.1 NULL type.
var NullBytes = []byte{TagNull, 0}

// asn1ErrorString is the local errors.New replacement.
type asn1ErrorString struct {
	s string
}

func (e *asn1ErrorString) Error() string { return e.s }

// tagAndLength is the single-result carrier for asn1ParseTagAndLength
// (gc's parseTagAndLength returns (ret tagAndLength, offset int, err
// error); v1 cannot emit multi-result functions, WS D).
type tagAndLength struct {
	class, tag, length int
	isCompound         bool
	offset             int
	err                error
}

// asn1ParseTagAndLength parses an ASN.1 tag and length pair (gc's
// parseTagAndLength, restructured to single-result).
func asn1ParseTagAndLength(bytes []byte, initOffset int) tagAndLength {
	offset := initOffset
	var ret tagAndLength
	if offset >= len(bytes) {
		ret.err = &asn1ErrorString{s: "asn1: internal error in parseTagAndLength"}
		ret.offset = offset
		return ret
	}
	b := bytes[offset]
	offset++
	ret.class = int(b >> 6)
	ret.isCompound = b&0x20 == 0x20
	ret.tag = int(b & 0x1f)

	if ret.tag == 0x1f {
		br := asn1ParseBase128Int(bytes, offset)
		ret.tag, offset = br.value, br.offset
		if br.err != nil {
			ret.err = br.err
			ret.offset = offset
			return ret
		}
		if ret.tag < 0x1f {
			ret.err = SyntaxError{"non-minimal tag"}
			ret.offset = offset
			return ret
		}
	}
	if offset >= len(bytes) {
		ret.err = SyntaxError{"truncated tag or length"}
		ret.offset = offset
		return ret
	}
	b = bytes[offset]
	offset++
	if b&0x80 == 0 {
		ret.length = int(b & 0x7f)
	} else {
		numBytes := int(b & 0x7f)
		if numBytes == 0 {
			ret.err = SyntaxError{"indefinite length found (not DER)"}
			ret.offset = offset
			return ret
		}
		ret.length = 0
		for i := 0; i < numBytes; i++ {
			if offset >= len(bytes) {
				ret.err = SyntaxError{"truncated tag or length"}
				ret.offset = offset
				return ret
			}
			b = bytes[offset]
			offset++
			if ret.length >= 1<<23 {
				ret.err = StructuralError{"length too large"}
				ret.offset = offset
				return ret
			}
			ret.length <<= 8
			ret.length |= int(b)
			if ret.length == 0 {
				ret.err = StructuralError{"superfluous leading zeros in length"}
				ret.offset = offset
				return ret
			}
		}
		if ret.length < 0x80 {
			ret.err = StructuralError{"non-minimal length"}
			ret.offset = offset
			return ret
		}
	}
	ret.offset = offset
	return ret
}

// base128IntResult is the single-result carrier for
// asn1ParseBase128Int (gc's parseBase128Int returns 3 values).
type base128IntResult struct {
	value  int
	offset int
	err    error
}

// asn1ParseBase128Int parses a base-128 encoded int (gc's
// parseBase128Int, restructured to single-result).
func asn1ParseBase128Int(bytes []byte, initOffset int) base128IntResult {
	offset := initOffset
	var ret64 int64
	for shifted := 0; offset < len(bytes); shifted++ {
		if shifted == 5 {
			return base128IntResult{offset: offset, err: StructuralError{"base 128 integer too large"}}
		}
		ret64 <<= 7
		b := bytes[offset]
		if shifted == 0 && b == 0x80 {
			return base128IntResult{offset: offset, err: SyntaxError{"integer is not minimally encoded"}}
		}
		ret64 |= int64(b & 0x7f)
		offset++
		if b&0x80 == 0 {
			ret := int(ret64)
			if ret64 > 2147483647 {
				return base128IntResult{offset: offset, err: StructuralError{"base 128 integer too large"}}
			}
			return base128IntResult{value: ret, offset: offset}
		}
	}
	return base128IntResult{offset: offset, err: SyntaxError{"truncated base 128 integer"}}
}

// asn1CheckInteger returns nil if the given bytes are a valid
// DER-encoded INTEGER (gc's checkInteger).
func asn1CheckInteger(bytes []byte) error {
	if len(bytes) == 0 {
		return StructuralError{"empty integer"}
	}
	if len(bytes) == 1 {
		return nil
	}
	if (bytes[0] == 0 && bytes[1]&0x80 == 0) || (bytes[0] == 0xff && bytes[1]&0x80 == 0x80) {
		return StructuralError{"integer not minimally-encoded"}
	}
	return nil
}

// asn1ParseInt64 treats the given bytes as a big-endian, signed integer
// (gc's parseInt64, restructured to single-result via asn1Int64Result).
type asn1Int64Result struct {
	v   int64
	err error
}

func asn1ParseInt64(bytes []byte) asn1Int64Result {
	if err := asn1CheckInteger(bytes); err != nil {
		return asn1Int64Result{err: err}
	}
	if len(bytes) > 8 {
		return asn1Int64Result{err: StructuralError{"integer too large"}}
	}
	var ret int64
	for bytesRead := 0; bytesRead < len(bytes); bytesRead++ {
		ret <<= 8
		ret |= int64(bytes[bytesRead])
	}
	ret <<= 64 - uint8(len(bytes))*8
	ret >>= 64 - uint8(len(bytes))*8
	return asn1Int64Result{v: ret}
}

// asn1ParseInt32 treats the given bytes as a big-endian, signed 32-bit
// integer (gc's parseInt32).
func asn1ParseInt32(bytes []byte) asn1Int64Result {
	r := asn1ParseInt64(bytes)
	if r.err != nil {
		return r
	}
	if r.v != int64(int32(r.v)) {
		return asn1Int64Result{err: StructuralError{"integer too large"}}
	}
	return r
}

// asn1ParseBool parses a DER BOOLEAN (gc's parseBool).
func asn1ParseBool(bytes []byte) (bool, error) {
	if len(bytes) != 1 {
		return false, SyntaxError{"invalid boolean"}
	}
	switch bytes[0] {
	case 0:
		return false, nil
	case 0xff:
		return true, nil
	}
	return false, SyntaxError{"invalid boolean"}
}

// asn1ParseBitString parses an ASN.1 BIT STRING (gc's parseBitString).
func asn1ParseBitString(bytes []byte) (BitString, error) {
	var ret BitString
	if len(bytes) == 0 {
		return ret, SyntaxError{"zero length BIT STRING"}
	}
	paddingBits := int(bytes[0])
	if paddingBits > 7 ||
		len(bytes) == 1 && paddingBits > 0 ||
		bytes[len(bytes)-1]&((1<<bytes[0])-1) != 0 {
		return ret, SyntaxError{"invalid padding bits in BIT STRING"}
	}
	ret.BitLength = (len(bytes)-1)*8 - paddingBits
	ret.Bytes = bytes[1:]
	return ret, nil
}

// asn1ParseObjectIdentifier parses an OBJECT IDENTIFIER (gc's
// parseObjectIdentifier).
func asn1ParseObjectIdentifier(bytes []byte) (ObjectIdentifier, error) {
	if len(bytes) == 0 {
		return nil, SyntaxError{"zero length OBJECT IDENTIFIER"}
	}
	s := make([]int, len(bytes)+1)
	br := asn1ParseBase128Int(bytes, 0)
	if br.err != nil {
		return nil, br.err
	}
	v, offset := br.value, br.offset
	if v < 80 {
		s[0] = v / 40
		s[1] = v % 40
	} else {
		s[0] = 2
		s[1] = v - 80
	}
	i := 2
	for ; offset < len(bytes); i++ {
		br = asn1ParseBase128Int(bytes, offset)
		if br.err != nil {
			return nil, br.err
		}
		v, offset = br.value, br.offset
		s[i] = v
	}
	return s[0:i], nil
}

// asn1IsPrintable reports whether b is in the ASN.1 PrintableString
// set (gc's isPrintable).
func asn1IsPrintable(b byte, asterisk, ampersand bool) bool {
	return 'a' <= b && b <= 'z' ||
		'A' <= b && b <= 'Z' ||
		'0' <= b && b <= '9' ||
		'\'' <= b && b <= ')' ||
		'+' <= b && b <= '/' ||
		b == ' ' ||
		b == ':' ||
		b == '=' ||
		b == '?' ||
		(asterisk && b == '*') ||
		(ampersand && b == '&')
}

// asn1IsNumeric reports whether b is in the ASN.1 NumericString set.
func asn1IsNumeric(b byte) bool {
	return '0' <= b && b <= '9' || b == ' '
}

// asn1ParsePrintableString parses a PrintableString (gc's
// parsePrintableString).
func asn1ParsePrintableString(bytes []byte) (string, error) {
	for _, b := range bytes {
		if !asn1IsPrintable(b, true, true) {
			return "", SyntaxError{"PrintableString contains invalid character"}
		}
	}
	return string(bytes), nil
}

// asn1ParseNumericString parses a NumericString.
func asn1ParseNumericString(bytes []byte) (string, error) {
	for _, b := range bytes {
		if !asn1IsNumeric(b) {
			return "", SyntaxError{"NumericString contains invalid character"}
		}
	}
	return string(bytes), nil
}

// asn1ParseIA5String parses an IA5String.
func asn1ParseIA5String(bytes []byte) (string, error) {
	for _, b := range bytes {
		if b >= 0x80 {
			return "", SyntaxError{"IA5String contains invalid character"}
		}
	}
	return string(bytes), nil
}

// asn1ParseT61String parses a T61String (gc's parseT61String: Latin-1
// bytes mapped to UTF-8).
func asn1ParseT61String(bytes []byte) (string, error) {
	buf := make([]byte, 0, len(bytes))
	for _, v := range bytes {
		if v < 0x80 {
			buf = append(buf, v)
		} else {
			buf = append(buf, 0xc0|(v>>6), 0x80|(v&0x3f))
		}
	}
	return string(buf), nil
}

// asn1ValidUTF8 reports whether bytes is valid UTF-8 (the utf8.Valid
// replacement).
func asn1ValidUTF8(bytes []byte) bool {
	i := 0
	for i < len(bytes) {
		b := bytes[i]
		var n int
		var min uint32
		switch {
		case b < 0x80:
			i++
			continue
		case b < 0xc2:
			return false
		case b < 0xe0:
			n, min = 1, 0x80
		case b < 0xf0:
			n, min = 2, 0x800
		case b < 0xf5:
			n, min = 3, 0x10000
		default:
			return false
		}
		if i+n >= len(bytes) {
			return false
		}
		v := uint32(b & (0x7f >> (n + 1)))
		for j := 1; j <= n; j++ {
			c := bytes[i+j]
			if c < 0x80 || c >= 0xc0 {
				return false
			}
			v = v<<6 | uint32(c&0x3f)
		}
		if v < min || (v >= 0xd800 && v <= 0xdfff) || v > 0x10ffff {
			return false
		}
		i += n + 1
	}
	return true
}

// asn1ParseUTF8String parses a UTF8String.
func asn1ParseUTF8String(bytes []byte) (string, error) {
	if !asn1ValidUTF8(bytes) {
		return "", &asn1ErrorString{s: "asn1: invalid UTF-8 string"}
	}
	return string(bytes), nil
}

// asn1ParseBMPString parses a BMPString (gc's parseBMPString: UCS-2
// with UTF-16 validation, mapped to UTF-8).
func asn1ParseBMPString(bmpString []byte) (string, error) {
	if len(bmpString)%2 != 0 {
		return "", &asn1ErrorString{s: "invalid BMPString"}
	}
	if l := len(bmpString); l >= 2 && bmpString[l-1] == 0 && bmpString[l-2] == 0 {
		bmpString = bmpString[:l-2]
	}
	buf := make([]byte, 0, len(bmpString))
	for len(bmpString) > 0 {
		point := uint16(bmpString[0])<<8 + uint16(bmpString[1])
		if point == 0xfffe || point == 0xffff ||
			(point >= 0xfdd0 && point <= 0xfdef) ||
			(point >= 0xd800 && point <= 0xdfff) {
			return "", &asn1ErrorString{s: "invalid BMPString"}
		}
		r := rune(point)
		if r < 0x80 {
			buf = append(buf, byte(r))
		} else if r < 0x800 {
			buf = append(buf, 0xc0|byte(r>>6), 0x80|byte(r&0x3f))
		} else {
			buf = append(buf, 0xe0|byte(r>>12), 0x80|byte((r>>6)&0x3f), 0x80|byte(r&0x3f))
		}
		bmpString = bmpString[2:]
	}
	return string(buf), nil
}

// asn1AppendLength appends a DER length (gc's appendLength).
func asn1AppendLength(dst []byte, i int) []byte {
	n := 0
	for j := i; j > 0; j >>= 8 {
		n++
	}
	if i < 128 && n <= 1 {
		return append(dst, byte(i))
	}
	dst = append(dst, 0x80|byte(n))
	for j := n - 1; j >= 0; j-- {
		dst = append(dst, byte(i>>uint(j*8)))
	}
	return dst
}

// asn1AppendTagAndLength appends a DER tag and length (gc's
// appendTagAndLength).
func asn1AppendTagAndLength(dst []byte, class, tag, length int, isCompound bool) []byte {
	b := byte(class<<6)
	if isCompound {
		b |= 0x20
	}
	if tag >= 31 {
		b |= 0x1f
		dst = append(dst, b)
		dst = asn1AppendBase128Int(dst, tag)
	} else {
		b |= byte(tag)
		dst = append(dst, b)
	}
	return asn1AppendLength(dst, length)
}

// asn1Base128IntLength returns the encoded length of n (gc's
// base128IntLength).
func asn1Base128IntLength(n int64) int {
	if n == 0 {
		return 1
	}
	l := 0
	for i := n; i > 0; i >>= 7 {
		l++
	}
	return l
}

// asn1AppendBase128Int appends a base-128 int (gc's appendBase128Int).
func asn1AppendBase128Int(dst []byte, n int) []byte {
	l := asn1Base128IntLength(int64(n))
	for i := l - 1; i >= 0; i-- {
		o := byte(int64(n) >> uint(i*7))
		o &= 0x7f
		if i != 0 {
			o |= 0x80
		}
		dst = append(dst, o)
	}
	return dst
}

// asn1EncodeInt64 encodes v as a DER INTEGER body (gc's int64Encoder).
func asn1EncodeInt64(dst []byte, v int64) []byte {
	n := 1
	for i := v; i > 127; i >>= 8 {
		n++
	}
	for i := v; i < -128; i >>= 8 {
		n++
	}
	for j := 0; j < n; j++ {
		dst = append(dst, byte(v>>uint((n-1-j)*8)))
	}
	return dst
}

// asn1MarshalPair is the Appendix M single-result carrier for Marshal.
type asn1MarshalPair struct {
	b   []byte
	err error
}

// asn1Tagged appends the tag+length prefix for class/tag/bodyLen.
func asn1Tagged(class, tag int, body []byte) []byte {
	out := asn1AppendTagAndLength(nil, class, tag, len(body), false)
	return append(out, body...)
}

// __asn1Marshal is the Appendix M desugar target behind Marshal: the
// DER encoder for the reflect-free subset (gc 1.27.1 semantics for
// these types).
func __asn1Marshal(val any) asn1MarshalPair {
	switch v := val.(type) {
	case nil:
		return asn1MarshalPair{err: &asn1ErrorString{s: "asn1: cannot marshal nil value"}}
	case RawValue:
		if len(v.FullBytes) != 0 {
			return asn1MarshalPair{b: v.FullBytes}
		}
		return asn1MarshalPair{b: asn1Tagged(v.Class, v.Tag, v.Bytes)}
	case RawContent:
		// At top level gc treats RawContent as []byte (OCTET STRING);
		// the struct-preservation semantics need reflect.
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagOctetString, []byte(v))}
	case bool:
		if v {
			return asn1MarshalPair{b: []byte{TagBoolean, 1, 0xff}}
		}
		return asn1MarshalPair{b: []byte{TagBoolean, 1, 0}}
	case Flag:
		return asn1MarshalPair{b: []byte{TagBoolean, 0}}
	case int:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagInteger, asn1EncodeInt64(nil, int64(v)))}
	case int8:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagInteger, asn1EncodeInt64(nil, int64(v)))}
	case int16:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagInteger, asn1EncodeInt64(nil, int64(v)))}
	case int32:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagInteger, asn1EncodeInt64(nil, int64(v)))}
	case int64:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagInteger, asn1EncodeInt64(nil, v))}
	case []byte:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagOctetString, v)}
	case string:
		printable := true
		for i := 0; i < len(v); i++ {
			if v[i] >= 0x80 || !asn1IsPrintable(v[i], false, false) {
				printable = false
				break
			}
		}
		if printable {
			return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagPrintableString, []byte(v))}
		}
		if !asn1ValidUTF8([]byte(v)) {
			return asn1MarshalPair{err: &asn1ErrorString{s: "asn1: string not valid UTF-8"}}
		}
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagUTF8String, []byte(v))}
	case BitString:
		body := make([]byte, 0, len(v.Bytes)+1)
		body = append(body, byte((8-v.BitLength%8)%8))
		body = append(body, v.Bytes...)
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagBitString, body)}
	case ObjectIdentifier:
		if len(v) < 2 || v[0] > 2 || (v[0] < 2 && v[1] >= 40) {
			return asn1MarshalPair{err: StructuralError{"invalid object identifier"}}
		}
		body := asn1AppendBase128Int(nil, v[0]*40+v[1])
		for i := 2; i < len(v); i++ {
			body = asn1AppendBase128Int(body, v[i])
		}
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagOID, body)}
	case Enumerated:
		return asn1MarshalPair{b: asn1Tagged(ClassUniversal, TagEnum, asn1EncodeInt64(nil, int64(v)))}
	case float32:
		// gc rejects these scalar types without needing reflect:
		// StructuralError{"unknown Go type: <T>"}.
		return asn1MarshalPair{err: StructuralError{"unknown Go type: float32"}}
	case float64:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: float64"}}
	case complex64:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: complex64"}}
	case complex128:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: complex128"}}
	case uint:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uint"}}
	case uint8:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uint8"}}
	case uint16:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uint16"}}
	case uint32:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uint32"}}
	case uint64:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uint64"}}
	case uintptr:
		return asn1MarshalPair{err: StructuralError{"unknown Go type: uintptr"}}
	}
	panic("encoding/asn1: Marshal: not in M7 scope (needs reflect for composite types, big.Int, time.Time and named types; re-test when reflect lands)")
}

// asn1ExpectTag parses one TLV and checks class/tag/compound, returning
// the inner bytes and the offset past the TLV.
type asn1TLV struct {
	inner  []byte
	offset int
	err    error
}

func asn1ReadTLV(b []byte, class, tag int, compound bool) asn1TLV {
	t := asn1ParseTagAndLength(b, 0)
	if t.err != nil {
		return asn1TLV{err: t.err}
	}
	if t.class != class || t.tag != tag || t.isCompound != compound {
		return asn1TLV{err: StructuralError{"tags don't match"}}
	}
	if t.offset+t.length > len(b) {
		return asn1TLV{err: SyntaxError{"data truncated"}}
	}
	return asn1TLV{inner: b[t.offset : t.offset+t.length], offset: t.offset + t.length}
}

// Marshal keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Marshal(val any) ([]byte, error) {
	panic("encoding/asn1: Marshal: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// Unmarshal keeps the exact gc 1.27.1 signature for type-checking.
// M7 scope: fail-fast body. Decoding requires storing through the
// recipient pointer (*p = v), which v1 cannot express (WS D: no
// *ast.StarExpr assignment) — so unlike Marshal there is no v1-spellable
// __-helper and no Appendix M desugar; every call site fails fast with
// the named M7 scope error. Re-test when WS D lands (the DER parsing
// core — asn1Parse* helpers — is retained in this source for that day).
func Unmarshal(b []byte, val any) (rest []byte, err error) {
	panic("encoding/asn1: Unmarshal: not in M7 scope (pointer store needs WS D; re-test when WS D lands)")
}

// MarshalWithParams keeps the exact gc 1.27.1 signature for
// type-checking. A real implementation needs reflect (params
// interpretation for struct fields) — unavailable to v1 synthetic
// packages. Body fails fast; re-test when reflect lands.
func MarshalWithParams(val any, params string) ([]byte, error) {
	panic("encoding/asn1: MarshalWithParams: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// UnmarshalWithParams keeps the exact gc 1.27.1 signature for
// type-checking. A real implementation needs reflect (params
// interpretation for struct fields) — unavailable to v1 synthetic
// packages. Body fails fast; re-test when reflect lands.
func UnmarshalWithParams(b []byte, val any, params string) (rest []byte, err error) {
	panic("encoding/asn1: UnmarshalWithParams: not in M7 scope (needs reflect; re-test when reflect lands)")
}
`
