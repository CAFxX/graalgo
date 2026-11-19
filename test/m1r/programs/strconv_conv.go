package main

// strconv from upstream's source: it imports errors (ErrRange, ErrSyntax
// and *NumError wrap them), whose initializer needs descriptor images
// (toolexec W9a), and unicode/utf8 for quoting. Unquote and QuotedPrefix
// reach internal/bytealg.IndexByteString, an assembly function (the M1c
// bytealg intrinsics).

import (
	"errors"
	"strconv"
	"unicode/utf8"
)

func show(v any, err error) {
	switch x := v.(type) {
	case int:
		print(x)
	case int64:
		print(x)
	case uint64:
		print(x)
	case float64:
		print(x)
	case bool:
		print(x)
	case string:
		print(x)
	}
	if err != nil {
		var ne *strconv.NumError
		print(" err=", err.Error())
		if errors.As(err, &ne) {
			print(" [func=", ne.Func, " num=", ne.Num, " range=", errors.Is(err, strconv.ErrRange), " syntax=", errors.Is(err, strconv.ErrSyntax), "]")
		}
	}
	println()
}

func main() {
	// Integers.
	for _, s := range []string{"0", "42", "-42", "+7", "9223372036854775807", "9223372036854775808", "-9223372036854775809", "", "abc", "12a", "1_000", " 1", "0x1f", "0b101", "0o17", "017"} {
		show(strconv.Atoi(s))
	}
	for _, c := range []struct {
		s    string
		base int
		bits int
	}{{"ff", 16, 64}, {"-80", 16, 8}, {"80", 16, 8}, {"zz", 36, 64}, {"0x1f", 0, 64}, {"0b101", 0, 16}, {"0o17", 0, 64}, {"1_000", 0, 64}, {"017", 0, 64},
		{"300", 10, 8}, {"-129", 10, 8}, {"32767", 10, 16}, {"2", 2, 64}, {"10", 1, 64}, {"10", 37, 64}, {"1", 10, 65}} {
		show(strconv.ParseInt(c.s, c.base, c.bits))
	}
	for _, s := range []string{"18446744073709551615", "18446744073709551616", "-1", "255", "0xFF", "1e3"} {
		show(strconv.ParseUint(s, 0, 64))
	}
	show(strconv.ParseUint("256", 10, 8))
	println(strconv.Itoa(0), strconv.Itoa(-1), strconv.Itoa(1<<62), strconv.FormatInt(-255, 16), strconv.FormatInt(35, 36),
		strconv.FormatUint(1<<63, 2), strconv.FormatInt(-9223372036854775808, 10), strconv.FormatUint(18446744073709551615, 10))

	// Booleans.
	for _, s := range []string{"1", "t", "TRUE", "true", "0", "F", "False", "yes", ""} {
		show(strconv.ParseBool(s))
	}
	println(strconv.FormatBool(true), strconv.FormatBool(false))

	// Floats: parsing (exact, Eisel-Lemire and the slow path), special values, errors.
	for _, s := range []string{"0", "-0", "1", "1.5", "3.14159", "1e10", "1e-10", "1.7976931348623157e308", "1.8e308", "-1.8e308", "5e-324", "2e-324",
		"4.9e-325", "0x1p-2", "0x1.8p1", "inf", "-Inf", "+Infinity", "NaN", "1_000.5", ".5", "5.", "1e", "1.2.3", "", "0.1", "0.30000000000000004",
		"123456789012345678901234567890", "2.2250738585072011e-308", "9007199254740993", "1e23", "8.41e21"} {
		show(strconv.ParseFloat(s, 64))
	}
	for _, s := range []string{"3.4028235e38", "3.4028236e38", "1e-46", "16777217", "0.1"} {
		show(strconv.ParseFloat(s, 32))
	}
	// Floats: formatting in every verb, shortest and fixed precision.
	for _, f := range []float64{0, 1, -1, 0.1, 1.0 / 3, 2.5, 123456789, 1e21, 1e20, 1e-7, 123456.789e-20, 5e-324, 1.7976931348623157e308, 100, 0.000001234} {
		println(strconv.FormatFloat(f, 'g', -1, 64), strconv.FormatFloat(f, 'e', -1, 64), strconv.FormatFloat(f, 'f', -1, 64),
			strconv.FormatFloat(f, 'f', 3, 64), strconv.FormatFloat(f, 'e', 2, 64), strconv.FormatFloat(f, 'g', 4, 64),
			strconv.FormatFloat(f, 'G', 10, 64), strconv.FormatFloat(f, 'b', -1, 64), strconv.FormatFloat(f, 'x', -1, 64),
			strconv.FormatFloat(f, 'X', 3, 64))
	}
	println(strconv.FormatFloat(0.1, 'g', -1, 32), strconv.FormatFloat(16777216, 'f', -1, 32), strconv.FormatFloat(3.4028235e38, 'e', -1, 32))
	big := 1e308
	inf := big * 10
	println(strconv.FormatFloat(inf, 'g', -1, 64), strconv.FormatFloat(-inf, 'f', 2, 64), strconv.FormatFloat(inf-inf, 'e', -1, 64))
	c, err := strconv.ParseComplex("1.5+2i", 128)
	println(real(c), imag(c), err == nil, strconv.FormatComplex(complex(1, -2), 'g', -1, 128))

	// Quoting, with unicode/utf8.
	for _, s := range []string{"", "plain", "tab\there", "quote\"back\\slash", "nul\x00bel\a", "héllo, 世界", "  \U0001F600", "bad\xffutf8", "`backquote`", "del\x7f", "\ufeff"} {
		println(strconv.Quote(s), strconv.QuoteToASCII(s), strconv.QuoteToGraphic(s), strconv.CanBackquote(s), utf8.ValidString(s), utf8.RuneCountInString(s))
	}
	println(strconv.QuoteRune('☺'), strconv.QuoteRuneToASCII('☺'), strconv.QuoteRune('\n'), strconv.QuoteRune(utf8.RuneError), strconv.QuoteRune(0x10FFFF+1))
	r, multibyte, tail, err := strconv.UnquoteChar(`☺bc`, '"')
	println(r, multibyte, tail, err == nil)
	for _, q := range []string{`"plain"`, `"tab\there"`, `"\u263a\U0001F600\x41\101"`, "`raw\\n`", `'x'`, `'\n'`, `"unterminated`, `"bad\q"`, `'ab'`, "\"nl\n\"", `""`, "``", `"\xff"`, "'\u00e9'"} {
		u, err := strconv.Unquote(q)
		println(u, err == nil, err == strconv.ErrSyntax)
	}
	for _, q := range []string{`"head" tail`, "`raw` rest", `'c'd`, `"open`, `x"y"`, `"esc\"aped" z`} {
		p, err := strconv.QuotedPrefix(q)
		println(p, err == nil)
	}
	println(strconv.IsPrint('a'), strconv.IsPrint('­'), strconv.IsGraphic('　'), strconv.IsPrint('　'))

	// Append forms.
	buf := []byte("x=")
	buf = strconv.AppendInt(buf, -12, 10)
	buf = append(buf, ' ')
	buf = strconv.AppendFloat(buf, 1.5e-3, 'g', -1, 64)
	buf = append(buf, ' ')
	buf = strconv.AppendQuote(buf, "q\n")
	buf = strconv.AppendBool(buf, true)
	buf = strconv.AppendQuoteRune(buf, 'é')
	buf = strconv.AppendUint(buf, 255, 16)
	println(string(buf))

	// The error values: wrapping, text, and As/Is through *NumError.
	_, err = strconv.Atoi("99999999999999999999")
	println(errors.Unwrap(err) == strconv.ErrRange, errors.Is(err, strconv.ErrRange), errors.Is(err, strconv.ErrSyntax), err.Error())
	_, err = strconv.ParseInt("x", 10, 64)
	var ne *strconv.NumError
	println(errors.As(err, &ne), ne.Func, ne.Num, ne.Err == strconv.ErrSyntax, ne.Unwrap() == strconv.ErrSyntax)
}
