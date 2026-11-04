package main

// copy()/append() of array elements must copy them, not alias them:
// mutating a copied array element must not affect the source.
func main() {
	s := [][2]int{{1, 2}, {3, 4}}
	t := make([][2]int, 2)
	copy(t, s)
	t[0][0] = 9
	println("copy s00", s[0][0], "t00", t[0][0])
	a := [2]int{5, 6}
	u := append([][2]int{}, a)
	a[0] = 9
	println("append-var a0", a[0], "u00", u[0][0])
}
