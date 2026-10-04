package main

// R2 adv02, per-closure distinguisher (review remediation): two closures
// created in the SAME iteration must share one per-iteration cell —
// a fresh-box-per-funclit implementation fails this (gc: 0 1 1 10 11 11).
func main() {
	var fns [6]func() int
	for i := 0; i < 3; i++ {
		x := i * 10
		fns[2*i] = func() int { return x }
		fns[2*i+1] = func() int {
			x++
			return x
		}
	}
	r0 := fns[0]
	w0 := fns[1]
	r1 := fns[2]
	w1 := fns[3]
	a := r0()
	b := w0()
	c := r0()
	d := r1()
	e := w1()
	f := r1()
	_, _, _, _, _, _ = a, b, c, d, e, f
}

// observe: a b c d e f
