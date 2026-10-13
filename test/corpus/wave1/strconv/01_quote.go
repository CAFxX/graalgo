package main

import (
	"fmt"
	"strconv"
)

func main() {
	// Quote/Unquote with escapes, unicode, edge cases.
	for _, s := range []string{"", "hello", "a\nb", "a\tb", "a\rb", "a\\b", "\"quoted\"",
		"héllo", "🙂", "a\x00b", "a\x7fb", "\x1b[31m", "back`tick"} {
		q := strconv.Quote(s)
		uq, err := strconv.Unquote(q)
		fmt.Printf("Quote(%q) = %s, roundtrip=%v, err=%v\n", s, q, uq == s, err != nil)
	}

	// QuoteToASCII / QuoteToGraphic.
	fmt.Printf("QuoteToASCII(%q) = %s\n", "héllo", strconv.QuoteToASCII("héllo"))
	fmt.Printf("QuoteToGraphic(%q) = %s\n", "a\x00b", strconv.QuoteToGraphic("a\x00b"))

	// QuoteRune.
	for _, r := range []rune{'a', '\n', 'é', '🙂', 0x0, 0x7f} {
		fmt.Printf("QuoteRune(%U) = %s\n", r, strconv.QuoteRune(r))
	}

	// Unquote errors.
	for _, s := range []string{`"abc`, `"a\xzz"`, `"a\u12"`, "'ab'", "`abc", `"a\q"`, ""} {
		v, err := strconv.Unquote(s)
		fmt.Printf("Unquote(%q) = %q, err=%v\n", s, v, err != nil)
	}

	// UnquoteChar (4 results).
	for _, tc := range []struct{ s string; quote byte }{
		{`a"`, '"'}, {`\n"`, '"'}, {`\u00e9"`, '"'}, {`\"`, 0}, {`x`, '\''},
	} {
		v, mb, tail, err := strconv.UnquoteChar(tc.s, tc.quote)
		fmt.Printf("UnquoteChar(%q) = %U, %v, %q, err=%v\n", tc.s, v, mb, tail, err != nil)
	}

	// CanBackquote / IsPrint / IsGraphic.
	for _, s := range []string{"hello", "a\nb", "a\tb", "héllo"} {
		fmt.Printf("CanBackquote(%q) = %v\n", s, strconv.CanBackquote(s))
	}
	for _, r := range []rune{'a', '\n', ' ', 0x7f, 'é', 0x2028} {
		fmt.Printf("IsPrint(%U)=%v IsGraphic(%U)=%v\n", r, strconv.IsPrint(r), r, strconv.IsGraphic(r))
	}

	// QuotedPrefix.
	for _, s := range []string{`"abc"def`, `"a\nb"`, `xyz`, `"unterminated`} {
		v, err := strconv.QuotedPrefix(s)
		fmt.Printf("QuotedPrefix(%q) = %q, err=%v\n", s, v, err != nil)
	}

	// ParseComplex.
	for _, s := range []string{"1+2i", "(1+2i)", "1i", "1", "i", "1+2j", ""} {
		v, err := strconv.ParseComplex(s, 128)
		fmt.Printf("ParseComplex(%q) = %v, err=%v\n", s, v, err != nil)
	}
	fmt.Printf("FormatComplex(1+2i) = %q\n", strconv.FormatComplex(1+2i, 'f', -1, 128))
}
