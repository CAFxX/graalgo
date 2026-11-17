// 57_defer_order: defer LIFO order, argument evaluation at defer time,
// deferred closures observing later writes, defers modifying named
// results, defers in loops, recover only in the deferred call, re-panic,
// and panics in deferred calls replacing the original.
package main

import (
	"errors"
	"fmt"
)

func order() {
	for i := 0; i < 4; i++ {
		defer fmt.Println("loop defer", i)
	}
	x := 1
	defer fmt.Println("arg at defer time", x)
	defer func() { fmt.Println("closure sees", x) }()
	x = 2
}

func named() (n int, err error) {
	defer func() {
		n *= 10
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
		}
	}()
	defer func() { n += 3 }()
	n = 1
	panic("boom")
}

func noRecoverOutsideDefer() (s string) {
	defer func() {
		r := recover()
		s = fmt.Sprint("outer got ", r)
	}()
	func() {
		defer func() {
			helper := func() any { return recover() }
			fmt.Println("nested helper recover:", helper())
		}()
		panic("first")
	}()
	return "unreached?"
}

func replace() (msg string) {
	defer func() { msg = fmt.Sprint(recover()) }()
	defer func() { panic("second") }()
	panic("first")
}

func rethrow() (out string) {
	defer func() { out = fmt.Sprint("final: ", recover()) }()
	defer func() {
		r := recover()
		panic(fmt.Sprint("re-", r))
	}()
	panic(errors.New("orig"))
}

func main() {
	order()
	fmt.Println(named())
	fmt.Println(noRecoverOutsideDefer())
	fmt.Println(replace())
	fmt.Println(rethrow())
	cnt := 0
	func() {
		defer func() { cnt++ }()
		defer func() { cnt *= 2 }()
		cnt = 5
	}()
	fmt.Println("cnt", cnt)
}
