// Targets strconv: IsPrint/IsGraphic sweeps over the full rune range
// (checksums + boundary samples), FormatBool/ParseBool, NumError fields,
// Error()/Unwrap() for ParseInt/ParseUint/ParseFloat/Atoi range and syntax
// errors, base prefixes and underscores, FormatInt in every base 2..36.
package main

import (
	"errors"
	"math"
	"strconv"
)

func errs(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func main() {
	var hp, hg uint64 = 14695981039346656037, 14695981039346656037
	np, ng := 0, 0
	prevP, prevG := false, false
	trans := 0
	for r := rune(-2); r <= 0x110002; r++ {
		p := strconv.IsPrint(r)
		g := strconv.IsGraphic(r)
		if p {
			np++
			hp = (hp ^ uint64(r)) * 1099511628211
		}
		if g {
			ng++
			hg = (hg ^ uint64(r)) * 1099511628211
		}
		if p != prevP || g != prevG {
			trans++
			if trans%97 == 1 {
				print(r, ":", p, g, " ")
			}
		}
		prevP, prevG = p, g
	}
	println()
	println("print", np, hp, "graphic", ng, hg, "trans", trans)
	for _, r := range []rune{' ', '\u00a0', '\u00ad', '\u2028', '\u3000', '\ufeff', '\U000E0020', 0xfffe, 0x1fffe, 0x10fffd} {
		print(strconv.IsPrint(r), strconv.IsGraphic(r), " ")
	}
	println()

	for _, s := range []string{"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False", "", "yes", "tRUE", " true"} {
		b, err := strconv.ParseBool(s)
		println(strconv.Quote(s), b, errs(err))
	}
	println(strconv.FormatBool(true), strconv.FormatBool(false), string(strconv.AppendBool([]byte("x="), true)))

	ints := []string{"0", "-0", "+5", "0x1F", "0X1f", "0b101", "0o17", "017", "0_1", "1_000", "1__0", "_1", "1_",
		"9223372036854775807", "9223372036854775808", "-9223372036854775808", "-9223372036854775809",
		"18446744073709551615", "18446744073709551616", "0x", "-", "", "1e3", " 1", "0x_1F", "0b", "zz"}
	for _, s := range ints {
		v, err := strconv.ParseInt(s, 0, 64)
		u, uerr := strconv.ParseUint(s, 0, 64)
		v8, err8 := strconv.ParseInt(s, 10, 8)
		println(strconv.Quote(s), v, errs(err), u, errs(uerr), v8, errs(err8))
	}
	for _, base := range []int{-1, 1, 37, 36, 2} {
		v, err := strconv.ParseInt("101", base, 64)
		println("base", base, v, errs(err))
	}
	_, err := strconv.ParseInt("1", 10, 65)
	println(errs(err))
	_, err = strconv.ParseUint("300", 10, 8)
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		println("NumError", ne.Func, strconv.Quote(ne.Num), ne.Err.Error(), ne.Err == strconv.ErrRange, errors.Is(err, strconv.ErrRange), errors.Unwrap(err) == strconv.ErrRange)
	}
	_, err = strconv.Atoi("12a")
	if errors.As(err, &ne) {
		println("NumError", ne.Func, strconv.Quote(ne.Num), errors.Is(err, strconv.ErrSyntax))
	}
	a, err := strconv.Atoi("-99999999999999999999")
	println(a, errs(err))
	for _, s := range []string{"1.5", "-0", "inf", "-Inf", "+INF", "nan", "NaN", "1e309", "-1e309", "4.9e-324", "2e-324", "0x1p-2", "0x1.8p1",
		"1_000.5", "1e", ".", "1.e1", ".5", "0x1p", "1e+", "3.4028236e38", "1e-46", "+-1"} {
		f, err := strconv.ParseFloat(s, 64)
		f32, err32 := strconv.ParseFloat(s, 32)
		println(strconv.Quote(s), math.Float64bits(f), errs(err), math.Float64bits(f32), errs(err32))
	}
	var h uint64 = 14695981039346656037
	vals := []int64{0, 1, -1, 35, -36, 255, math.MaxInt64, math.MinInt64, 123456789, -987654321}
	for base := 2; base <= 36; base++ {
		for _, v := range vals {
			s := strconv.FormatInt(v, base)
			for i := 0; i < len(s); i++ {
				h = (h ^ uint64(s[i])) * 1099511628211
			}
			back, err := strconv.ParseInt(s, base, 64)
			if err != nil || back != v {
				println("roundtrip fail", base, v)
			}
			us := strconv.FormatUint(uint64(v), base)
			for i := 0; i < len(us); i++ {
				h = (h ^ uint64(us[i])) * 1099511628211
			}
		}
	}
	println("bases", h, strconv.FormatInt(math.MinInt64, 2), strconv.FormatUint(math.MaxUint64, 36), strconv.FormatInt(-35, 36))
	println(string(strconv.AppendInt(nil, -42, 7)), string(strconv.AppendUint([]byte("u"), 42, 16)), strconv.Itoa(math.MinInt64))
	for _, b := range []int{1, 37} {
		func() {
			defer func() {
				r := recover()
				if s, ok := r.(string); ok {
					println("formatint panic", s)
				} else if e, ok := r.(interface{ Error() string }); ok {
					println("formatint panic", e.Error())
				}
			}()
			println(strconv.FormatInt(5, b))
		}()
	}
}
