// Targets the fatal panic chain printout when a deferred call panics while
// an earlier panic is still in flight and nothing recovers: gc prints
// "panic: A" then "\tpanic: B" (and further nested levels), exit status 2.
package main

import "strconv"

type codeErr struct{ code int }

func (e codeErr) Error() string { return "code " + strconv.Itoa(e.code) }

func level2() {
	defer func() {
		println("level2 defer runs, panicking third")
		panic(codeErr{3})
	}()
	panic("second")
}

func level1() {
	defer func() {
		println("level1 defer runs")
		level2()
	}()
	panic("first")
}

func main() {
	defer println("main defer still runs")
	sum := 0
	for i := 0; i < 10; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					sum += r.(int)
				}
			}()
			panic(i)
		}()
	}
	println("recovered sum", sum)
	level1()
}
