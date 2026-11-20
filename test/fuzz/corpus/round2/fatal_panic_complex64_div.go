// Fatal: an unrecovered panic whose value is a complex64 computed by a
// complex64 division (rounded once from float64) after recovered warm-ups
// that round-trip complex64 panic values; gc prints "panic: (re+imi)",
// a goroutine trace, and exits 2.
package main

import (
	"math"
	"strconv"
)

//go:noinline
func div(a, b complex64) complex64 { return a / b }

//go:noinline
func mul(a, b complex64) complex64 { return a * b }

// bits hides the sign/payload of NaN, which Go leaves unspecified.
func bits(f float32) string {
	if f != f {
		return "nan"
	}
	return strconv.FormatUint(uint64(math.Float32bits(f)), 16)
}

func attempt(c complex64) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			v := r.(complex64)
			msg = "recovered " + strconv.FormatComplex(complex128(v), 'g', -1, 64) +
				" bits " + bits(real(v)) + "/" + bits(imag(v))
		}
	}()
	panic(c)
}

func main() {
	acc := complex64(complex(1, 1))
	for i := 1; i <= 6; i++ {
		acc = div(mul(acc, complex(float32(i), 0.5)), complex(0.3, float32(i)))
		println(attempt(acc))
	}
	inf := float32(math.Inf(1))
	println(attempt(div(complex(inf, 1), complex(1, 1))))
	println(attempt(div(1, 0)))
	println(attempt(mul(complex(inf, 0), complex(0, 1))))

	var boxed any = acc
	var other any = complex128(acc)
	println("boxed eq", boxed == any(acc), boxed == other)
	m := map[any]int{boxed: 1, other: 2}
	println("map kinds", len(m), m[acc], m[complex128(acc)])

	tiny := complex64(complex(1e-30, 1e-30))
	println(attempt(mul(tiny, tiny)))
	println(attempt(div(tiny, complex(1e30, 1e30))))

	println("about to panic")
	panic(div(complex(1, 2), complex(3, -7)))
}
