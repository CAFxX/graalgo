// 09_examples: InternalExample with exact Output, Unordered output, and a
// failing example (got/want diff). Run with -test.v for the full log.
package main

import (
	"fmt"
	"testing"
)

func ExampleHello() {
	fmt.Println("hello")
	// Output: hello
}

func ExampleUnordered() {
	fmt.Println("b")
	fmt.Println("a")
	// Unordered output:
	// a
	// b
}

func ExampleFail() {
	fmt.Println("got-this")
	// Output: want-that
}

func main() {
	examples := []testing.InternalExample{
		{"ExampleHello", ExampleHello, "hello\n", false},
		{"ExampleUnordered", ExampleUnordered, "a\nb\n", true},
		{"ExampleFail", ExampleFail, "want-that\n", false},
	}
	testing.Main(nil, nil, nil, examples)
}

