// Targets strconv quoting: Quote/QuoteToASCII/QuoteToGraphic/QuoteRune*
// on control chars, invalid UTF-8, surrogates, BOM, non-printables;
// Unquote/UnquoteChar on every escape form and malformed input (error
// messages printed); AppendQuote*; CanBackquote; QuotedPrefix.
package main

import (
	"strconv"
	"strings"
)

func errs(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func main() {
	samples := []string{
		"", "plain", "tab\there", "nl\n\r\v\f\a\b", "quote\"'`", "back\\slash",
		"\x00\x01\x1f\x7f", "\x80\xff", "\u00e9\u65e5\u672c\U0001F600", "\u00a0\u2028\u2029\ufeff",
		"\u0300combining", "\U0010FFFF", "\xed\xa0\x80", "\u200b\u00ad", "a\u0085b",
		"\U000E0001", "~\x7e", "\u3000ideo",
	}
	for _, s := range samples {
		q := strconv.Quote(s)
		a := strconv.QuoteToASCII(s)
		g := strconv.QuoteToGraphic(s)
		u, err := strconv.Unquote(q)
		println(q, a, g, u == s, errs(err), strconv.CanBackquote(s))
	}
	runes := []rune{0, 7, 'a', '\'', '"', '\\', 0x7f, 0xa0, 0xad, 0x2028, 0xd800, 0xdfff, 0xfffd, 0xffff, 0x10ffff, 0x110000, -1, 0x1F600, 0x3000, 0xe0001}
	for _, r := range runes {
		println(r, strconv.QuoteRune(r), strconv.QuoteRuneToASCII(r), strconv.QuoteRuneToGraphic(r))
	}
	var buf []byte
	buf = strconv.AppendQuote(buf, "x\ty")
	buf = strconv.AppendQuoteToASCII(buf, "\u00fc")
	buf = strconv.AppendQuoteRune(buf, '\u263a')
	buf = strconv.AppendQuoteRuneToASCII(buf, '\u263a')
	buf = strconv.AppendQuoteToGraphic(buf, "\u00a0")
	buf = strconv.AppendQuoteRuneToGraphic(buf, '\u2028')
	println(string(buf))

	inputs := []string{
		`"abc"`, `'a'`, "`raw\\n`", `"\a\b\f\n\r\t\v\\\""`, `"\x41\101\u0041\U00000041"`,
		`"\xff"`, `"\377"`, `"\400"`, `"\u12"`, `"\U00110000"`, `"\ud800"`, `"\q"`,
		`"unterminated`, `"a"b"`, `''`, `'ab'`, `'\''`, `"\'"`, `'\"'`, "`a`b`",
		"`a\rb`", "`multi\nline`", `"new` + "\n" + `line"`, `"`, ``, `x`, `"\0"`,
		`"\08"`, `'\x80'`, `"é"`, `'日'`, `'\u65e5'`, "\"\xff\"", "'\xff'", `"\uFFFD"`,
	}
	for _, in := range inputs {
		u, err := strconv.Unquote(in)
		pre, perr := strconv.QuotedPrefix(in + "tail")
		println(strconv.Quote(in), "->", strconv.Quote(u), errs(err), strconv.Quote(pre), errs(perr))
	}
	ucs := []struct {
		s string
		q byte
	}{
		{`\n`, '"'}, {`\'`, '"'}, {`\'`, '\''}, {`\"`, '\''}, {`\"`, '"'}, {`"`, '"'}, {`'`, '\''},
		{`\x7g`, '"'}, {`\777`, '"'}, {`\u00e9rest`, '"'}, {`é!`, 0}, {"\xffz", '"'}, {`\`, '"'},
		{`\U0001F600x`, '\''}, {`\uD800`, '"'}, {`\z`, '"'}, {`abc`, '`'},
	}
	for _, c := range ucs {
		v, mb, tail, err := strconv.UnquoteChar(c.s, c.q)
		println(strconv.Quote(c.s), c.q, v, mb, strconv.Quote(tail), errs(err))
	}
	// round-trip sweep over BMP runes plus some astral; checksum the quotes
	var h uint64 = 14695981039346656037
	bad := 0
	var sb strings.Builder
	for r := rune(0); r < 0x11000; r += 7 {
		sb.Reset()
		sb.WriteRune(r)
		sb.WriteByte('x')
		s := sb.String()
		for _, q := range []string{strconv.Quote(s), strconv.QuoteToASCII(s), strconv.QuoteToGraphic(s), strconv.QuoteRune(r)} {
			for i := 0; i < len(q); i++ {
				h = (h ^ uint64(q[i])) * 1099511628211
			}
			u, err := strconv.Unquote(q)
			if err != nil || (u != s && u != string(r)) {
				bad++
			}
		}
		if strconv.CanBackquote(s) {
			h = (h ^ 1) * 1099511628211
		}
	}
	println("sweep", h, bad)
}
