package main

import "fmt"

// R2313-P1-2: the target expression of a losing select receive case
// (here the side-effecting index idx()) must never be evaluated: gc
// evaluates a recv-case target only when the case is chosen. The
// copy-back desugar preserves this timing by placing the copy-back
// inside the case body. If a refactor hoisted the target export or
// the copy-back to select entry, idx() would run and "calls" would
// print 1 instead of 0.

var calls int

func idx() int {
	calls++
	return 0
}

func main() {
	win := make(chan int, 1)
	win <- 7
	lose := make(chan int)
	buf := [1]int{}
	got := -1
	select {
	case buf[idx()] = <-lose:
		fmt.Println("losing case chosen")
	case got = <-win:
		fmt.Println("winner", got, buf)
	default:
		fmt.Println("default")
	}
	fmt.Println("calls", calls)
}
