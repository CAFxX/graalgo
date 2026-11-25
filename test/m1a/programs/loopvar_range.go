// Per-iteration loop variables of range loops (Go 1.22; m3-plan G6 mutant
// M-LOWER-03: range loops lowered onto one shared variable). Closures
// and pointers taken in the body must see each iteration's own value.
package main

func main() {
	var fs []func() int
	for _, v := range []int{1, 2, 3} {
		fs = append(fs, func() int { return v })
	}
	var ks []*int
	for k := range 3 {
		ks = append(ks, &k)
	}
	var rs []func() rune
	for _, r := range "aé世" {
		rs = append(rs, func() rune { return r })
	}
	println(fs[0](), fs[1](), fs[2](), *ks[0], *ks[1], *ks[2], rs[0](), rs[1](), rs[2]())
}
