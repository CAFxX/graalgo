package main

// Loop-capture: nested loops — inner and outer variables each get
// per-iteration cells; the phase stack must not confuse the two loops.
func main() {
	var fns [9]func() int
	k := 0
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			y := i*10 + j
			fns[k] = func() int { return y }
			k++
		}
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
}

// observe: sum
