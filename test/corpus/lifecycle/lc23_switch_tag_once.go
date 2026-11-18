package main

// An expression switch evaluates its tag exactly once, also when the
// cases are interface-typed and the tag is concrete (Go compares the
// boxed tag with each case; the tag expression is never re-run).

import "fmt"

var calls int

func f() int {
	calls++
	return 7
}

func g() any {
	calls++
	return "s"
}

func main() {
	switch any(f()) {
	case any(5):
		fmt.Println("five")
	case any(6):
		fmt.Println("six")
	case any(7):
		fmt.Println("seven")
	}
	fmt.Println("calls:", calls)

	calls = 0
	var x any = 6
	switch f() {
	case x:
		fmt.Println("x")
	case any(5), any("7"):
		fmt.Println("wrong type")
	case any(7):
		fmt.Println("seven")
	}
	fmt.Println("calls:", calls)

	calls = 0
	switch f() {
	case x:
		fmt.Println("x")
	default:
		fmt.Println("default")
	}
	fmt.Println("calls:", calls)

	calls = 0
	switch g() {
	case 1, 2:
		fmt.Println("int")
	case "t", "s":
		fmt.Println("string")
	}
	fmt.Println("calls:", calls)
}
