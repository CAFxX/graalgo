// Recovered panic values of every kind keep their dynamic type and value:
// complex64, named floats/bools, arrays, structs, pointers, funcs, errors
// whose Error method itself panics (recovered inside), nested re-panics
// replacing the value, and recover() identity of the boxed value.
package main

import (
	"errors"
	"math"
	"strconv"
)

type temp float32
type flag bool
type quad [4]int8
type rec struct {
	n int
	s string
}

type badErr struct{ code int }

func (b badErr) Error() string {
	if b.code < 0 {
		panic("Error() itself panicked")
	}
	return "bad " + strconv.Itoa(b.code)
}

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case complex64:
		return "complex64 " + strconv.FormatComplex(complex128(x), 'g', -1, 64)
	case complex128:
		return "complex128 " + strconv.FormatComplex(x, 'g', -1, 128)
	case temp:
		return "temp " + strconv.FormatFloat(float64(x), 'g', -1, 32)
	case flag:
		return "flag " + strconv.FormatBool(bool(x))
	case quad:
		return "quad " + strconv.Itoa(int(x[0])+int(x[3]))
	case [2]any:
		return "[2]any " + describe(x[0]) + "+" + describe(x[1])
	case rec:
		return "rec " + strconv.Itoa(x.n) + x.s
	case *rec:
		return "*rec " + x.s
	case func() int:
		return "func " + strconv.Itoa(x())
	case error:
		return "error " + safeErr(x)
	case string:
		return "string " + x
	case int:
		return "int " + strconv.Itoa(x)
	case float64:
		return "float64 " + strconv.FormatFloat(x, 'g', -1, 64)
	case uintptr:
		return "uintptr " + strconv.FormatUint(uint64(x), 10)
	default:
		return "other"
	}
}

func safeErr(e error) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "<Error panicked: " + r.(string) + ">"
		}
	}()
	return e.Error()
}

func main() {
	nan := math.NaN()
	vals := []any{
		complex64(complex(1, -2)),
		complex(nan, math.Inf(-1)),
		temp(-0.1),
		flag(true),
		quad{1, 2, 3, -4},
		[2]any{int8(1), "x"},
		rec{7, "r"},
		&rec{s: "p"},
		func() int { return 42 },
		badErr{3},
		badErr{-1},
		errors.New("plain"),
		"str",
		1 << 40,
		nan,
		uintptr(9),
	}
	for _, v := range vals {
		got := catch(func() { panic(v) })
		println(describe(got))
	}

	p := &rec{n: 1}
	got := catch(func() { panic(p) })
	println("pointer identity", got.(*rec) == p)
	q := quad{9}
	got = catch(func() { panic(q) })
	q[0] = 0
	println("array copied into panic", got.(quad)[0], got == any(quad{9}))

	got = catch(func() {
		defer func() { panic(rec{2, "second"}) }()
		panic(rec{1, "first"})
	})
	println("replaced", describe(got))

	got = catch(func() {
		defer func() {
			r := recover()
			panic([2]any{r, temp(2.5)})
		}()
		panic(complex64(3i))
	})
	println("wrapped", describe(got))

	got = catch(func() {
		var arr [3]int
		i := 5
		_ = arr[i%8]
	})
	println("runtime", describe(got))
	re, ok := got.(interface{ RuntimeError() })
	println("is runtime.Error", ok, re != nil)

	got = catch(func() {
		var e error = badErr{-2}
		_ = e.Error()
	})
	println("panicking Error", describe(got))

	got = catch(func() {
		var m map[rec]int
		m[rec{}] = 1
	})
	println("nil map write", describe(got))

	got = catch(func() {
		var x any = temp(1)
		_ = x.(flag)
	})
	println("assert", describe(got))
	got = catch(func() {
		var x any = quad{}
		_ = x.(interface{ Error() string })
	})
	println("assert iface", describe(got))

	got = catch(func() { panic(errors.Join(badErr{1}, errors.New("two"))) })
	println("joined", describe(got))
	e1 := badErr{5}
	var target badErr
	println("errors.As", errors.As(errors.Join(errors.New("x"), e1), &target), target.code)
	println("errors.Is struct", errors.Is(errors.Join(e1), badErr{5}), errors.Is(e1, badErr{6}))
	println("caught nil", catch(func() {}) == nil)
}
