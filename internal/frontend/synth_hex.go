// P3-M7 (wave 1): synthetic `encoding/hex` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/hex")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (hex.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): Encode, EncodeToString,
// EncodedLen, DecodedLen, AppendEncode, Dump, ErrLength,
// InvalidByteError.Error are real Go, adapted from the gc 1.27.1
// implementation (io-free; the streaming encoder/decoder/dumper types,
// NewEncoder/NewDecoder/Dumper are omitted — see below). The
// errors/fmt/slices helpers used by gc are replaced with local
// equivalents (hexErrorString, hexQuoteRune, hexGrow). Dump is
// implemented directly over a byte slice (no io, no strings.Builder).
//
// Multi-result members (Decode, DecodeString, AppendDecode) keep exact
// gc signatures with fail-fast bodies for type-checking; their real
// implementations live in the single-result __hexDecode,
// __hexDecodeString and __hexAppendDecode helpers, and the exporter
// desugars N-target call sites to them (Appendix M pattern,
// mrdesugar_m7.go). Any other call position fails fast with the named
// M7 scope error.
//
// Omitted (io is not in M7 scope — NOT OK per the wave-1 import rules,
// so the exact gc signatures cannot even be declared):
//   func NewEncoder(w io.Writer) io.Writer
//   func NewDecoder(r io.Reader) io.Reader
//   func Dumper(w io.Writer) io.WriteCloser
// Re-test trigger: when io lands (P3-M8), declare these with exact gc
// signatures and implement the streaming encoder/decoder/dumper over
// the synthetic Encode/Decode core. This omission is deliberate and
// loud — never silent (the p3frontend drift test asserts the omission
// list).
// Retirement: see ../../docs/arch/synth-retirement-tracker.md#encodinghex for this package's root cause and retires-when trigger (encoding/hex).
package main

// syntheticHexSource is the P3-M7 synthetic `encoding/hex` package:
// exact gc 1.27.1 signatures for the in-scope API; real-Go encode/decode
// core; fail-fast bodies for the multi-result members (desugared) and
// the io-dependent streaming API (omitted).
const syntheticHexSource = `package hex

const (
	hexTable        = "0123456789abcdef"
	hexReverseTable = "" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\xff\xff\xff\xff\xff\xff" +
		"\xff\x0a\x0b\x0c\x0d\x0e\x0f\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\x0a\x0b\x0c\x0d\x0e\x0f\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"
)

// EncodedLen returns the length of an encoding of n source bytes.
func EncodedLen(n int) int { return n * 2 }

// Encode encodes src into EncodedLen(len(src)) bytes of dst.
func Encode(dst, src []byte) int {
	j := 0
	for _, v := range src {
		dst[j] = hexTable[v>>4]
		dst[j+1] = hexTable[v&0x0f]
		j += 2
	}
	return len(src) * 2
}

// hexGrow is the slices.Grow replacement (v1 synthetic scope has no
// slices package).
func hexGrow(dst []byte, n int) []byte {
	if n <= cap(dst)-len(dst) {
		return dst
	}
	ndst := make([]byte, len(dst), 2*cap(dst)+n)
	copy(ndst, dst)
	return ndst
}

// AppendEncode appends the hexadecimally encoded src to dst
// and returns the extended buffer.
func AppendEncode(dst, src []byte) []byte {
	n := EncodedLen(len(src))
	dst = hexGrow(dst, n)
	Encode(dst[len(dst):][:n], src)
	return dst[:len(dst)+n]
}

// hexErrorString is the errors.New replacement (v1 synthetic scope has
// no errors import here to keep the package self-contained).
type hexErrorString struct {
	s string
}

func (e *hexErrorString) Error() string { return e.s }

// ErrLength reports an attempt to decode an odd-length input.
var ErrLength = &hexErrorString{s: "encoding/hex: odd length hex string"}

// InvalidByteError values describe errors resulting from an invalid byte
// in a hex string.
type InvalidByteError byte

func (e InvalidByteError) Error() string {
	return "encoding/hex: invalid byte: " + hexFormatRune(rune(e))
}

// hexFormatRune renders r exactly like gc's fmt "%#U" verb (which
// InvalidByteError.Error uses): "U+XXXX" plus, for printable runes, a
// space and the rune UTF-8-encoded between single quotes (fmt writes
// with utf8.EncodeRune, not Go-quoted: '\'' shows as ''', 0xA1 as the
// two UTF-8 bytes of '¡'). Printable follows strconv.IsPrint for
// 0-255 (verified against gc 1.27.1): 0x20-0x7E, 0xA1-0xAC, 0xAE-0xFF;
// 0xA0 and 0xAD are not printable.
func hexFormatRune(r rune) string {
	var buf [12]byte
	w := len(buf)
	b := byte(r)
	printable := (b >= 0x20 && b <= 0x7e) || (b >= 0xa1 && b <= 0xac) || (b >= 0xae)
	if printable {
		w--
		buf[w] = '\''
		if b < 0x80 {
			w--
			buf[w] = b
		} else {
			w--
			buf[w] = 0x80 | (b & 0x3f)
			w--
			buf[w] = 0xc0 | (b >> 6)
		}
		w--
		buf[w] = '\''
		w--
		buf[w] = ' '
	}
	w--
	buf[w] = "0123456789ABCDEF"[b&0xf]
	w--
	buf[w] = "0123456789ABCDEF"[b>>4]
	w--
	buf[w] = '0'
	w--
	buf[w] = '0'
	w--
	buf[w] = '+'
	w--
	buf[w] = 'U'
	return string(buf[w:])
}

// DecodedLen returns the length of a decoding of x source bytes.
func DecodedLen(x int) int { return x / 2 }

// hexDecodePair is the Appendix M single-result carrier for Decode.
type hexDecodePair struct {
	n   int
	err error
}

// __hexDecode is the Appendix M desugar target behind Decode: the real
// decode logic (gc 1.27.1 semantics: InvalidByteError before ErrLength
// on odd-length input with a bad trailing byte).
func __hexDecode(dst, src []byte) hexDecodePair {
	i, j := 0, 0
	for ; j < len(src)-1; j += 2 {
		p := src[j]
		q := src[j+1]

		a := hexReverseTable[p]
		b := hexReverseTable[q]
		if a > 0x0f {
			return hexDecodePair{n: i, err: InvalidByteError(p)}
		}
		if b > 0x0f {
			return hexDecodePair{n: i, err: InvalidByteError(q)}
		}
		dst[i] = (a << 4) | b
		i++
	}
	if len(src)%2 == 1 {
		if hexReverseTable[src[j]] > 0x0f {
			return hexDecodePair{n: i, err: InvalidByteError(src[j])}
		}
		return hexDecodePair{n: i, err: ErrLength}
	}
	return hexDecodePair{n: i}
}

// hexDecodeStringPair is the Appendix M single-result carrier for
// DecodeString and AppendDecode.
type hexDecodeStringPair struct {
	b   []byte
	err error
}

// __hexDecodeString is the Appendix M desugar target behind
// DecodeString.
func __hexDecodeString(s string) hexDecodeStringPair {
	dst := make([]byte, DecodedLen(len(s)))
	pr := __hexDecode(dst, []byte(s))
	return hexDecodeStringPair{b: dst[:pr.n], err: pr.err}
}

// __hexAppendDecode is the Appendix M desugar target behind
// AppendDecode.
func __hexAppendDecode(dst, src []byte) hexDecodeStringPair {
	n := DecodedLen(len(src))
	dst = hexGrow(dst, n)
	pr := __hexDecode(dst[len(dst):][:n], src)
	return hexDecodeStringPair{b: dst[:len(dst)+pr.n], err: pr.err}
}

// Decode keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Decode(dst, src []byte) (int, error) {
	panic("encoding/hex: Decode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// AppendDecode keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func AppendDecode(dst, src []byte) ([]byte, error) {
	panic("encoding/hex: AppendDecode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// EncodeToString returns the hexadecimal encoding of src.
func EncodeToString(src []byte) string {
	dst := make([]byte, EncodedLen(len(src)))
	Encode(dst, src)
	return string(dst)
}

// DecodeString keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M,
// mrdesugar_m7.go), every other position fails fast. Body never runs.
func DecodeString(s string) ([]byte, error) {
	panic("encoding/hex: DecodeString: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// hexDumpToChar maps a byte to its dump character (gc's toChar).
func hexDumpToChar(b byte) byte {
	if b < 32 || b > 126 {
		return '.'
	}
	return b
}

// hexDumper accumulates a hexdump-C line stream into a byte slice (gc's
// dumper, without the io.Writer).
type hexDumper struct {
	out        []byte
	rightChars [18]byte
	buf        [14]byte
	used       int  // number of bytes in the current line
	n          uint // number of bytes, total
	closed     bool
}

func (h *hexDumper) write(data []byte) {
	for i := range data {
		if h.used == 0 {
			h.buf[0] = byte(h.n >> 24)
			h.buf[1] = byte(h.n >> 16)
			h.buf[2] = byte(h.n >> 8)
			h.buf[3] = byte(h.n)
			Encode(h.buf[4:], h.buf[:4])
			h.buf[12] = ' '
			h.buf[13] = ' '
			h.out = append(h.out, h.buf[4:]...)
		}
		Encode(h.buf[:], data[i:i+1])
		h.buf[2] = ' '
		l := 3
		if h.used == 7 {
			h.buf[3] = ' '
			l = 4
		} else if h.used == 15 {
			h.buf[3] = ' '
			h.buf[4] = '|'
			l = 5
		}
		h.out = append(h.out, h.buf[:l]...)
		h.rightChars[h.used] = hexDumpToChar(data[i])
		h.used++
		h.n++
		if h.used == 16 {
			h.rightChars[16] = '|'
			h.rightChars[17] = '\n'
			h.out = append(h.out, h.rightChars[:]...)
			h.used = 0
		}
	}
}

func (h *hexDumper) close() {
	if h.closed {
		return
	}
	h.closed = true
	if h.used == 0 {
		return
	}
	h.buf[0] = ' '
	h.buf[1] = ' '
	h.buf[2] = ' '
	h.buf[3] = ' '
	h.buf[4] = '|'
	nBytes := h.used
	for h.used < 16 {
		l := 3
		if h.used == 7 {
			l = 4
		} else if h.used == 15 {
			l = 5
		}
		h.out = append(h.out, h.buf[:l]...)
		h.used++
	}
	h.rightChars[nBytes] = '|'
	h.rightChars[nBytes+1] = '\n'
	h.out = append(h.out, h.rightChars[:nBytes+2]...)
}

// Dump returns a string that contains a hex dump of the given data. The
// format of the hex dump matches the output of 'hexdump -C'.
func Dump(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	h := &hexDumper{}
	h.write(data)
	h.close()
	return string(h.out)
}
`
