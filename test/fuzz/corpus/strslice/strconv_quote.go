// Targets strconv quoting/unquoting over invalid UTF-8, surrogates and
// non-printables (Quote, QuoteToASCII, QuoteToGraphic, QuoteRune, Unquote,
// UnquoteChar, CanBackquote), plus Itoa/FormatInt/Parse error strings.
package main

import (
	"errors"
	"strconv"
)

func errs(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

var strs = []string{"", "plain", "tab\there", "nl\n", "\x00\x01\x7f", "\xff\xfe", "\xc0\x80", "\xed\xa0\x80",
	"é\u00ad\u200b\u2028\u2029", "\ufeff", "\U0001F600", "\U0010FFFF", "\ufffd", "back\\slash\"quote'", "`tick`",
	"\u0085\u00a0\u3000", "\a\b\f\r\v", "日本語"}

var quoted = []string{`""`, `"abc"`, `"\x41\u00e9\U0001F600"`, `"\xff"`, `"\ud800"`, `"\U00110000"`, `'a'`,
	`'ab'`, `'\''`, `"\'"`, "`raw\\n`", "`a\rb`", `"unterminated`, `"\q"`, `"a"b"`, `'\xff'`, `''`, `"\0"`,
	`"\000\377"`, `"\400"`, `'\u2028'`, `"\u00"`, `"日"`, "'日'", `"` + "\n" + `"`}

func main() {
	for _, s := range strs {
		q := strconv.Quote(s)
		a := strconv.QuoteToASCII(s)
		g := strconv.QuoteToGraphic(s)
		println("q", q, a, g, strconv.CanBackquote(s))
		u, err := strconv.Unquote(q)
		u2, err2 := strconv.Unquote(a)
		println("  rt", u == s, errs(err), u2 == s, errs(err2), len(u))
		b := strconv.AppendQuote([]byte("pre:"), s)
		println("  app", string(b) == "pre:"+q)
	}
	for _, q := range quoted {
		u, err := strconv.Unquote(q)
		println("unq", strconv.QuoteToASCII(q), strconv.QuoteToASCII(u), errs(err), errors.Is(err, strconv.ErrSyntax))
	}
	for _, r := range []rune{'a', '\'', '"', '\\', 0, 0x7f, 0xad, 0x2028, 0xd800, 0x110000, -1, 0x1f600, 0xfffd, 0xfeff} {
		println("qr", strconv.QuoteRune(r), strconv.QuoteRuneToASCII(r), strconv.QuoteRuneToGraphic(r), strconv.IsPrint(r), strconv.IsGraphic(r))
	}
	for _, s := range []string{`\x41rest`, `\u00e9x`, `\'x`, `\"x`, `\nx`, `\777`, `\xZZ`, `ab`, "\xffz", `\U0010ffff`} {
		for _, quote := range []byte{'"', '\''} {
			v, mb, tail, err := strconv.UnquoteChar(s, quote)
			println("uc", strconv.Quote(s), string(quote), v, mb, strconv.Quote(tail), errs(err))
		}
	}
	qp, err := strconv.QuotedPrefix(`"abc"def`)
	println("qp", qp, errs(err))
	_, err = strconv.QuotedPrefix(`"abc`)
	println("qp-err", errs(err))
	// integer formatting across bases and extremes
	for _, n := range []int64{0, -1, 1 << 62, -1 << 63, 1<<63 - 1, 255, -255} {
		println("fmt", strconv.Itoa(int(n)), strconv.FormatInt(n, 2), strconv.FormatInt(n, 16), strconv.FormatInt(n, 36))
	}
	println("fmtu", strconv.FormatUint(1<<64-1, 10), strconv.FormatUint(1<<64-1, 7))
	for _, s := range []string{"", "0", "-0", "+5", "0x1f", "1_000", "0b101", "0o17", "9223372036854775808", "-9223372036854775809", "12a", " 1", "٣"} {
		v, err := strconv.ParseInt(s, 0, 64)
		w, err2 := strconv.Atoi(s)
		x, err3 := strconv.ParseUint(s, 10, 8)
		println("parse", strconv.Quote(s), v, errs(err), w, errs(err2), x, errs(err3))
	}
	for _, s := range []string{"1", "t", "TRUE", "yes", "0", "F"} {
		b, err := strconv.ParseBool(s)
		println("bool", s, b, errs(err))
	}
	ne := &strconv.NumError{Func: "F", Num: "\xff", Err: strconv.ErrRange}
	println("numerr", ne.Error(), errors.Is(ne, strconv.ErrRange), errors.Unwrap(ne) == strconv.ErrRange)
}
