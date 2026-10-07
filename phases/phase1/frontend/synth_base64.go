// P3-M7 (wave 1): synthetic `encoding/base64` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/base64")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (base64.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): Encode, EncodeToString,
// EncodedLen, DecodedLen, AppendEncode, NewEncoding, WithPadding,
// Strict, CorruptInputError.Error are real Go, adapted from the gc
// 1.27.1 implementation (io-free; the streaming encoder/decoder types,
// NewEncoder/NewDecoder and newlineFilteringReader are omitted — see
// below). The strconv.IntSize/byteorder/slices helpers used by gc are
// replaced with local equivalents (intSize64 const, manual big-endian
// stores, base64Grow).
//
// Multi-result members (Decode, DecodeString, AppendDecode) keep exact
// gc signatures with fail-fast bodies for type-checking; their real
// implementations live in the single-result __base64Decode,
// __base64DecodeString and __base64AppendDecode helpers, and the
// exporter desugars N-target call sites to them (Appendix M pattern,
// mrdesugar_m7.go). Any other call position fails fast with the named
// M7 scope error.
//
// Omitted (io is not in M7 scope — NOT OK per the wave-1 import rules,
// so the exact gc signatures cannot even be declared):
//   func NewEncoder(enc *Encoding, w io.Writer) io.WriteCloser
//   func NewDecoder(enc *Encoding, r io.Reader) io.Reader
// Re-test trigger: when io lands (P3-M8), declare these with exact gc
// signatures and implement the streaming encoder/decoder over the
// synthetic Encode/decode core. This omission is deliberate and loud —
// never silent (the p3frontend drift test asserts the omission list).
package main

// syntheticBase64Source is the P3-M7 synthetic `encoding/base64`
// package: exact gc 1.27.1 signatures for the in-scope API; real-Go
// encode/decode core; fail-fast bodies for the multi-result members
// (desugared) and the io-dependent streaming API (omitted).
const syntheticBase64Source = `package base64

// Encoding is a radix 64 encoding/decoding scheme, defined by a
// 64-character alphabet.
type Encoding struct {
	encode    [64]byte   // mapping of symbol index to symbol byte value
	decodeMap [256]uint8 // mapping of symbol byte value to symbol index
	padChar   rune
	strict    bool
}

const (
	StdPadding rune = '=' // Standard padding character
	NoPadding  rune = -1  // No padding
)

const (
	base64DecodeMapInitialize = "" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"
	base64InvalidIndex = '\xff'
)

// NewEncoding returns a new padded Encoding defined by the given alphabet,
// which must be a 64-byte string that contains unique byte values and
// does not contain the padding character or CR / LF ('\r', '\n').
func NewEncoding(encoder string) *Encoding {
	if len(encoder) != 64 {
		panic("encoding alphabet is not 64-bytes long")
	}

	e := new(Encoding)
	e.padChar = StdPadding
	copy(e.encode[:], encoder)
	copy(e.decodeMap[:], base64DecodeMapInitialize)

	for i := 0; i < len(encoder); i++ {
		switch {
		case encoder[i] == '\n' || encoder[i] == '\r':
			panic("encoding alphabet contains newline character")
		case e.decodeMap[encoder[i]] != base64InvalidIndex:
			panic("encoding alphabet includes duplicate symbols")
		}
		e.decodeMap[encoder[i]] = uint8(i)
	}
	return e
}

// WithPadding creates a new encoding identical to enc except
// with a specified padding character, or NoPadding to disable padding.
func (enc Encoding) WithPadding(padding rune) *Encoding {
	switch {
	case padding < NoPadding || padding == '\r' || padding == '\n' || padding > 0xff:
		panic("invalid padding")
	case padding != NoPadding && enc.decodeMap[byte(padding)] != base64InvalidIndex:
		panic("padding contained in alphabet")
	}
	enc.padChar = padding
	// NB: v1 cannot spell &enc (WS D); new(Encoding) + field copies is
	// identical semantics (enc is already a copy — value receiver).
	c := new(Encoding)
	c.encode = enc.encode
	c.decodeMap = enc.decodeMap
	c.padChar = enc.padChar
	c.strict = enc.strict
	return c
}

// Strict creates a new encoding identical to enc except with
// strict decoding enabled.
func (enc Encoding) Strict() *Encoding {
	// NB: v1 cannot spell &enc (WS D); new(Encoding) + field copies is
	// identical semantics (enc is already a copy — value receiver).
	c := new(Encoding)
	c.encode = enc.encode
	c.decodeMap = enc.decodeMap
	c.padChar = enc.padChar
	c.strict = true
	return c
}

// StdEncoding is the standard base64 encoding, as defined in RFC 4648.
var StdEncoding = NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/")

// URLEncoding is the alternate base64 encoding defined in RFC 4648.
var URLEncoding = NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")

// base64NoPadding returns a copy of enc with padding disabled — exactly
// WithPadding(NoPadding)'s semantics (NoPadding skips both validation
// branches in WithPadding), spelled with a pointer receiver. The package
// vars below cannot call the value-receiver WithPadding on *Encoding:
// v1 has no implicit method-receiver deref (WS D) and *ast.StarExpr is
// not expressible in v1 scope.
func (enc *Encoding) base64NoPadding() *Encoding {
	c := new(Encoding)
	c.encode = enc.encode
	c.decodeMap = enc.decodeMap
	c.padChar = NoPadding
	c.strict = enc.strict
	return c
}

// RawStdEncoding is the standard raw, unpadded base64 encoding.
var RawStdEncoding = StdEncoding.base64NoPadding()

// RawURLEncoding is the unpadded alternate base64 encoding defined in RFC 4648.
var RawURLEncoding = URLEncoding.base64NoPadding()

// Encode encodes src using the encoding enc,
// writing EncodedLen(len(src)) bytes to dst.
func (enc *Encoding) Encode(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	_ = enc.encode

	for len(src) >= 3 {
		val := uint(src[0])<<16 | uint(src[1])<<8 | uint(src[2])

		_ = dst[3]
		dst[0] = enc.encode[val>>18&0x3F]
		dst[1] = enc.encode[val>>12&0x3F]
		dst[2] = enc.encode[val>>6&0x3F]
		dst[3] = enc.encode[val&0x3F]

		src = src[3:]
		dst = dst[4:]
	}

	switch len(src) {
	case 0:
		return
	case 1:
		val := uint(src[0]) << 16
		dst[0] = enc.encode[val>>18&0x3F]
		dst[1] = enc.encode[val>>12&0x3F]
		if enc.padChar != NoPadding {
			dst[2] = byte(enc.padChar)
			dst[3] = byte(enc.padChar)
		}
	case 2:
		val := uint(src[0])<<16 | uint(src[1])<<8
		dst[0] = enc.encode[val>>18&0x3F]
		dst[1] = enc.encode[val>>12&0x3F]
		dst[2] = enc.encode[val>>6&0x3F]
		if enc.padChar != NoPadding {
			dst[3] = byte(enc.padChar)
		}
	}
}

// base64Grow is the slices.Grow replacement (v1 synthetic scope has no
// slices package).
func base64Grow(dst []byte, n int) []byte {
	if n <= cap(dst)-len(dst) {
		return dst
	}
	ndst := make([]byte, len(dst), 2*cap(dst)+n)
	copy(ndst, dst)
	return ndst
}

// AppendEncode appends the base64 encoded src to dst
// and returns the extended buffer.
func (enc *Encoding) AppendEncode(dst, src []byte) []byte {
	n := enc.EncodedLen(len(src))
	dst = base64Grow(dst, n)
	enc.Encode(dst[len(dst):][:n], src)
	return dst[:len(dst)+n]
}

// EncodeToString returns the base64 encoding of src.
func (enc *Encoding) EncodeToString(src []byte) string {
	buf := make([]byte, enc.EncodedLen(len(src)))
	enc.Encode(buf, src)
	return string(buf)
}

// EncodedLen returns the length in bytes of the base64 encoding of an
// input buffer of length n.
func (enc *Encoding) EncodedLen(n int) int {
	if enc.padChar == NoPadding {
		return n/3*4 + (n%3*8+5)/6
	}
	return (n + 2) / 3 * 4
}

// CorruptInputError is returned when the input is invalid.
type CorruptInputError int64

func (e CorruptInputError) Error() string {
	return "illegal base64 data at input byte " + base64FormatInt(int64(e))
}

// base64FormatInt formats v in base 10 (the strconv.FormatInt
// replacement: v1 synthetic scope has no strconv package).
func base64FormatInt(v int64) string {
	neg := v < 0
	var u uint64
	if neg {
		u = uint64(-(v + 1)) + 1
	} else {
		u = uint64(v)
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

// base64QuantumResult is the single-result carrier for decodeQuantum
// (gc returns (nsi, n int, err error); v1 cannot emit multi-result
// functions, WS D).
type base64QuantumResult struct {
	nsi int
	n   int
	err error
}

// decodeQuantum decodes up to 4 base64 bytes from src[si:].
func (enc *Encoding) decodeQuantum(dst, src []byte, si int) base64QuantumResult {
	var dbuf [4]byte
	dlen := 4
	var perr error

	_ = enc.decodeMap

	for j := 0; j < len(dbuf); j++ {
		if len(src) == si {
			switch {
			case j == 0:
				return base64QuantumResult{nsi: si}
			case j == 1, enc.padChar != NoPadding:
				return base64QuantumResult{nsi: si, err: CorruptInputError(si - j)}
			}
			dlen = j
			break
		}
		in := src[si]
		si++

		out := enc.decodeMap[in]
		if out != 0xff {
			dbuf[j] = out
			continue
		}

		if in == '\n' || in == '\r' {
			j--
			continue
		}

		if rune(in) != enc.padChar {
			return base64QuantumResult{nsi: si, err: CorruptInputError(si - 1)}
		}

		switch j {
		case 0, 1:
			return base64QuantumResult{nsi: si, err: CorruptInputError(si - 1)}
		case 2:
			for si < len(src) && (src[si] == '\n' || src[si] == '\r') {
				si++
			}
			if si == len(src) {
				return base64QuantumResult{nsi: si, err: CorruptInputError(len(src))}
			}
			if rune(src[si]) != enc.padChar {
				return base64QuantumResult{nsi: si, err: CorruptInputError(si - 1)}
			}
			si++
		}

		for si < len(src) && (src[si] == '\n' || src[si] == '\r') {
			si++
		}
		if si < len(src) {
			perr = CorruptInputError(si)
		}
		dlen = j
		break
	}

	val := uint(dbuf[0])<<18 | uint(dbuf[1])<<12 | uint(dbuf[2])<<6 | uint(dbuf[3])
	dbuf[2], dbuf[1], dbuf[0] = byte(val>>0), byte(val>>8), byte(val>>16)
	switch dlen {
	case 4:
		dst[2] = dbuf[2]
		dbuf[2] = 0
		fallthrough
	case 3:
		dst[1] = dbuf[1]
		if enc.strict && dbuf[2] != 0 {
			return base64QuantumResult{nsi: si, err: CorruptInputError(si - 1)}
		}
		dbuf[1] = 0
		fallthrough
	case 2:
		dst[0] = dbuf[0]
		if enc.strict && (dbuf[1] != 0 || dbuf[2] != 0) {
			return base64QuantumResult{nsi: si, err: CorruptInputError(si - 2)}
		}
	}

	return base64QuantumResult{nsi: si, n: dlen - 1, err: perr}
}

// base64Assemble32Result is the single-result carrier for assemble32.
type base64Assemble32Result struct {
	dn uint32
	ok bool
}

// assemble32 assembles 4 base64 digits into 3 bytes.
func base64Assemble32(n1, n2, n3, n4 byte) base64Assemble32Result {
	if n1|n2|n3|n4 == 0xff {
		return base64Assemble32Result{}
	}
	return base64Assemble32Result{
		dn: uint32(n1)<<26 |
			uint32(n2)<<20 |
			uint32(n3)<<14 |
			uint32(n4)<<8,
		ok: true,
	}
}

// base64Assemble64Result is the single-result carrier for assemble64.
type base64Assemble64Result struct {
	dn uint64
	ok bool
}

// assemble64 assembles 8 base64 digits into 6 bytes.
func base64Assemble64(n1, n2, n3, n4, n5, n6, n7, n8 byte) base64Assemble64Result {
	if n1|n2|n3|n4|n5|n6|n7|n8 == 0xff {
		return base64Assemble64Result{}
	}
	return base64Assemble64Result{
		dn: uint64(n1)<<58 |
			uint64(n2)<<52 |
			uint64(n3)<<46 |
			uint64(n4)<<40 |
			uint64(n5)<<34 |
			uint64(n6)<<28 |
			uint64(n7)<<22 |
			uint64(n8)<<16,
		ok: true,
	}
}

// base64DecodePair is the Appendix M single-result carrier for Decode.
type base64DecodePair struct {
	n   int
	err error
}

// base64IntSize64 reports whether ints are 64 bits (the
// strconv.IntSize >= 64 replacement: v1 synthetic scope has no strconv
// package; the expression is gc's strconv.IntSize definition).
var base64IntSize64 = 32<<(^uint(0)>>63) >= 64

// base64PutU64BE stores dn in dst in big-endian order (the
// internal/byteorder.BEPutUint64 replacement).
func base64PutU64BE(dst []byte, dn uint64) {
	dst[0] = byte(dn >> 56)
	dst[1] = byte(dn >> 48)
	dst[2] = byte(dn >> 40)
	dst[3] = byte(dn >> 32)
	dst[4] = byte(dn >> 24)
	dst[5] = byte(dn >> 16)
	dst[6] = byte(dn >> 8)
	dst[7] = byte(dn)
}

// base64PutU32BE stores dn in dst in big-endian order.
func base64PutU32BE(dst []byte, dn uint32) {
	dst[0] = byte(dn >> 24)
	dst[1] = byte(dn >> 16)
	dst[2] = byte(dn >> 8)
	dst[3] = byte(dn)
}

// __base64DecodeCore is the real decode loop (gc 1.27.1's Decode body,
// restructured to single-result).
func __base64DecodeCore(enc *Encoding, dst, src []byte) base64DecodePair {
	if len(src) == 0 {
		return base64DecodePair{}
	}
	_ = enc.decodeMap

	n := 0
	si := 0
	var err error
	for base64IntSize64 && len(src)-si >= 8 && len(dst)-n >= 8 {
		src2 := src[si : si+8]
		ar := base64Assemble64(
			enc.decodeMap[src2[0]],
			enc.decodeMap[src2[1]],
			enc.decodeMap[src2[2]],
			enc.decodeMap[src2[3]],
			enc.decodeMap[src2[4]],
			enc.decodeMap[src2[5]],
			enc.decodeMap[src2[6]],
			enc.decodeMap[src2[7]],
		)
		if ar.ok {
			base64PutU64BE(dst[n:], ar.dn)
			n += 6
			si += 8
		} else {
			qr := enc.decodeQuantum(dst[n:], src, si)
			si = qr.nsi
			n += qr.n
			err = qr.err
			if err != nil {
				return base64DecodePair{n: n, err: err}
			}
		}
	}

	for len(src)-si >= 4 && len(dst)-n >= 4 {
		src2 := src[si : si+4]
		ar := base64Assemble32(
			enc.decodeMap[src2[0]],
			enc.decodeMap[src2[1]],
			enc.decodeMap[src2[2]],
			enc.decodeMap[src2[3]],
		)
		if ar.ok {
			base64PutU32BE(dst[n:], ar.dn)
			n += 3
			si += 4
		} else {
			qr := enc.decodeQuantum(dst[n:], src, si)
			si = qr.nsi
			n += qr.n
			err = qr.err
			if err != nil {
				return base64DecodePair{n: n, err: err}
			}
		}
	}

	for si < len(src) {
		qr := enc.decodeQuantum(dst[n:], src, si)
		si = qr.nsi
		n += qr.n
		err = qr.err
		if err != nil {
			return base64DecodePair{n: n, err: err}
		}
	}
	return base64DecodePair{n: n, err: err}
}

// __base64Decode is the Appendix M desugar target behind
// (*Encoding).Decode.
func __base64Decode(enc *Encoding, dst, src []byte) base64DecodePair {
	return __base64DecodeCore(enc, dst, src)
}

// base64DecodeStringPair is the Appendix M single-result carrier for
// DecodeString and AppendDecode.
type base64DecodeStringPair struct {
	b   []byte
	err error
}

// __base64DecodeString is the Appendix M desugar target behind
// (*Encoding).DecodeString.
func __base64DecodeString(enc *Encoding, s string) base64DecodeStringPair {
	dbuf := make([]byte, enc.DecodedLen(len(s)))
	pr := __base64Decode(enc, dbuf, []byte(s))
	return base64DecodeStringPair{b: dbuf[:pr.n], err: pr.err}
}

// base64DecodedLen is gc's decodedLen.
func base64DecodedLen(n int, padChar rune) int {
	if padChar == NoPadding {
		return n/4*3 + n%4*6/8
	}
	return n / 4 * 3
}

// __base64AppendDecode is the Appendix M desugar target behind
// (*Encoding).AppendDecode.
func __base64AppendDecode(enc *Encoding, dst, src []byte) base64DecodeStringPair {
	n := len(src)
	for n > 0 && rune(src[n-1]) == enc.padChar {
		n--
	}
	n = base64DecodedLen(n, NoPadding)

	dst = base64Grow(dst, n)
	pr := __base64Decode(enc, dst[len(dst):][:n], src)
	return base64DecodeStringPair{b: dst[:len(dst)+pr.n], err: pr.err}
}

// Decode keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func (enc *Encoding) Decode(dst, src []byte) (n int, err error) {
	panic("encoding/base64: Decode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// DecodeString keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func (enc *Encoding) DecodeString(s string) ([]byte, error) {
	panic("encoding/base64: DecodeString: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// AppendDecode keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func (enc *Encoding) AppendDecode(dst, src []byte) ([]byte, error) {
	panic("encoding/base64: AppendDecode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of base64-encoded data.
func (enc *Encoding) DecodedLen(n int) int {
	return base64DecodedLen(n, enc.padChar)
}
`
