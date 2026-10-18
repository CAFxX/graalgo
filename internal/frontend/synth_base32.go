// SYNTHETIC (Principle 4) — encoding/base32: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M7 (wave 1): synthetic `encoding/base32` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/base32")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (base32.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): Encode, EncodeToString,
// EncodedLen, DecodedLen, AppendEncode, NewEncoding, WithPadding,
// CorruptInputError.Error, NewEncoder, NewDecoder are real Go, adapted
// from the gc 1.27.1 implementation. The streaming encoder/decoder
// Read/Write/Close stay fail-fast (they need r.Read/w.Write — 2-result
// interface calls, WS D); readEncodedData and newlineFilteringReader are
// unexported helpers of those paths and stay omitted with them.
//
// Multi-result members (Decode, DecodeString, AppendDecode) keep exact
// gc signatures with fail-fast bodies for type-checking; their real
// implementations live in the single-result __base32Decode,
// __base32DecodeString and __base32AppendDecode helpers, and the
// exporter desugars N-target call sites to them (Appendix M pattern,
// mrdesugar_m7.go). Any other call position fails fast with the named
// M7 scope error.
//
// P3-M8: NewEncoder/NewDecoder and the encoder/decoder types are declared
// with exact gc signatures (io landed, so the signatures are nameable).
// The constructors are real; the streaming Read/Write/Close methods fail
// fast with the named M8/WS-D error (re-test when WS D lands multi-result
// calls). The p3frontend drift test pins the declared surface.
package main

// syntheticBase32Source is the P3-M7 synthetic `encoding/base32`
// package: exact gc 1.27.1 signatures for the in-scope API; real-Go
// encode/decode core; fail-fast bodies for the multi-result members
// (desugared) and for the streaming encoder/decoder Read/Write/Close
// (P3-M8: declared with exact gc signatures; need 2-result interface
// dispatch — WS D).
const syntheticBase32Source = `package base32

import "io"

// Encoding is a radix 32 encoding/decoding scheme, defined by a
// 32-character alphabet.
type Encoding struct {
	encode    [32]byte   // mapping of symbol index to symbol byte value
	decodeMap [256]uint8 // mapping of symbol byte value to symbol index
	padChar   rune
}

const (
	StdPadding rune = '=' // Standard padding character
	NoPadding  rune = -1  // No padding
)

const (
	base32DecodeMapInitialize = "" +
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
	base32InvalidIndex = '\xff'
)

// NewEncoding returns a new padded Encoding defined by the given alphabet,
// which must be a 32-byte string that contains unique byte values and
// does not contain the padding character or CR / LF ('\r', '\n').
func NewEncoding(encoder string) *Encoding {
	if len(encoder) != 32 {
		panic("encoding alphabet is not 32-bytes long")
	}

	e := new(Encoding)
	e.padChar = StdPadding
	copy(e.encode[:], encoder)
	copy(e.decodeMap[:], base32DecodeMapInitialize)

	for i := 0; i < len(encoder); i++ {
		switch {
		case encoder[i] == '\n' || encoder[i] == '\r':
			panic("encoding alphabet contains newline character")
		case e.decodeMap[encoder[i]] != base32InvalidIndex:
			panic("encoding alphabet includes duplicate symbols")
		}
		e.decodeMap[encoder[i]] = uint8(i)
	}
	return e
}

// StdEncoding is the standard base32 encoding, as defined in RFC 4648.
var StdEncoding = NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")

// HexEncoding is the "Extended Hex Alphabet" defined in RFC 4648.
var HexEncoding = NewEncoding("0123456789ABCDEFGHIJKLMNOPQRSTUV")

// WithPadding creates a new encoding identical to enc except
// with a specified padding character, or NoPadding to disable padding.
func (enc Encoding) WithPadding(padding rune) *Encoding {
	switch {
	case padding < NoPadding || padding == '\r' || padding == '\n' || padding > 0xff:
		panic("invalid padding")
	case padding != NoPadding && enc.decodeMap[byte(padding)] != base32InvalidIndex:
		panic("padding contained in alphabet")
	}
	enc.padChar = padding
	// NB: v1 cannot spell &enc (WS D); new(Encoding) + field copies is
	// identical semantics (enc is already a copy — value receiver).
	c := new(Encoding)
	c.encode = enc.encode
	c.decodeMap = enc.decodeMap
	c.padChar = enc.padChar
	return c
}

// Encode encodes src using the encoding enc,
// writing EncodedLen(len(src)) bytes to dst.
func (enc *Encoding) Encode(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	_ = enc.encode

	for len(src) >= 5 {
		hi := uint32(src[0])<<24 | uint32(src[1])<<16 | uint32(src[2])<<8 | uint32(src[3])
		lo := hi<<8 | uint32(src[4])

		_ = dst[7]
		dst[0] = enc.encode[(hi>>27)&0x1F]
		dst[1] = enc.encode[(hi>>22)&0x1F]
		dst[2] = enc.encode[(hi>>17)&0x1F]
		dst[3] = enc.encode[(hi>>12)&0x1F]
		dst[4] = enc.encode[(hi>>7)&0x1F]
		dst[5] = enc.encode[(hi>>2)&0x1F]
		dst[6] = enc.encode[(lo>>5)&0x1F]
		dst[7] = enc.encode[(lo)&0x1F]

		src = src[5:]
		dst = dst[8:]
	}

	if len(src) == 0 {
		return
	}

	val := uint32(0)
	switch len(src) {
	case 4:
		val |= uint32(src[3])
		dst[6] = enc.encode[val<<3&0x1F]
		dst[5] = enc.encode[val>>2&0x1F]
		fallthrough
	case 3:
		val |= uint32(src[2]) << 8
		dst[4] = enc.encode[val>>7&0x1F]
		fallthrough
	case 2:
		val |= uint32(src[1]) << 16
		dst[3] = enc.encode[val>>12&0x1F]
		dst[2] = enc.encode[val>>17&0x1F]
		fallthrough
	case 1:
		val |= uint32(src[0]) << 24
		dst[1] = enc.encode[val>>22&0x1F]
		dst[0] = enc.encode[val>>27&0x1F]
	}

	if enc.padChar != NoPadding {
		nPad := (len(src)*8/5) + 1
		for i := nPad; i < 8; i++ {
			dst[i] = byte(enc.padChar)
		}
	}
}

// base32Grow appends n zero bytes' worth of capacity to dst (the
// slices.Grow replacement: v1 synthetic scope has no slices package).
func base32Grow(dst []byte, n int) []byte {
	if n <= cap(dst)-len(dst) {
		return dst
	}
	ndst := make([]byte, len(dst), 2*cap(dst)+n)
	copy(ndst, dst)
	return ndst
}

// AppendEncode appends the base32 encoded src to dst
// and returns the extended buffer.
func (enc *Encoding) AppendEncode(dst, src []byte) []byte {
	n := enc.EncodedLen(len(src))
	dst = base32Grow(dst, n)
	enc.Encode(dst[len(dst):][:n], src)
	return dst[:len(dst)+n]
}

// EncodeToString returns the base32 encoding of src.
func (enc *Encoding) EncodeToString(src []byte) string {
	buf := make([]byte, enc.EncodedLen(len(src)))
	enc.Encode(buf, src)
	return string(buf)
}

// EncodedLen returns the length in bytes of the base32 encoding
// of an input buffer of length n.
func (enc *Encoding) EncodedLen(n int) int {
	if enc.padChar == NoPadding {
		return n/5*8 + (n%5*8+4)/5
	}
	return (n + 4) / 5 * 8
}

// ---------------------------------------------------------------------------
// Streaming encoder/decoder (P3-M8 — exact gc 1.27.1 shapes, now nameable
// since io landed). The constructors are real; Read/Write/Close stay
// fail-fast — they need r.Read/w.Write (2-result interface calls, WS D).
// Re-test when WS D lands.
// ---------------------------------------------------------------------------

type encoder struct {
	err  error
	enc  *Encoding
	w    io.Writer
	buf  [5]byte    // buffered data waiting to be encoded
	nbuf int        // number of bytes in buf
	out  [1024]byte // output buffer
}

// Write implements io.Writer. M8 scope: fail-fast (see above).
func (e *encoder) Write(p []byte) (n int, err error) {
	panic("base32.encoder.Write: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// Close implements io.Closer. M8 scope: fail-fast (see above).
func (e *encoder) Close() error {
	panic("base32.encoder.Close: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// NewEncoder returns a new base32 stream encoder. Data written to
// the returned writer will be encoded using enc and then written to w.
func NewEncoder(enc *Encoding, w io.Writer) io.WriteCloser {
	return &encoder{enc: enc, w: w}
}

type decoder struct {
	err    error
	enc    *Encoding
	r      io.Reader
	end    bool         // saw end of message
	buf    [1024]byte   // leftover input
	nbuf   int
	out    []byte       // leftover decoded output
	outbuf [1024 / 8 * 5]byte
}

// Read implements io.Reader. M8 scope: fail-fast (see above).
func (d *decoder) Read(p []byte) (n int, err error) {
	panic("base32.decoder.Read: not in M8 scope (needs 2-result interface dispatch, WS D; re-test when WS D lands)")
}

// NewDecoder returns a new base32 stream decoder.
func NewDecoder(enc *Encoding, r io.Reader) io.Reader {
	return &decoder{enc: enc, r: r}
}

// CorruptInputError is returned when the input is invalid.
type CorruptInputError int64

func (e CorruptInputError) Error() string {
	return "illegal base32 data at input byte " + base32FormatInt(int64(e))
}

// base32FormatInt formats v in base 10 (the strconv.FormatInt
// replacement: v1 synthetic scope has no strconv package).
func base32FormatInt(v int64) string {
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

// base32StripNewlines strips \r and \n from src into dst, returning the
// number of bytes written (gc's stripNewlines).
func base32StripNewlines(dst, src []byte) int {
	offset := 0
	for _, b := range src {
		if b == '\r' || b == '\n' {
			continue
		}
		dst[offset] = b
		offset++
	}
	return offset
}

// base32DecodeResult is the single-result carrier for the internal
// decode (gc's decode returns (n int, end bool, err error); v1 cannot
// emit multi-result functions, WS D).
type base32DecodeResult struct {
	n   int
	end bool
	err error
}

// decode is like Decode but returns an additional 'end' value, which
// indicates if end-of-message padding was encountered. src must have
// been stripped of \r and \n.
func (enc *Encoding) decode(dst, src []byte) base32DecodeResult {
	_ = enc.decodeMap

	dsti := 0
	olen := len(src)
	n := 0
	end := false

	for len(src) > 0 && !end {
		var dbuf [8]byte
		dlen := 8

		for j := 0; j < 8; {
			if len(src) == 0 {
				if enc.padChar != NoPadding {
					return base32DecodeResult{n: n, err: CorruptInputError(olen - len(src) - j)}
				}
				dlen, end = j, true
				break
			}
			in := src[0]
			src = src[1:]
			if in == byte(enc.padChar) && j >= 2 && len(src) < 8 {
				if len(src)+j < 8-1 {
					return base32DecodeResult{n: n, err: CorruptInputError(olen)}
				}
				for k := 0; k < 8-1-j; k++ {
					if len(src) > k && src[k] != byte(enc.padChar) {
						return base32DecodeResult{n: n, err: CorruptInputError(olen - len(src) + k - 1)}
					}
				}
				dlen, end = j, true
				if dlen == 1 || dlen == 3 || dlen == 6 {
					return base32DecodeResult{n: n, err: CorruptInputError(olen - len(src) - 1)}
				}
				break
			}
			dbuf[j] = enc.decodeMap[in]
			if dbuf[j] == 0xFF {
				return base32DecodeResult{n: n, err: CorruptInputError(olen - len(src) - 1)}
			}
			j++
		}

		switch dlen {
		case 8:
			dst[dsti+4] = dbuf[6]<<5 | dbuf[7]
			n++
			fallthrough
		case 7:
			dst[dsti+3] = dbuf[4]<<7 | dbuf[5]<<2 | dbuf[6]>>3
			n++
			fallthrough
		case 5:
			dst[dsti+2] = dbuf[3]<<4 | dbuf[4]>>1
			n++
			fallthrough
		case 4:
			dst[dsti+1] = dbuf[1]<<6 | dbuf[2]<<1 | dbuf[3]>>4
			n++
			fallthrough
		case 2:
			dst[dsti+0] = dbuf[0]<<3 | dbuf[1]>>2
			n++
		}
		dsti += 5
	}
	return base32DecodeResult{n: n, end: end}
}

// base32DecodePair is the Appendix M single-result carrier for Decode.
type base32DecodePair struct {
	n   int
	err error
}

// __base32Decode is the Appendix M desugar target behind
// (*Encoding).Decode: the real decode logic (gc 1.27.1 semantics:
// newline characters stripped, CorruptInputError on malformed input).
func __base32Decode(enc *Encoding, dst, src []byte) base32DecodePair {
	buf := make([]byte, len(src))
	l := base32StripNewlines(buf, src)
	r := enc.decode(dst, buf[:l])
	return base32DecodePair{n: r.n, err: r.err}
}

// base32DecodeStringPair is the Appendix M single-result carrier for
// DecodeString and AppendDecode.
type base32DecodeStringPair struct {
	b   []byte
	err error
}

// __base32DecodeString is the Appendix M desugar target behind
// (*Encoding).DecodeString.
func __base32DecodeString(enc *Encoding, s string) base32DecodeStringPair {
	buf := []byte(s)
	l := base32StripNewlines(buf, buf)
	r := enc.decode(buf, buf[:l])
	return base32DecodeStringPair{b: buf[:r.n], err: r.err}
}

// base32DecodedLen is gc's decodedLen.
func base32DecodedLen(n int, padChar rune) int {
	if padChar == NoPadding {
		return n/8*5 + n%8*5/8
	}
	return n / 8 * 5
}

// __base32AppendDecode is the Appendix M desugar target behind
// (*Encoding).AppendDecode.
func __base32AppendDecode(enc *Encoding, dst, src []byte) base32DecodeStringPair {
	n := len(src)
	for n > 0 && rune(src[n-1]) == enc.padChar {
		n--
	}
	n = base32DecodedLen(n, NoPadding)

	dst = base32Grow(dst, n)
	pr := __base32Decode(enc, dst[len(dst):][:n], src)
	return base32DecodeStringPair{b: dst[:len(dst)+pr.n], err: pr.err}
}

// Decode keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func (enc *Encoding) Decode(dst, src []byte) (n int, err error) {
	panic("encoding/base32: Decode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// DecodeString keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func (enc *Encoding) DecodeString(s string) ([]byte, error) {
	panic("encoding/base32: DecodeString: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// AppendDecode keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func (enc *Encoding) AppendDecode(dst, src []byte) ([]byte, error) {
	panic("encoding/base32: AppendDecode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of base32-encoded data.
func (enc *Encoding) DecodedLen(n int) int {
	return base32DecodedLen(n, enc.padChar)
}
`
