package main

// Minimal straight-line program: constants, binary arithmetic, assignment.
//
// The trailing self-assignment is the M2 observability idiom: it reads z so
// the program type-checks ("declared and not used"), and the NodeFactory
// test reads z's frame slot after execution. Real programs observe values
// via println (WS G intrinsic stub), which replaces this idiom.
func main() {
	x := 6
	y := x + 1
	z := x * y
	z = z
}
