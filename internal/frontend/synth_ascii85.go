// P3-M7 (wave 1): synthetic `encoding/ascii85` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (self-contained: no imports). The resulting
// *types.Package is what overlayImporter.Import("encoding/ascii85")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (ascii85.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): Encode, MaxEncodedLen and
// CorruptInputError.Error are real Go, adapted from the gc 1.27.1
// implementation (io-free; the streaming encoder/decoder types and
// NewEncoder/NewDecoder are omitted — see below). The strconv helper
// used by gc is replaced with a local equivalent (ascii85FormatInt).
//
// Multi-result member Decode (3 results: ndst, nsrc, err) keeps the
// exact gc signature with a fail-fast body for type-checking; its real
// implementation lives in the single-result __ascii85Decode helper, and
// the exporter desugars 3-target call sites to it (Appendix M pattern,
// mrdesugar_m7.go). Any other call position fails fast with the named
// M7 scope error.
//
// Omitted (io is not in M7 scope — NOT OK per the wave-1 import rules,
// so the exact gc signatures cannot even be declared):
//   func NewEncoder(w io.Writer) io.WriteCloser
//   func NewDecoder(r io.Reader) io.Reader
// Re-test trigger: when io lands (P3-M8), declare these with exact gc
// signatures and implement the streaming encoder/decoder over the
// synthetic Encode/Decode core. This omission is deliberate and loud —
// never silent (the p3frontend drift test asserts the omission list).
package main

// syntheticAscii85Source is the P3-M7 synthetic `encoding/ascii85`
// package: exact gc 1.27.1 signatures for the in-scope API; real-Go
// encode/decode core; fail-fast bodies for the multi-result member
// (desugared) and the io-dependent streaming API (omitted).
const syntheticAscii85Source = `package ascii85

// Encode encodes src into at most MaxEncodedLen(len(src)) bytes of dst,
// returning the actual number of bytes written.
func Encode(dst, src []byte) int {
	if len(src) == 0 {
		return 0
	}

	n := 0
	for len(src) > 0 {
		dst[0] = 0
		dst[1] = 0
		dst[2] = 0
		dst[3] = 0
		dst[4] = 0

		var v uint32
		switch len(src) {
		default:
			v |= uint32(src[3])
			fallthrough
		case 3:
			v |= uint32(src[2]) << 8
			fallthrough
		case 2:
			v |= uint32(src[1]) << 16
			fallthrough
		case 1:
			v |= uint32(src[0]) << 24
		}

		if v == 0 && len(src) >= 4 {
			dst[0] = 'z'
			dst = dst[1:]
			src = src[4:]
			n++
			continue
		}

		for i := 4; i >= 0; i-- {
			dst[i] = '!' + byte(v%85)
			v /= 85
		}

		m := 5
		if len(src) < 4 {
			m -= 4 - len(src)
			src = nil
		} else {
			src = src[4:]
		}
		dst = dst[m:]
		n += m
	}
	return n
}

// MaxEncodedLen returns the maximum length of an encoding of n source bytes.
func MaxEncodedLen(n int) int { return (n + 3) / 4 * 5 }

// CorruptInputError is returned when the input is invalid.
type CorruptInputError int64

func (e CorruptInputError) Error() string {
	return "illegal ascii85 data at input byte " + ascii85FormatInt(int64(e))
}

// ascii85FormatInt formats v in base 10 (the strconv.FormatInt
// replacement: v1 synthetic scope has no strconv package).
func ascii85FormatInt(v int64) string {
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

// ascii85DecodeTriple is the Appendix M single-result carrier for Decode
// (gc returns (ndst, nsrc int, err error); v1 cannot emit multi-result
// functions, WS D).
type ascii85DecodeTriple struct {
	ndst int
	nsrc int
	err  error
}

// __ascii85Decode is the Appendix M desugar target behind Decode: the
// real decode logic (gc 1.27.1 semantics: space/control chars ignored,
// 'z' shorthand, flush handling, CorruptInputError on malformed input).
func __ascii85Decode(dst, src []byte, flush bool) ascii85DecodeTriple {
	var v uint32
	var nb int
	ndst := 0
	nsrc := 0
	for i, b := range src {
		if len(dst)-ndst < 4 {
			return ascii85DecodeTriple{ndst: ndst, nsrc: nsrc}
		}
		switch {
		case b <= ' ':
			continue
		case b == 'z' && nb == 0:
			nb = 5
			v = 0
		case '!' <= b && b <= 'u':
			v = v*85 + uint32(b-'!')
			nb++
		default:
			return ascii85DecodeTriple{err: CorruptInputError(i)}
		}
		if nb == 5 {
			nsrc = i + 1
			dst[ndst] = byte(v >> 24)
			dst[ndst+1] = byte(v >> 16)
			dst[ndst+2] = byte(v >> 8)
			dst[ndst+3] = byte(v)
			ndst += 4
			nb = 0
			v = 0
		}
	}
	if flush {
		nsrc = len(src)
		if nb > 0 {
			if nb == 1 {
				return ascii85DecodeTriple{err: CorruptInputError(len(src))}
			}
			for i := nb; i < 5; i++ {
				v = v*85 + 84
			}
			for i := 0; i < nb-1; i++ {
				dst[ndst] = byte(v >> 24)
				v <<= 8
				ndst++
			}
		}
	}
	return ascii85DecodeTriple{ndst: ndst, nsrc: nsrc}
}

// Decode keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 3-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Decode(dst, src []byte, flush bool) (ndst, nsrc int, err error) {
	panic("encoding/ascii85: Decode: unreachable (call sites desugar to __ascii85Decode)")
}
`
