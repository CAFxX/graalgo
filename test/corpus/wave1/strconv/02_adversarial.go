package main

import (
	"fmt"
	"strconv"
)

func main() {
	// NumError: verify error types and messages.
	_, err := strconv.ParseInt("xyz", 10, 64)
	if ne, ok := err.(*strconv.NumError); ok {
		fmt.Printf("NumError: Func=%q Num=%q Err=%v\n", ne.Func, ne.Num, ne.Err)
		fmt.Printf("ErrSyntax==ne.Err: %v\n", ne.Err == strconv.ErrSyntax)
	}
	_, err = strconv.ParseInt("99999999999999999999", 10, 64)
	if ne, ok := err.(*strconv.NumError); ok {
		fmt.Printf("Range: Err=%v (ErrRange=%v)\n", ne.Err, ne.Err == strconv.ErrRange)
	}

	// Error strings (must match gc exactly).
	fmt.Printf("ErrSyntax: %v\n", strconv.ErrSyntax)
	fmt.Printf("ErrRange: %v\n", strconv.ErrRange)

	// Adversarial: underscores in weird positions, base 0 with prefixes.
	for _, s := range []string{"_42", "42_", "0x_42", "0x42_", "0b_1", "0o_7", "1_.5", "_1.5"} {
		vi, ei := strconv.ParseInt(s, 0, 64)
		vf, ef := strconv.ParseFloat(s, 64)
		fmt.Printf("%q: ParseInt=%d,%v ParseFloat=%v,%v\n", s, vi, ei != nil, vf, ef != nil)
	}

	// Huge exponents, denormals, hex floats.
	for _, s := range []string{"1e1000", "1e-1000", "4.9e-324", "2.5e-324", "0x1.fffffffffffffp1023", "0x1p-1074"} {
		v, err := strconv.ParseFloat(s, 64)
		fmt.Printf("ParseFloat(%q) = %v, err=%v\n", s, v, err != nil)
	}

	// AppendQuote with all-escapes string.
	var dst []byte
	dst = strconv.AppendQuote(dst, "\x00\x01\x02\xff\xfe")
	dst = strconv.AppendQuoteRune(dst, '🙂')
	fmt.Printf("AppendQuote escapes: %q\n", string(dst))

	// FormatFloat special formats.
	fmt.Printf("%q\n", strconv.FormatFloat(1e21, 'f', -1, 64))
	fmt.Printf("%q\n", strconv.FormatFloat(1e21, 'e', -1, 64))
	fmt.Printf("%q\n", strconv.FormatFloat(123.456, 'g', 4, 64))
}
