// P3-M7 (wave 1): synthetic `encoding/pem` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer. It imports the synthetic `encoding/base64` package
// (the context-imports-time pattern from M5: synthetic→synthetic
// imports resolve via the overlay importer). The resulting
// *types.Package is what overlayImporter.Import("encoding/pem")
// returns; the exporter emits the synthetic declarations' bodies into
// the same wire document via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (pem.go). Only
// frontend/v1-supported constructs (no generics, no multi-result
// declarations with real bodies — WS D): the Block type, Decode and
// EncodeToMemory are real Go, adapted from the gc 1.27.1 implementation
// (io-free). The bytes helpers used by gc (IndexByte/Index/LastIndex/
// HasSuffix/HasPrefix/TrimRight/TrimSpace/Cut/ContainsAny) are
// reimplemented locally (pemBytes*) — the wave-1 bytes package has not
// landed, and the import rules forbid reaching outside the synthetic
// set. Base64 encoding uses the synthetic encoding/base64 package
// (single-result members only); base64 decoding is reimplemented
// locally (pemBase64Decode) because the public Decode is multi-result
// and unexported cross-package names are unnameable (the Appendix M
// note in mrdesugar_m5.go).
//
// Multi-result member Decode keeps the exact gc signature with a
// fail-fast body for type-checking; its real implementation lives in
// the single-result __pemDecode helper, and the exporter desugars
// 2-target call sites to it (Appendix M pattern, mrdesugar_m7.go). Any
// other call position fails fast with the named M7 scope error.
//
// Omitted (io is not in M7 scope — NOT OK per the wave-1 import rules,
// so the exact gc signature cannot even be declared):
//   func Encode(out io.Writer, b *Block) error
// Re-test trigger: when io lands (P3-M8), declare it with the exact gc
// signature and implement it over the synthetic core. This omission is
// deliberate and loud — never silent (the p3frontend drift test asserts
// the omission list).
// Retirement: see ../../docs/arch/synth-retirement-tracker.md#encodingpem for this package's root cause and retires-when trigger (encoding/pem).
package main

// syntheticPemSource is the P3-M7 synthetic `encoding/pem` package:
// exact gc 1.27.1 signatures for the in-scope API; real-Go block
// parsing and PEM generation; fail-fast bodies for the multi-result
// member (desugared) and the io-dependent Encode (omitted).
const syntheticPemSource = `package pem

import "encoding/base64"

// Block represents a PEM encoded structure.
type Block struct {
	Type    string            // The type, taken from the preamble (i.e. "RSA PRIVATE KEY").
	Headers map[string]string // Optional headers.
	Bytes   []byte            // The decoded bytes of the contents. Typically a DER encoded ASN.1 structure.
}

// pemBytesIndexByte is bytes.IndexByte (local: the wave-1 bytes package
// has not landed).
func pemBytesIndexByte(data []byte, c byte) int {
	for i, b := range data {
		if b == c {
			return i
		}
	}
	return -1
}

// pemBytesIndex is bytes.Index (local).
func pemBytesIndex(data, sep []byte) int {
	if len(sep) == 0 {
		return 0
	}
	for i := 0; i+len(sep) <= len(data); i++ {
		match := true
		for j := 0; j < len(sep); j++ {
			if data[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// pemBytesLastIndex is bytes.LastIndex (local).
func pemBytesLastIndex(data, sep []byte) int {
	if len(sep) == 0 {
		return len(data)
	}
	for i := len(data) - len(sep); i >= 0; i-- {
		match := true
		for j := 0; j < len(sep); j++ {
			if data[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// pemBytesHasSuffix is bytes.HasSuffix (local).
func pemBytesHasSuffix(data, suffix []byte) bool {
	if len(suffix) > len(data) {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		if data[len(data)-len(suffix)+i] != suffix[i] {
			return false
		}
	}
	return true
}

// pemBytesHasPrefix is bytes.HasPrefix (local).
func pemBytesHasPrefix(data, prefix []byte) bool {
	if len(prefix) > len(data) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if data[i] != prefix[i] {
			return false
		}
	}
	return true
}

// pemBytesTrimRightSpaceTab is bytes.TrimRight(data, " \t") (local).
func pemBytesTrimRightSpaceTab(data []byte) []byte {
	i := len(data)
	for i > 0 && (data[i-1] == ' ' || data[i-1] == '\t') {
		i--
	}
	return data[:i]
}

// pemBytesTrimSpace is bytes.TrimSpace (local).
func pemBytesTrimSpace(data []byte) []byte {
	i := 0
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	j := len(data)
	for j > i && (data[j-1] == ' ' || data[j-1] == '\t' || data[j-1] == '\n' || data[j-1] == '\r') {
		j--
	}
	return data[i:j]
}

// pemBytesCutResult is the single-result carrier for pemBytesCut
// (gc's bytes.Cut returns 3 values).
type pemBytesCutResult struct {
	before []byte
	after  []byte
	found  bool
}

// pemBytesCut is bytes.Cut (local).
func pemBytesCut(data, sep []byte) pemBytesCutResult {
	i := pemBytesIndex(data, sep)
	if i < 0 {
		return pemBytesCutResult{before: data}
	}
	return pemBytesCutResult{before: data[:i], after: data[i+len(sep):], found: true}
}

// pemBytesContainsSpaceTab is bytes.ContainsAny(data, " \t") (local).
func pemBytesContainsSpaceTab(data []byte) bool {
	for _, b := range data {
		if b == ' ' || b == '\t' {
			return true
		}
	}
	return false
}

// pemLineResult is the single-result carrier for pemGetLine (gc's
// getLine returns (line, rest []byte, consumed int)).
type pemLineResult struct {
	line     []byte
	rest     []byte
	consumed int
}

// pemGetLine returns the first \r\n or \n delineated line (gc's getLine).
func pemGetLine(data []byte) pemLineResult {
	i := pemBytesIndexByte(data, '\n')
	var j int
	if i < 0 {
		i = len(data)
		j = i
	} else {
		j = i + 1
		if i > 0 && data[i-1] == '\r' {
			i--
		}
	}
	return pemLineResult{
		line:     pemBytesTrimRightSpaceTab(data[0:i]),
		rest:     data[j:],
		consumed: j,
	}
}

// pemRemoveSpacesAndTabs returns a copy of data with all spaces and
// tabs removed (gc's removeSpacesAndTabs).
func pemRemoveSpacesAndTabs(data []byte) []byte {
	if !pemBytesContainsSpaceTab(data) {
		return data
	}
	result := make([]byte, len(data))
	n := 0
	for _, b := range data {
		if b == ' ' || b == '\t' {
			continue
		}
		result[n] = b
		n++
	}
	return result[0:n]
}

var pemStart = []byte("\n-----BEGIN ")
var pemEnd = []byte("\n-----END ")
var pemEndOfLine = []byte("-----")
var pemColon = []byte(":")

// pemBase64Result is the single-result carrier for pemBase64Decode.
type pemBase64Result struct {
	n   int
	err error
}

// pemBase64Decode decodes RFC 4648 base64 (StdEncoding alphabet) into
// dst. It is a local port of the synthetic encoding/base64 decode core:
// the public base64 Decode is multi-result (desugared only at user call
// sites) and unexported cross-package helpers are unnameable, so pem
// carries its own copy. Differentially tested against gc like the rest.
func pemBase64Decode(dst, src []byte) pemBase64Result {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var decodeMap [256]uint8
	for i := range decodeMap {
		decodeMap[i] = 0xff
	}
	for i := 0; i < len(alphabet); i++ {
		decodeMap[alphabet[i]] = uint8(i)
	}
	n := 0
	si := 0
	for si < len(src) {
		var dbuf [4]byte
		dlen := 4
		var qerr error
		j := 0
		for j < 4 {
			if si == len(src) {
				if j == 0 {
					return pemBase64Result{n: n}
				}
				if j == 1 {
					return pemBase64Result{n: n, err: base64.CorruptInputError(si - j)}
				}
				dlen = j
				break
			}
			in := src[si]
			si++
			out := decodeMap[in]
			if out != 0xff {
				dbuf[j] = out
				j++
				continue
			}
			if in == '\n' || in == '\r' {
				continue
			}
			if in != '=' {
				return pemBase64Result{n: n, err: base64.CorruptInputError(si - 1)}
			}
			if j == 0 || j == 1 {
				return pemBase64Result{n: n, err: base64.CorruptInputError(si - 1)}
			}
			if j == 2 {
				for si < len(src) && (src[si] == '\n' || src[si] == '\r') {
					si++
				}
				if si == len(src) {
					return pemBase64Result{n: n, err: base64.CorruptInputError(len(src))}
				}
				if src[si] != '=' {
					return pemBase64Result{n: n, err: base64.CorruptInputError(si - 1)}
				}
				si++
			}
			for si < len(src) && (src[si] == '\n' || src[si] == '\r') {
				si++
			}
			if si < len(src) {
				qerr = base64.CorruptInputError(si)
			}
			dlen = j
			break
		}
		val := uint(dbuf[0])<<18 | uint(dbuf[1])<<12 | uint(dbuf[2])<<6 | uint(dbuf[3])
		switch dlen {
		case 4:
			dst[n] = byte(val >> 16)
			dst[n+1] = byte(val >> 8)
			dst[n+2] = byte(val)
			n += 3
		case 3:
			dst[n] = byte(val >> 16)
			dst[n+1] = byte(val >> 8)
			n += 2
		case 2:
			dst[n] = byte(val >> 16)
			n += 1
		}
		if qerr != nil {
			return pemBase64Result{n: n, err: qerr}
		}
	}
	return pemBase64Result{n: n}
}

// pemDecodePair is the Appendix M single-result carrier for Decode.
type pemDecodePair struct {
	p    *Block
	rest []byte
}

// __pemDecode is the Appendix M desugar target behind Decode: the real
// block parser (gc 1.27.1 semantics: finds the next PEM block, headers,
// base64 body; nil block + full input as rest when no PEM data).
func __pemDecode(data []byte) pemDecodePair {
	rest := data

	endTrailerIndex := 0
	for {
		if endTrailerIndex < 0 || endTrailerIndex > len(rest) {
			return pemDecodePair{rest: data}
		}
		rest = rest[endTrailerIndex:]

		endIndex := pemBytesIndex(rest, pemEnd)
		if endIndex < 0 {
			return pemDecodePair{rest: data}
		}
		endTrailerIndex = endIndex + len(pemEnd)
		beginIndex := pemBytesLastIndex(rest[:endIndex], pemStart[1:])
		if beginIndex < 0 || (beginIndex > 0 && rest[beginIndex-1] != '\n') {
			continue
		}
		rest = rest[beginIndex+len(pemStart)-1:]
		endIndex -= beginIndex + len(pemStart) - 1
		endTrailerIndex -= beginIndex + len(pemStart) - 1

		lr := pemGetLine(rest)
		typeLine, rest, consumed := lr.line, lr.rest, lr.consumed
		endIndex -= consumed
		endTrailerIndex -= consumed
		if !pemBytesHasSuffix(typeLine, pemEndOfLine) {
			continue
		}
		typeLine = typeLine[0 : len(typeLine)-len(pemEndOfLine)]

		p := &Block{
			Headers: make(map[string]string),
			Type:    string(typeLine),
		}

		for {
			if len(rest) == 0 {
				return pemDecodePair{rest: data}
			}
			lr = pemGetLine(rest)
			line, next, consumed := lr.line, lr.rest, lr.consumed

			cr := pemBytesCut(line, pemColon)
			if !cr.found {
				break
			}

			key := pemBytesTrimSpace(cr.before)
			val := pemBytesTrimSpace(cr.after)
			p.Headers[string(key)] = string(val)
			rest = next
			endIndex -= consumed
			endTrailerIndex -= consumed
		}

		if len(p.Headers) > 0 && endIndex < 0 {
			continue
		}

		endTrailer := rest[endTrailerIndex:]
		endTrailerLen := len(typeLine) + len(pemEndOfLine)
		if len(endTrailer) < endTrailerLen {
			continue
		}

		restOfEndLine := endTrailer[endTrailerLen:]
		endTrailer = endTrailer[:endTrailerLen]
		if !pemBytesHasPrefix(endTrailer, typeLine) ||
			!pemBytesHasSuffix(endTrailer, pemEndOfLine) {
			continue
		}

		if s := pemGetLine(restOfEndLine).line; len(s) != 0 {
			continue
		}

		p.Bytes = []byte{}
		if endIndex > 0 {
			base64Data := pemRemoveSpacesAndTabs(rest[:endIndex])
			p.Bytes = make([]byte, base64.StdEncoding.DecodedLen(len(base64Data)))
			dr := pemBase64Decode(p.Bytes, base64Data)
			if dr.err != nil {
				continue
			}
			p.Bytes = p.Bytes[:dr.n]
		}

		lr = pemGetLine(rest[endIndex+len(pemEnd)-1:])
		rest = lr.rest
		return pemDecodePair{p: p, rest: rest}
	}
}

// Decode keeps the exact gc 1.27.1 signature for type-checking; the
// exporter desugars 2-target call sites (Appendix M, mrdesugar_m7.go),
// every other position fails fast. Body never runs.
func Decode(data []byte) (p *Block, rest []byte) {
	panic("encoding/pem: Decode: not in M7 scope (multi-result calls need the M7 desugar; re-test when WS D lands)")
}

// pemWriteString appends s to out.
func pemWriteString(out []byte, s string) []byte {
	return append(out, s...)
}

// pemWriteHeader appends "k: v\n" to out.
func pemWriteHeader(out []byte, k, v string) []byte {
	out = append(out, k...)
	out = append(out, ':', ' ')
	out = append(out, v...)
	return append(out, '\n')
}

// pemSortStrings sorts keys in place (the slices.Sort replacement: v1
// synthetic scope has no slices package; insertion sort is exact for
// the ordering).
func pemSortStrings(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

// EncodeToMemory returns the PEM encoding of b. If b has invalid
// headers and cannot be encoded, EncodeToMemory returns nil.
func EncodeToMemory(b *Block) []byte {
	for k := range b.Headers {
		for i := 0; i < len(k); i++ {
			if k[i] == ':' {
				return nil
			}
		}
	}

	var out []byte
	out = pemWriteString(out, "-----BEGIN ")
	out = pemWriteString(out, b.Type)
	out = pemWriteString(out, "-----\n")

	if len(b.Headers) > 0 {
		const procType = "Proc-Type"
		h := make([]string, 0, len(b.Headers))
		hasProcType := false
		for k := range b.Headers {
			if k == procType {
				hasProcType = true
				continue
			}
			h = append(h, k)
		}
		if hasProcType {
			out = pemWriteHeader(out, procType, b.Headers[procType])
		}
		pemSortStrings(h)
		for _, k := range h {
			out = pemWriteHeader(out, k, b.Headers[k])
		}
		out = append(out, '\n')
	}

	encLen := base64.StdEncoding.EncodedLen(len(b.Bytes))
	enc := make([]byte, encLen)
	base64.StdEncoding.Encode(enc, b.Bytes)
	for len(enc) > 64 {
		out = append(out, enc[:64]...)
		out = append(out, '\n')
		enc = enc[64:]
	}
	if len(enc) > 0 {
		out = append(out, enc...)
		out = append(out, '\n')
	}

	out = pemWriteString(out, "-----END ")
	out = pemWriteString(out, b.Type)
	out = pemWriteString(out, "-----\n")
	return out
}
`
