// strconv integer formatting and parsing: FormatInt/FormatUint/AppendInt in
// every base 2..36 for edge values, Itoa, ParseInt/ParseUint with base 0
// prefixes and underscores, bitSize range errors (err.Error() printed and
// clamped values), syntax errors, Atoi errors, and invalid-base panics.
package main

import (
	"errors"
	"strconv"
)

var ints = []int64{0, 1, -1, 7, -8, 35, 36, -36, 127, -128, 255, 1000, -1000, 2147483647, -2147483648,
	9223372036854775807, -9223372036854775808, 0x5555555555555555, -0x123456789}
var uints = []uint64{0, 1, 35, 36, 1295, 1296, 4294967295, 18446744073709551615, 1 << 63, 12345678901234567890}

func catch(name string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			println(name, "no panic")
			return
		}
		if e, ok := r.(interface{ Error() string }); ok {
			println(name, "panic:", e.Error())
		} else if s, ok := r.(string); ok {
			println(name, "panic str:", s)
		} else {
			println(name, "panic other")
		}
	}()
	f()
}

func perr(err error) string {
	if err == nil {
		return "nil"
	}
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		return err.Error() + " [func=" + ne.Func + " num=" + ne.Num + " range=" + strconv.FormatBool(errors.Is(err, strconv.ErrRange)) + " syntax=" + strconv.FormatBool(errors.Is(err, strconv.ErrSyntax)) + "]"
	}
	return err.Error()
}

func main() {
	for _, x := range ints {
		for base := 2; base <= 36; base++ {
			s := strconv.FormatInt(x, base)
			back, err := strconv.ParseInt(s, base, 64)
			println("fi", x, base, s, back == x, perr(err))
		}
		println("itoa", strconv.Itoa(int(x)), string(strconv.AppendInt([]byte("x="), x, 16)), strconv.Quote(strconv.FormatInt(x, 36)))
	}
	for _, u := range uints {
		for _, base := range []int{2, 3, 7, 8, 10, 16, 32, 36} {
			s := strconv.FormatUint(u, base)
			back, err := strconv.ParseUint(s, base, 64)
			println("fu", u, base, s, back == u, perr(err))
		}
	}
	ins := []string{"0", "-0", "+0", "0x1F", "0X1f", "-0x80", "0b101", "0B11", "0o17", "0O17", "017", "08", "0_1", "1_000", "1__0", "_1", "1_",
		"0x_ff", "0x", "0b", "0o", "+", "-", "", " 1", "1 ", "1e3", "9223372036854775807", "9223372036854775808", "-9223372036854775808",
		"-9223372036854775809", "18446744073709551615", "18446744073709551616", "99999999999999999999999", "-0b1111111", "-0b10000001",
		"0x7fffffff", "0x80000000", "-0x80000000", "-0x80000001", "١٢", "0xg", "++1", "--1", "0x-1", "-_1", "0_x1"}
	for _, s := range ins {
		for _, bs := range []int{0, 8, 16, 32, 64} {
			v, err := strconv.ParseInt(s, 0, bs)
			println("pi0", strconv.Quote(s), bs, v, perr(err))
		}
		u, err := strconv.ParseUint(s, 0, 64)
		println("pu0", strconv.Quote(s), u, perr(err))
		v10, err := strconv.ParseInt(s, 10, 64)
		println("pi10", strconv.Quote(s), v10, perr(err))
		a, err := strconv.Atoi(s)
		println("atoi", strconv.Quote(s), a, perr(err))
	}
	others := []struct {
		s    string
		base int
	}{{"zz", 36}, {"ZZ", 36}, {"10", 2}, {"2", 2}, {"777", 8}, {"8", 8}, {"ff", 16}, {"fg", 16}, {"1y2p0ij32e8e7", 36}, {"1y2p0ij32e8e8", 36}, {"-1y2p0ij32e8e8", 36}}
	for _, o := range others {
		v, err := strconv.ParseInt(o.s, o.base, 64)
		u, err2 := strconv.ParseUint(o.s, o.base, 64)
		println("pb", o.s, o.base, v, perr(err), u, perr(err2))
	}
	_, err := strconv.ParseInt("1", 1, 64)
	println("base1", perr(err))
	_, err = strconv.ParseInt("1", 37, 64)
	println("base37", perr(err))
	_, err = strconv.ParseInt("1", 10, 65)
	println("bits65", perr(err))
	_, err = strconv.ParseUint("1", 10, -1)
	println("bits-1", perr(err))
	catch("FormatInt base1", func() { strconv.FormatInt(5, 1) })
	catch("FormatInt base37", func() { strconv.FormatUint(5, 37) })
	catch("AppendInt base0", func() { strconv.AppendInt(nil, 5, 0) })
	long := "123456789012345678901234567890123456789012345678901234567890"
	_, err = strconv.Atoi(long)
	println("long", perr(err))
	println("quote", strconv.QuoteRune(0x10ffff), strconv.QuoteRuneToASCII('世'), strconv.FormatBool(true))
}
