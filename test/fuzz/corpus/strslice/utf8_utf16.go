// Targets unicode/utf8 (DecodeRune/DecodeLastRune on invalid and
// truncated input, EncodeRune of invalid runes, ValidString, RuneLen,
// FullRune, AppendRune, RuneCount) and unicode/utf16 with lone surrogates.
package main

import (
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

var samples = []string{"", "a", "\x80", "\xc2", "\xc2\x80", "\xc0\x80", "\xe0\x80\x80", "\xe0\xa0", "\xe0\xa0\x80",
	"\xed\x9f\xbf", "\xed\xa0\x80", "\xef\xbf\xbf", "\xf0\x90\x80", "\xf0\x90\x80\x80", "\xf4\x8f\xbf\xbf",
	"\xf4\x90\x80\x80", "\xf5", "\xff", "a\xc3\xa9", "\xc3\xa9\xff", "日本", "日\xe6\x9c", "\xf0\x9f\x98\x80z"}

var runes = []rune{-1, 0, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000,
	0x10ffff, 0x110000, 0x7fffffff, -0x80000000}

func main() {
	for _, s := range samples {
		q := strconv.Quote(s)
		r, n := utf8.DecodeRuneInString(s)
		rb, nb := utf8.DecodeRune([]byte(s))
		lr, ln := utf8.DecodeLastRuneInString(s)
		lrb, lnb := utf8.DecodeLastRune([]byte(s))
		println("dec", q, r, n, rb, nb, lr, ln, lrb, lnb)
		println("  valid", utf8.ValidString(s), utf8.Valid([]byte(s)), utf8.FullRuneInString(s), utf8.FullRune([]byte(s)),
			utf8.RuneCountInString(s), utf8.RuneCount([]byte(s)), len(s) > 0 && utf8.RuneStart(s[len(s)-1]))
		// manual decode loop
		print("  loop:")
		for i := 0; i < len(s); {
			r, w := utf8.DecodeRuneInString(s[i:])
			print(" ", r, "/", w)
			i += w
		}
		println()
		print("  back:")
		for j := len(s); j > 0; {
			r, w := utf8.DecodeLastRuneInString(s[:j])
			print(" ", r, "/", w)
			j -= w
		}
		println()
	}
	for _, r := range runes {
		buf := make([]byte, 4)
		n := utf8.EncodeRune(buf, r)
		app := utf8.AppendRune([]byte("x"), r)
		println("enc", r, n, strconv.Quote(string(buf[:n])), utf8.RuneLen(r), utf8.ValidRune(r), strconv.Quote(string(app)))
	}
	// EncodeRune into a too-small buffer panics
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				if e, ok := rec.(interface{ Error() string }); ok {
					println("small buffer panic:", e.Error())
				}
			}
		}()
		b := make([]byte, 1)
		utf8.EncodeRune(b, 0x20ac)
	}()
	// utf16
	for _, rs := range [][]rune{{'a', 0x10000, 0x10ffff}, {0xd800}, {0xdfff, 'b'}, {-1, 0x110000}, {0xfffd, 0xffff, 0xe000}} {
		enc := utf16.Encode(rs)
		print("u16enc")
		for _, u := range enc {
			print(" ", u)
		}
		println()
		dec := utf16.Decode(enc)
		print("u16dec")
		for _, r := range dec {
			print(" ", r)
		}
		println()
	}
	for _, us := range [][]uint16{{0xd800}, {0xdc00}, {0xd800, 0xdc00}, {0xdc00, 0xd800}, {0xd800, 'a', 0xdc00}, {0xdbff, 0xdfff}, {0xd83d, 0xde00, 0xd83d}, {}} {
		dec := utf16.Decode(us)
		print("u16raw", len(us), "->")
		for _, r := range dec {
			print(" ", r)
		}
		println(" ", strconv.QuoteToASCII(string(dec)))
	}
	for _, r := range []rune{0xffff, 0x10000, 0xd800, 0x10ffff, 0x110000, -1} {
		r1, r2 := utf16.EncodeRune(r)
		println("pair", r, r1, r2, utf16.DecodeRune(r1, r2), utf16.IsSurrogate(r), utf16.RuneLen(r))
	}
	println("decoderune-bad", utf16.DecodeRune(0xdc00, 0xd800), utf16.DecodeRune('a', 'b'))
	ap := utf16.AppendRune(nil, 0x1f600)
	ap = utf16.AppendRune(ap, 0xd800)
	ap = utf16.AppendRune(ap, 'z')
	print("append16")
	for _, u := range ap {
		print(" ", u)
	}
	println()
	// round trip of every 97th code point
	bad := 0
	for r := rune(0); r <= 0x10ffff; r += 97 {
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		d, w := utf8.DecodeRune(buf[:n])
		if w != n || (d != r && utf8.ValidRune(r)) {
			bad++
		}
		e := utf16.Encode([]rune{r})
		if utf16.Decode(e)[0] != r && utf8.ValidRune(r) {
			bad++
		}
	}
	println("roundtrip bad", bad)
	println("consts", utf8.RuneError, utf8.RuneSelf, utf8.MaxRune, utf8.UTFMax)
}
