package main

// Loop-capture: range variables get per-iteration cells.
func main() {
	s := []int{10, 20, 30}
	var fns [3]func() int
	k := 0
	for _, v := range s {
		fns[k] = func() int { return v }
		k++
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
}

// observe: sum
