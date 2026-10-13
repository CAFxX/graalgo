package main

import "fmt"

type SyntaxError struct {
	msg string
	pos int
}

func (e SyntaxError) Error() string { return fmt.Sprintf("%s at %d", e.msg, e.pos) }

type CodeError int

func (e CodeError) Error() string { return fmt.Sprintf("code %d", int(e)) }

func check(n int) error {
	if n < 0 {
		return nil
	}
	if n%2 == 0 {
		return SyntaxError{"bad token", n}
	}
	return CodeError(n)
}

func main() {
	for i := -1; i < 3; i++ {
		err := check(i)
		if err == nil {
			fmt.Println("nil")
		} else {
			fmt.Println(err.Error())
		}
	}
	// A nil error compared through the interface stays nil.
	var e error
	fmt.Println(e == nil)
}
