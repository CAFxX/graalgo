package main

// R2 adv05 (review remediation): while-style loop — body var x gets
// per-iteration cells; outer var i is a plain shared capture. Both
// behaviors in one program. gc: 21 9 3 3.
func main() {
	var fns [3]func() int
	var gns [3]func() int
	k := 0
	i := 0
	for i < 3 {
		x := i * 7
		fns[k] = func() int { return x }
		gns[k] = func() int { return i }
		k++
		i++
	}
	xs := 0
	is := 0
	for _, f := range fns {
		xs += f()
	}
	for _, g := range gns {
		is += g()
	}
	_, _, _, _ = xs, is, k, i
}

// observe: xs is k i
