package main

// RV18-P1-11 (review remediation): RangeIntNode swallowed a labeled
// `continue` addressed to an outer loop — it continued the inner range
// instead of the outer loop (silent wrong control flow). Labeled `break`
// to an outer loop is covered for symmetry; plain/self-labeled/nested
// cases guard the label-match rule. gc: 6 3 9 6 8 39.
func main() {
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
	c3 := 0
	for i := range 6 {
		if i%2 == 0 {
			continue
		}
		c3 += i
	}
	c4 := 0
	for i := range 10 {
		if i == 4 {
			break
		}
		c4 += i
	}
	c5 := 0
inner:
	for i := range 5 {
		if i == 2 {
			continue inner
		}
		c5 += i
	}
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
	_, _, _, _, _, _ = c1, c2, c3, c4, c5, c6
}

// observe: c1 c2 c3 c4 c5 c6
