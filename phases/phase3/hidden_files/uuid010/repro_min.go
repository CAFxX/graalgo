package main

// BUG-MODPROBE-010 minimal reproducer: multi-result call as the sole
// function argument. Go semantics: f(g()) binds g's results to f's params
// in order.
import "fmt"

func two() (int, int) {
	return 3, 4
}

func sum(a, b int) int {
	return a + b
}

func main() {
	// Sole-arg MR call to fixed-arity func.
	fmt.Println(sum(two()))
	// Sole-arg MR call to variadic func.
	fmt.Println(two())
}
