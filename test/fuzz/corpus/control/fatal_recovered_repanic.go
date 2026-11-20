// Targets the fatal printout after recover() followed by re-panic of the
// same value inside the deferred func: gc marks the first entry
// "[recovered, repanicked]" instead of printing a second line; exit 2.
package main

import "errors"

var errBase = errors.New("base failure")

func work(n int) (res int) {
	defer func() {
		if r := recover(); r != nil {
			println("work saw panic, n =", n)
			if n > 2 {
				panic(r)
			}
			res = -n
		}
	}()
	if n > 0 {
		panic(errBase)
	}
	return 1
}

func main() {
	for n := 0; n < 3; n++ {
		println("work", n, work(n))
	}
	work(3)
	println("unreachable")
}
