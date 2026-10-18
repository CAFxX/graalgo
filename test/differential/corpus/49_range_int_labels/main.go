package main

import "fmt"

// Labeled break/continue targeting an outer loop from inside
// `for i := range n` (range over int, Go 1.22+). Regression cover for the
// RangeIntNode bug that swallowed a labeled `continue` addressed to an
// outer loop (it continued the inner range instead).
func main() {
	// 1. labeled continue to an outer loop from inside `for i := range n`
	c1 := 0
outer:
	for k := 0; k < 3; k++ {
		_ = k
		for i := range 5 {
			if i == 2 {
				continue outer
			}
			c1++
		}
	}
	fmt.Println("labeled-continue:", c1) // 2 per outer iter -> 6

	// 2. labeled break to an outer loop from inside `for i := range n`
	c2 := 0
outerB:
	for k := 0; k < 10; k++ {
		for i := range 5 {
			if i == 3 {
				break outerB
			}
			c2++
		}
		c2 += 100
	}
	fmt.Println("labeled-break:", c2) // k=0 counts i=0,1,2 then breaks out -> 3

	// 3. plain continue inside `for i := range n` stays in the inner loop
	c3 := 0
	for i := range 6 {
		if i%2 == 0 {
			continue
		}
		c3 += i
	}
	fmt.Println("plain-continue:", c3) // 1+3+5 = 9

	// 4. plain break inside `for i := range n` stays in the inner loop
	c4 := 0
	for i := range 10 {
		if i == 4 {
			break
		}
		c4 += i
	}
	fmt.Println("plain-break:", c4) // 0+1+2+3 = 6

	// 5. labeled continue targeting the range loop itself
	c5 := 0
inner:
	for i := range 5 {
		if i == 2 {
			continue inner
		}
		c5 += i
	}
	fmt.Println("self-labeled-continue:", c5) // 0+1+3+4 = 8

	// 6. nested `for i := range n`: plain continue stays in the inner range
	c6 := 0
	for a := range 3 {
		_ = a
		for b := range 4 {
			if b == 1 {
				continue
			}
			c6++
		}
		c6 += 10
	}
	fmt.Println("nested-plain-continue:", c6) // 3*(3+10) = 39
}
