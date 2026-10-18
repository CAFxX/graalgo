// SYNTHETIC (Principle 4) — unicode/utf16: real go1.27.1 source does not compile under graalgo as-published (import closure hits go:linkname / asm / intrinsics / unsafe — B-4); pure-Go port.
// RETIRES WHEN: graalgo compiles the real go1.27.1 GOROOT sources directly — then delete this file.
// RE-TEST TRIGGER: real-source compilation lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (wave 3, tier-1a worker): synthetic `unicode/utf16` package.
//
// REAL Go source adapted from gc go1.27.1 ($GOROOT/src/unicode/utf16),
// embedded as text and type-checked on demand with the overlay importer.
// The resulting *types.Package is what overlayImporter.Import("unicode/utf16")
// returns; the exporter emits the synthetic declarations' bodies into
// the same wire document via exportSyntheticPackages.
//
// Verbatim copy of gc go1.27.1 unicode/utf16/utf16.go.
// Self-contained, no imports.
package main

// syntheticUnicodeUtf16Source is the P3-M9 synthetic `unicode/utf16` package.
const syntheticUnicodeUtf16Source = `package utf16
// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package utf16 implements encoding and decoding of UTF-16 sequences.

// The conditions replacementChar==unicode.ReplacementChar and
// maxRune==unicode.MaxRune are verified in the tests.
// Defining them locally avoids this package depending on package unicode.

const (
	replacementChar = '\uFFFD'     // Unicode replacement character
	maxRune         = '\U0010FFFF' // Maximum valid Unicode code point.
)

const (
	// 0xd800-0xdc00 encodes the high 10 bits of a pair.
	// 0xdc00-0xe000 encodes the low 10 bits of a pair.
	// the value is those 20 bits plus 0x10000.
	surr1 = 0xd800
	surr2 = 0xdc00
	surr3 = 0xe000

	surrSelf = 0x10000
)

// IsSurrogate reports whether the specified Unicode code point
// can appear in a surrogate pair.
func IsSurrogate(r rune) bool {
	return surr1 <= r && r < surr3
}

// DecodeRune returns the UTF-16 decoding of a surrogate pair.
// If the pair is not a valid UTF-16 surrogate pair, DecodeRune returns
// the Unicode replacement code point U+FFFD.
func DecodeRune(r1, r2 rune) rune {
	if surr1 <= r1 && r1 < surr2 && surr2 <= r2 && r2 < surr3 {
		return (r1-surr1)<<10 | (r2 - surr2) + surrSelf
	}
	return replacementChar
}

// EncodeRune returns the UTF-16 surrogate pair r1, r2 for the given rune.
// If the rune is not a valid Unicode code point or does not need encoding,
// EncodeRune returns U+FFFD, U+FFFD.
func EncodeRune(r rune) (r1, r2 rune) {
	if r < surrSelf || r > maxRune {
		return replacementChar, replacementChar
	}
	r -= surrSelf
	return surr1 + (r>>10)&0x3ff, surr2 + r&0x3ff
}

// encodeRunePair is the single-result internal helper for EncodeRune.
// (P3-M9 tier-1a: v1 cannot emit multi-result functions (P3-M5), so the
// public EncodeRune is skipped; internal callers use this helper.)
func encodeRunePair(r rune) [2]rune {
	if r < surrSelf || r > maxRune {
		return [2]rune{replacementChar, replacementChar}
	}
	r -= surrSelf
	return [2]rune{surr1 + (r>>10)&0x3ff, surr2 + r&0x3ff}
}

// RuneLen returns the number of 16-bit words in the UTF-16 encoding of the rune.
// It returns -1 if the rune is not a valid value to encode in UTF-16.
func RuneLen(r rune) int {
	switch {
	case 0 <= r && r < surr1, surr3 <= r && r < surrSelf:
		return 1
	case surrSelf <= r && r <= maxRune:
		return 2
	default:
		return -1
	}
}

// Encode returns the UTF-16 encoding of the Unicode code point sequence s.
func Encode(s []rune) []uint16 {
	n := len(s)
	for _, v := range s {
		if v >= surrSelf {
			n++
		}
	}

	a := make([]uint16, n)
	n = 0
	for _, v := range s {
		switch RuneLen(v) {
		case 1: // normal rune
			a[n] = uint16(v)
			n++
		case 2: // needs surrogate sequence
			rr := encodeRunePair(v)
			a[n] = uint16(rr[0])
			a[n+1] = uint16(rr[1])
			n += 2
		default:
			a[n] = uint16(replacementChar)
			n++
		}
	}
	return a[:n]
}

// AppendRune appends the UTF-16 encoding of the Unicode code point r
// to the end of p and returns the extended buffer. If the rune is not
// a valid Unicode code point, it appends the encoding of U+FFFD.
func AppendRune(a []uint16, r rune) []uint16 {
	// This function is inlineable for fast handling of ASCII.
	switch {
	case 0 <= r && r < surr1, surr3 <= r && r < surrSelf:
		// normal rune
		return append(a, uint16(r))
	case surrSelf <= r && r <= maxRune:
		// needs surrogate sequence
		rr := encodeRunePair(r)
		return append(a, uint16(rr[0]), uint16(rr[1]))
	}
	return append(a, replacementChar)
}

// Decode returns the Unicode code point sequence represented
// by the UTF-16 encoding s.
func Decode(s []uint16) []rune {
	// Preallocate capacity to hold up to 64 runes.
	// Decode inlines, so the allocation can live on the stack.
	buf := make([]rune, 0, 64)
	return decode(s, buf)
}

// decode appends to buf the Unicode code point sequence represented
// by the UTF-16 encoding s and return the extended buffer.
func decode(s []uint16, buf []rune) []rune {
	for i := 0; i < len(s); i++ {
		var ar rune
		switch r := s[i]; {
		case r < surr1, surr3 <= r:
			// normal rune
			ar = rune(r)
		case surr1 <= r && r < surr2 && i+1 < len(s) &&
			surr2 <= s[i+1] && s[i+1] < surr3:
			// valid surrogate sequence
			ar = DecodeRune(rune(r), rune(s[i+1]))
			i++
		default:
			// invalid surrogate sequence
			ar = replacementChar
		}
		buf = append(buf, ar)
	}
	return buf
}
`
