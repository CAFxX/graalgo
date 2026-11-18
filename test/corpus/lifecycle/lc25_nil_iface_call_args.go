package main

// A method call on a nil interface evaluates its arguments first and
// panics at the call (gc order): the argument's side effect happens
// before the nil-dereference panic.

import "fmt"

type I interface{ M(int, int) int }

func arg(n int) int {
	fmt.Println("arg", n, "evaluated")
	return n
}

func main() {
	defer func() { fmt.Println("recovered:", recover()) }()
	var i I
	fmt.Println(i.M(arg(1), arg(2)))
}
