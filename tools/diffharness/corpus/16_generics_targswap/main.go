// TargSwap target: two observably-ordered type parameters. The untyped
// constants 1 and 2 are assignable under both [int, float64] and
// [float64, int], so the TargSwap mutant still compiles while printing
// different output.
package main

import "fmt"

func Show[T int | float64, U int | float64](t T, u U) string {
	return fmt.Sprintf("%T/%T=%v/%v", t, t, u, u)
}

func main() {
	fmt.Println(Show[int, float64](1, 2))
}
