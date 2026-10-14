package main

// Package-level variable inc/dec.
//
// Regression test (M8 closeout, mutation M-J01/M-J02): the inc/dec code path
// for package-level variables (NodeFactory, gslot branch) swapped ADD/SUB
// and the suite stayed green — no test exercised i++/i-- on a global.
import "fmt"

var counter = 10
var fcounter = 1.5

func main() {
	counter++
	fmt.Println(counter) // 11
	counter--
	counter--
	fmt.Println(counter) // 9
	fcounter++
	fmt.Println(fcounter) // 2.5
}

