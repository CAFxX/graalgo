// Targets deferred closures modifying named results: after a plain return,
// after recover, with shadowed results, multiple named results, results of
// struct/slice type, and the value seen by the caller when the defer
// modifies the result after the return expression is evaluated.
package main

import (
	"errors"
	"strconv"
)

func double() (r int) {
	defer func() { r *= 2 }()
	return 21
}

func unnamedNotAffected() int {
	r := 5
	defer func() { r *= 100 }()
	return r
}

func recoverSetsErr(fail bool) (n int, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.New("recovered: " + rec.(string))
			n = -1
		}
	}()
	n = 7
	if fail {
		panic("boom")
	}
	return n * 2, nil
}

func shadow() (x int) {
	defer func() { x++ }()
	{
		x := 50
		_ = x
	}
	return 9
}

type pair struct{ a, b int }

func structResult() (p pair) {
	defer func() { p.b = p.a + 1 }()
	return pair{a: 3, b: 0}
}

func sliceResult() (s []int) {
	defer func() { s = append(s, 99) }()
	s = []int{1}
	return append(s, 2)
}

func chain() (s string) {
	defer func() { s += "-d1" }()
	defer func() { s += "-d2" }()
	defer func() {
		if r := recover(); r != nil {
			s = "rec(" + strconv.Itoa(r.(int)) + ")"
		}
	}()
	s = "start"
	panic(42)
}

func returnFromRecoveredContinues() (out []string) {
	defer func() { out = append(out, "after-recover-defer") }()
	func() {
		defer func() {
			r := recover()
			out = append(out, "inner recovered "+r.(string))
		}()
		panic("inner")
	}()
	out = append(out, "continued")
	return out
}

func deferredReadsResult() (r int) {
	defer func() { println("deferred sees r =", r) }()
	r = 1
	return r + 10
}

func main() {
	println("double", double())
	println("unnamed", unnamedNotAffected())
	n, err := recoverSetsErr(false)
	println("ok", n, err == nil)
	n, err = recoverSetsErr(true)
	println("fail", n, err.Error())
	println("shadow", shadow())
	p := structResult()
	println("struct", p.a, p.b)
	s := sliceResult()
	for _, v := range s {
		print(v, " ")
	}
	println()
	println("chain", chain())
	for _, line := range returnFromRecoveredContinues() {
		println(line)
	}
	println("deferredReadsResult", deferredReadsResult())
}
