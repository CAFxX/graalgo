// Targets package initialization order: package-level vars initialized in
// dependency order (not declaration order) across functions and methods,
// multiple init() functions in one file run in source order after all vars,
// and side effects recorded during initialization.
package main

import "strconv"

var trace []string

func note(s string, v int) int {
	trace = append(trace, s+"="+strconv.Itoa(v))
	return v
}

// declared before its dependencies
var a = note("a", b+c)
var b = note("b", f())
var c = note("c", 3)
var d, e = pair()
var total = note("total", sumAll())
var z = note("z", w*2)
var w = note("w", T{}.m())
var arr = [3]int{note("arr0", c), note("arr1", 1), note("arr2", b)}
var sl = []string{x, y}
var x = "x" + y
var y = "y" + strconv.Itoa(note("y", 7))
var m = map[string]int{"a": a, "z": z}
var mFunc = func() int { return note("mFunc", len(m)) }()
var _ = note("blank", 42)
var ptr = &arr[1]
var structVal = S{A: note("S.A", 10), B: &c}

type S struct {
	A int
	B *int
}

type T struct{}

func (T) m() int { return note("T.m", c+100) }

func f() int { return note("f", c*2) }

func pair() (int, int) {
	note("pair", 0)
	return a + 1, a + 2
}

func sumAll() int { return a + b + c + d + e }

var initCount int

func init() {
	initCount++
	trace = append(trace, "init1:"+strconv.Itoa(a)+","+strconv.Itoa(initCount))
	*ptr += 1000
}

func init() {
	initCount++
	trace = append(trace, "init2:"+strconv.Itoa(arr[1])+","+strconv.Itoa(initCount))
	structVal.A++
	*structVal.B = -c
}

var late = note("late", initCount) // runs before any init()

func init() {
	initCount++
	trace = append(trace, "init3:late="+strconv.Itoa(late)+",c="+strconv.Itoa(c))
}

func main() {
	for i, t := range trace {
		println(i, t)
	}
	println(a, b, c, d, e, total, z, w)
	println(arr[0], arr[1], arr[2], len(sl), sl[0], sl[1], x, y)
	println(m["a"], m["z"], mFunc, structVal.A, *structVal.B, initCount, late)
}
