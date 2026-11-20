// Targets exact runtime panic messages in recovered form: index and slice
// bounds (incl. 3-index slices and arrays via pointers), integer division
// by zero, nil map writes, negative make lengths, unsigned/shift panics,
// custom error and non-error panic values, re-panics and nested recovers.
package main

import (
	"errors"
	"strconv"
)

type myErr struct{ code int }

func (e *myErr) Error() string { return "myErr " + strconv.Itoa(e.code) }

type valErr struct{ s string }

func (e valErr) Error() string { return "valErr " + e.s }

func try(tag string, f func()) {
	defer func() {
		r := recover()
		switch v := r.(type) {
		case nil:
			println(tag, "-> no panic")
		case error:
			println(tag, "-> error:", v.Error())
		case string:
			println(tag, "-> string:", v)
		case int:
			println(tag, "-> int:", v)
		default:
			println(tag, "-> other")
		}
	}()
	f()
}

//go:noinline
func n(v int) int { return v }

//go:noinline
func u(v uint) uint { return v }

func main() {
	s := []int{1, 2, 3}
	a := [4]int{}
	pa := &a
	str := "héllo"
	try("idx", func() { _ = s[n(5)] })
	try("idxneg", func() { _ = s[n(-1)] })
	try("arr", func() { _ = pa[n(4)] })
	try("str", func() { _ = str[n(10)] })
	try("slice-hi", func() { _ = s[1:n(4)] })
	try("slice-lohi", func() { _ = s[n(3):n(2)] })
	try("slice-cap", func() { _ = s[0:2:n(9)] })
	try("slice3", func() { _ = s[n(2):n(1):3] })
	try("slice3b", func() { _ = s[0:n(3):n(2)] })
	try("arrslice", func() { _ = pa[n(2):n(5)] })
	try("strslice", func() { _ = str[n(3):n(100)] })
	try("strslice2", func() { _ = str[n(4):n(3)] })
	try("ok-slice", func() { println(len(s[3:]), len(str[6:])) })
	try("div", func() { println(10 / n(0)) })
	try("mod", func() { println(10 % n(0)) })
	try("udiv", func() { println(u(7) / u(0)) })
	try("int8-div", func() {
		var z int8
		z = int8(n(0))
		println(int8(5) / z)
	})
	try("float-div", func() {
		f := float64(n(0))
		println(1/f, -1/f)
	})
	try("nilmap", func() {
		var m map[string]int
		println(m["x"], len(m))
		m["x"] = 1
	})
	try("makeneg", func() { _ = make([]int, n(-1)) })
	try("makecap", func() { _ = make([]int, n(5), n(2)) })
	try("shift", func() { println(1 << n(-1)) })
	try("shift-ok", func() { println(1<<n(70), -1>>n(70), uint8(1)<<n(8)) })
	try("custom-ptr", func() { panic(&myErr{42}) })
	try("custom-val", func() { panic(valErr{"v"}) })
	try("errors", func() { panic(errors.New("plain")) })
	try("wrapped", func() { panic(errors.Join(errors.New("a"), errors.New("b"))) })
	try("string", func() { panic("just a string") })
	try("int", func() { panic(7) })
	try("float", func() { panic(1.5) })
	try("repanic", func() {
		defer func() {
			r := recover()
			panic("re:" + r.(string))
		}()
		panic("first")
	})
	try("nested", func() {
		defer func() {
			println("outer defer sees", recover() == nil)
		}()
		func() {
			defer func() {
				r := recover()
				println("inner recovered:", r.(error).Error())
			}()
			_ = s[n(10)]
		}()
		println("after inner")
	})
	try("defer-order", func() {
		for i := range 3 {
			defer func() { print("d", i, " ") }()
		}
		defer println()
		panic("go")
	})
	println()
	try("conv-slice-arr", func() {
		arr := [4]int(s[:n(3)])
		_ = arr
	})
	try("conv-ok", func() {
		arr := [2]int(s[:n(2)])
		arr[0] = 100
		println(arr[0], s[0])
	})
	try("append-nil", func() {
		var ns []int
		ns = append(ns, 1)
		println(len(ns), ns[0])
		var np *[3]int
		_ = np[:]
	})
	try("type-assert-in-defer", func() {
		defer func() {
			if r := recover(); r != nil {
				_ = r.(int)
			}
		}()
		panic("not int")
	})
	var e error = &myErr{1}
	var target *myErr
	println(errors.As(e, &target), target.code, errors.Is(e, e))
}
