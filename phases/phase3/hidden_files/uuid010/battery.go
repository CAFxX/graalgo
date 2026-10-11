package main

// BUG-MODPROBE-010 battery: multi-result calls in argument position.
// Each case must be byte-identical vs gc 1.27.1 on gmp=1 and gmp=4.
import "fmt"

func two() (int, int)               { return 3, 4 }
func three() (int, string, bool)    { return 7, "seven", true }
func oneErr() (int, error)          { return 42, nil }
func sum(a, b int) int              { return a + b }
func sum3(a int, s string, b bool) string {
	if b {
		return s
	}
	return "no"
}
func takeErr(a int, err error) int {
	if err != nil {
		return -1
	}
	return a
}

type calc struct{ base int }

func (c calc) add2(a, b int) int { return c.base + a + b }
func (c *calc) add2p(a, b int) int { return c.base + a + b }

func nested(x int) int { return x * 2 }

func main() {
	// 1. MR as sole arg, fixed arity.
	fmt.Println(sum(two()))
	// 2. MR as sole arg, variadic.
	fmt.Println(two())
	// 3. MR as sole arg, 3 results.
	fmt.Println(sum3(three()))
	// 4. MR with error result.
	fmt.Println(takeErr(oneErr()))
	// 5. Nested MR calls: h(f(g())).
	fmt.Println(nested(sum(two())))
	// 6. Method value with MR arg (value receiver).
	c := calc{base: 100}
	fmt.Println(c.add2(two()))
	// 7. Method value with MR arg (pointer receiver).
	fmt.Println(c.add2p(two()))
	// 8. MR call result used in expression statement.
	sum(two())
	fmt.Println("done")
	// 9. MR as sole arg in return position.
	fmt.Println(retSum())
}

func retSum() int {
	return sum(two())
}
