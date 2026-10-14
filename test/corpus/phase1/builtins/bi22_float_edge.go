package main

// Float edge cases: negative zero and tiny values.
//
// Regression tests (M8 closeout):
// - The -0.0 literal is +0.0 in gc (go/types folds it); a runtime unary
//   negation used to produce IEEE -0.0 instead.
// - Float == must use IEEE semantics (-0.0 == 0.0 is true); Double.compare
//   (total ordering) used to say false.
// - %f of tiny values (1e-300) must print 0.000000, not crash.
// - Nil slice/map print []/map[] (not <nil>); %T of typed nils prints the type.
import "fmt"

func main() {
	// -0.0 literal is +0.0.
	x := -0.0
	fmt.Printf("lit: %f %v eq:%v\n", x, x, x == 0.0)

	// True -0.0 via unary minus of a variable: prints -0, compares equal.
	z := 0.0
	n := -z
	fmt.Printf("neg: %v %f eq:%v\n", n, n, n == z)

	// Tiny %f rounds to zero.
	fmt.Printf("tiny: %f\n", 1e-300)
	fmt.Printf("tinyE: %e\n", 1e-300)

	// Nil slice/map %v.
	var s []int
	var m map[string]int
	fmt.Printf("nilv: %v %v\n", s, m)
	fmt.Printf("nilt: %T %T\n", s, m)

	// NaN never equals itself (IEEE).
	nan := 0.0 / 1.0
	nan = nan / nan
	fmt.Printf("nanEq:%v nanNe:%v\n", nan == nan, nan != nan)
}
