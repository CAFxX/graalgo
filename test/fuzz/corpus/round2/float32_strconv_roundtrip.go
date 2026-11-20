// float32 printing and strconv round trips: FormatFloat(x,'g',-1,32),
// ParseFloat(s,32) rounding of halfway/denormal inputs, println of float32
// and complex64, complex64<->complex128 conversions that round each part.
package main

import (
	"math"
	"strconv"
	"strings"
)

func rt(x float32) {
	s := strconv.FormatFloat(float64(x), 'g', -1, 32)
	y, err := strconv.ParseFloat(s, 32)
	ok := err == nil && math.Float32bits(float32(y)) == math.Float32bits(x)
	if math.IsNaN(float64(x)) {
		ok = math.IsNaN(y)
	}
	println(s, strconv.FormatFloat(float64(x), 'e', -1, 32),
		strings.Replace(strconv.FormatFloat(float64(x), 'x', -1, 32), "0x", "hex:", 1), ok)
}

func parse32(s string) {
	f, err := strconv.ParseFloat(s, 32)
	e := "nil"
	if err != nil {
		e = err.Error()
	}
	println("parse32", strings.Replace(s, "0x", "hex:", 1), math.Float32bits(float32(f)), strconv.FormatFloat(f, 'g', -1, 64), e)
}

//go:noinline
func narrow(c complex128) complex64 { return complex64(c) }

//go:noinline
func widen(c complex64) complex128 { return complex128(c) }

func main() {
	vals := []float32{
		0, float32(math.Copysign(0, -1)), 1, 0.1, 1.0 / 3, 16777216, 16777217,
		math.MaxFloat32, math.SmallestNonzeroFloat32, -math.SmallestNonzeroFloat32 * 3,
		1.17549435e-38, 1e-45, 3.4028235e38, 2.5e-41, 123456789, 0.000123456,
		float32(math.Inf(-1)), float32(math.NaN()), 9.999999e9, 1e10, 5e-324 * 0,
	}
	for _, v := range vals {
		rt(v)
	}
	for i := uint32(0x3f800000); i < 0x3f800000+5; i++ {
		rt(math.Float32frombits(i))
	}
	rt(math.Float32frombits(0x00000001))
	rt(math.Float32frombits(0x007fffff))
	rt(math.Float32frombits(0x7f7fffff))

	parse32("16777217")
	parse32("16777219")
	parse32("3.4028235e38")
	parse32("3.4028236e38")
	parse32("3.40282357e38")
	parse32("1e39")
	parse32("1e-46")
	parse32("7e-46")
	parse32("0x1.fffffep127")
	parse32("0x1.ffffffp127")
	parse32("1.00000005960464477539062499")
	parse32("1.000000059604644775390625")
	parse32("1.00000017881393432617187501")
	parse32("-0")
	parse32("inf")
	parse32("1_000.5")
	parse32(".e1")

	println("println f32", float32(0.1), float32(1e20), float32(1)/3, float32(math.Copysign(0, -1)))
	println("println f64", 0.1, 1e21, 1e20, 1.0/3, 100.0, 1e-7)

	c := complex(0.1, 1e-40)
	n := narrow(c)
	println("narrow", math.Float32bits(real(n)), math.Float32bits(imag(n)))
	w := widen(n)
	println("widen", strconv.FormatComplex(w, 'g', -1, 128))
	println("narrow overflow", strconv.FormatComplex(widen(narrow(complex(1e39, -1e39))), 'g', -1, 128))
	println("narrow nan", strconv.FormatComplex(widen(narrow(complex(math.NaN(), 1e-50))), 'g', -1, 128))
	println("fmtc64", strconv.FormatComplex(complex(1.0/3, 2.0/3), 'g', -1, 64))
	println("fmtc64 f", strconv.FormatComplex(complex(1.0/3, -2.0/3), 'f', 4, 64))
	pc, err := strconv.ParseComplex("(1.00000005960464477539062501+3e38i)", 64)
	println("parsec64", math.Float32bits(float32(real(pc))), math.Float32bits(float32(imag(pc))), err == nil)
	_, err = strconv.ParseComplex("(1+4e38i)", 64)
	println("parsec64 err", err.Error())

	var sum float32
	for i := 1; i <= 1000; i++ {
		sum += 1 / float32(i)
	}
	println("harmonic f32", strconv.FormatFloat(float64(sum), 'g', -1, 32))
	h := float32(0.1)
	println("f32 ops", h*3 == 0.3, h+0.2 == float32(0.3), float64(h) == 0.1)
	println("f32->int", int32(h*21474835200), int64(h*-9.2233715e19), uint8(h*2559))
	println("f32 fma-ish", strconv.FormatFloat(float64(h*h+h), 'g', -1, 32))
	println("AppendFloat", string(strconv.AppendFloat([]byte("x="), float64(float32(2.5e-8)), 'G', 3, 32)))
}
