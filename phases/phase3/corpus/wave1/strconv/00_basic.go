package main

import (
	"fmt"
	"strconv"
)

func main() {
	// ParseInt family: bases, bit sizes, edge values.
	for _, tc := range []struct {
		s       string
		base    int
		bitSize int
	}{
		{"0", 10, 64}, {"-0", 10, 64}, {"+42", 10, 64},
		{"9223372036854775807", 10, 64}, {"-9223372036854775808", 10, 64},
		{"9223372036854775808", 10, 64}, {"-9223372036854775809", 10, 64},
		{"ff", 16, 64}, {"FF", 16, 32}, {"0xff", 0, 64}, {"0o77", 0, 64},
		{"0b101", 0, 64}, {"1_000", 10, 64}, {"1__2", 10, 64},
		{"", 10, 64}, {"  42", 10, 64}, {"42  ", 10, 64},
		{"127", 10, 8}, {"128", 10, 8}, {"-129", 10, 8},
		{"32767", 10, 16}, {"32768", 10, 16},
	} {
		v, err := strconv.ParseInt(tc.s, tc.base, tc.bitSize)
		fmt.Printf("ParseInt(%q,%d,%d) = %d, err=%v\n", tc.s, tc.base, tc.bitSize, v, err != nil)
	}

	// ParseUint.
	for _, s := range []string{"0", "42", "18446744073709551615", "18446744073709551616", "-1", "+42"} {
		v, err := strconv.ParseUint(s, 10, 64)
		fmt.Printf("ParseUint(%q) = %d, err=%v\n", s, v, err != nil)
	}

	// Atoi/Itoa.
	for _, s := range []string{"0", "-42", "2147483647", "2147483648", "abc", ""} {
		v, err := strconv.Atoi(s)
		fmt.Printf("Atoi(%q) = %d, err=%v\n", s, v, err != nil)
	}
	for _, v := range []int{0, -42, 2147483647, -2147483648} {
		fmt.Printf("Itoa(%d) = %q\n", v, strconv.Itoa(v))
	}

	// ParseBool.
	for _, s := range []string{"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False", "yes", "", "2"} {
		v, err := strconv.ParseBool(s)
		fmt.Printf("ParseBool(%q) = %v, err=%v\n", s, v, err != nil)
	}

	// ParseFloat: special values, precision.
	for _, s := range []string{"0", "-0", "1.5", "-1.5", "1e10", "1E-10", "Inf", "-Inf", "+Inf", "NaN", "nan", "0x1p4", "1_000.5", "", "abc"} {
		v, err := strconv.ParseFloat(s, 64)
		fmt.Printf("ParseFloat(%q) = %v, err=%v\n", s, v, err != nil)
	}

	// Format functions.
	fmt.Printf("FormatInt(-42,16) = %q\n", strconv.FormatInt(-42, 16))
	fmt.Printf("FormatUint(42,2) = %q\n", strconv.FormatUint(42, 2))
	fmt.Printf("FormatFloat(1.5,'e',-1,64) = %q\n", strconv.FormatFloat(1.5, 'e', -1, 64))
	fmt.Printf("FormatFloat(0.1,'f',-1,64) = %q\n", strconv.FormatFloat(0.1, 'f', -1, 64))
	fmt.Printf("FormatBool(true) = %q\n", strconv.FormatBool(true))

	// Append functions.
	dst := strconv.AppendInt(nil, -42, 10)
	dst = strconv.AppendQuote(dst, "hi")
	fmt.Printf("Append = %q\n", string(dst))

	// IntSize.
	fmt.Printf("IntSize = %d\n", strconv.IntSize)
}
