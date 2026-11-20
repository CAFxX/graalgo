// strconv.ParseFloat edge cases at bitSize 32 and 64: inf/nan spellings,
// hex floats (with and without p exponent), underscores, overflow to ±Inf
// with ErrRange, underflow to 0/denormals, halfway rounding, very long
// mantissas, and syntax errors — printing value bits and err.Error().
package main

import (
	"errors"
	"math"
	"strconv"
)

var ins = []string{"0", "-0", "+0", "1", "-1", ".5", "5.", ".", "-.", "1e", "1e+", "1e-5", "1E5", "1e400", "-1e400", "1e-400", "-1e-400",
	"inf", "Inf", "INF", "+inf", "-inf", "infinity", "-Infinity", "+INFINITY", "infin", "nan", "NaN", "NAN", "-nan", "+nan", "nan(1)",
	"0x1p0", "0x1P-2", "0x1.8p1", "-0x.1p4", "0x1", "0x1.8", "0xp1", "0x1p", "0x1p+1024", "0x1p-1074", "0x1p-1075", "0x1.0000000000001p-1075",
	"0x1.fffffffffffff8p1023", "0x1.fffffep127", "0x1.ffffffp127", "0x1p-149", "0x1p-150", "0x1.1p-150",
	"1_000.5", "1__0", "_1", "1_", "0x_1p0", "0x1_0p0", "1_0e1_0", "1.5_", "0_1", "01.5", "1e+0_1",
	"4.9406564584124654e-324", "2.4703282292062327e-324", "2.4703282292062328e-324", "2.2250738585072011e-308", "2.2250738585072012e-308",
	"1.7976931348623157e308", "1.7976931348623158e308", "1.7976931348623159e308", "1.797693134862315808e308",
	"3.4028234663852886e38", "3.4028235677973366e38", "3.4028235677973367e38", "1.401298464324817e-45", "7.006492321624085e-46", "7.006492321624086e-46",
	"0.1", "0.30000000000000004", "9007199254740993", "9007199254740993.0000000000000001", "123456789012345678901234567890",
	"0.000000000000000000000000000000000000000000001", "1" + zeros(400) + "e-400", "0." + zeros(400) + "1e400",
	"1.00000000000000011102230246251565404236316680908203125", "1.00000000000000011102230246251565404236316680908203124",
	"1.00000000000000011102230246251565404236316680908203126", " 1", "1 ", "", "+", "-", "1e1e1", "0x", "1.5e-2147483649", "1e2147483648",
	"1.0e+00000000000000000000000000000000000000002", "١", "1,5", "0b101", "0o7"}

func zeros(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

func fb(x float64) string {
	if x != x {
		return "NaN"
	}
	return strconv.FormatFloat(x, 'g', -1, 64) + "/" + strconv.FormatUint(math.Float64bits(x), 16)
}

func perr(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error() + " range=" + strconv.FormatBool(errors.Is(err, strconv.ErrRange)) + " syntax=" + strconv.FormatBool(errors.Is(err, strconv.ErrSyntax))
}

func main() {
	for _, s := range ins {
		q := s
		if len(q) > 60 {
			q = q[:30] + "..." + q[len(q)-10:]
		}
		v64, err := strconv.ParseFloat(s, 64)
		println("p64", strconv.Quote(q), fb(v64), perr(err))
		v32, err := strconv.ParseFloat(s, 32)
		f := float32(v32)
		println("p32", strconv.Quote(q), fb(v32), math.Float32bits(f), float64(f) == v32 || v32 != v32, perr(err))
		if err == nil && v32 == v32 {
			println("  f32", f, strconv.FormatFloat(v32, 'g', -1, 32))
		}
	}
	// 40-digit exact decimal expansions of consecutive float64s round-trip
	base := 1.0
	for i := 0; i < 8; i++ {
		lo := base
		hi := math.Nextafter(lo, 2)
		ex := strconv.FormatFloat(lo, 'e', 40, 64)
		v, _ := strconv.ParseFloat(ex, 64)
		println("rt40", i, ex, fb(v), fb(hi), v == lo)
		base = hi
	}
	// ParseFloat of FormatFloat for many 32-bit patterns
	bad := 0
	for b := uint32(0); b < 0x7f800000; b += 0x00f0f0f1 {
		f := math.Float32frombits(b)
		for _, fm := range []byte{'e', 'g', 'f', 'x', 'b'} {
			s := strconv.FormatFloat(float64(f), fm, -1, 32)
			v, err := strconv.ParseFloat(s, 32)
			if fm == 'b' {
				continue
			}
			if err != nil || float32(v) != f {
				bad++
				println("bad32", b, s)
			}
		}
	}
	println("bad32 total", bad)
	_, err := strconv.ParseFloat("1", 16)
	println("bitsize16", perr(err))
	v, err := strconv.ParseFloat("1e39", 32)
	println("f32ovf", fb(v), perr(err))
}
