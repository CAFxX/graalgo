// recover_compiled: recover() keeps working once the deferred function is
// compiled (BUG-0049: an identity test on the argument array failed in
// compiled code, which hands the callee a copy under argument type
// speculation). 40,000 recovered panics through a deferred closure, a
// deferred named function and a deferred method value (a wrapper frame); and
// recover() in a function the deferred call calls stays nil.
package main

import "fmt"

type T struct{ n int }

func (t *T) rec(e *any) { *e = recover() }

func f(v int) int {
	if v%2 == 0 {
		panic(v)
	}
	return v
}

func rec(e *any) { *e = recover() }

func nested(e *any) { *e = inner() }

func inner() any { return recover() }

func viaClosure(v int) (r int, err any) {
	defer func() { err = recover() }()
	return f(v), nil
}

func viaFunc(v int) (r int, err any) {
	defer rec(&err)
	return f(v), nil
}

func viaMethod(t *T, v int) (r int, err any) {
	defer t.rec(&err)
	return f(v), nil
}

func viaNested(v int) (err any) {
	defer func() {
		if r := recover(); r != nil && err == nil {
			err = fmt.Sprint("outer ", r)
		}
	}()
	defer nested(&err)
	f(v)
	return nil
}

func main() {
	t := &T{}
	counts := [4]int{}
	for k := 0; k < 40000; k++ {
		if _, err := viaClosure(k); err != nil {
			counts[0]++
		}
		if _, err := viaFunc(k); err != nil {
			counts[1]++
		}
		if _, err := viaMethod(t, k); err != nil {
			counts[2]++
		}
		if err := viaNested(k); err != nil && fmt.Sprint(err)[:5] == "outer" {
			counts[3]++
		}
	}
	fmt.Println(counts)
}
