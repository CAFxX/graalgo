package main

// Overlapping append: a spread source may alias the target's backing array,
// so the copy needs memmove (not memcpy) semantics. Also: zero-arg append is
// the identity (preserving nil-ness), and call arguments are evaluated
// before any element is stored (R2 P0-1, R1 P1-2, remediation class hunt).
func main() {
	// Overlap, no growth (spare capacity): a forward copy loop corrupts.
	s := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s2 := append(s[:2], s[:8]...)
	println("fwd:", s2[0], s2[1], s2[2], s2[3], s2[4], s2[5], s2[6], s2[7], s2[8], s2[9])

	// Overlap, target starts inside the source region.
	t := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	t2 := append(t[:5], t[2:7]...)
	println("part:", t2[0], t2[1], t2[2], t2[3], t2[4], t2[5], t2[6], t2[7], t2[8], t2[9])

	// Overlap forcing growth (cap-limited slice): fresh backing array.
	g := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	g2 := append(g[:2:2], g[:8]...)
	println("grow:", g2[0], g2[1], g2[2], g2[3], g2[4], g2[5], g2[6], g2[7], g2[8], g2[9])
	println("src-untouched:", g[0], g[8], g[9])

	// No overlap: regression guard for the memmove path.
	a := []int{1, 2}
	b := []int{3, 4, 5}
	c := append(a, b...)
	println("noalias:", c[0], c[1], c[2], c[3], c[4])

	// Empty / nil spread: identity.
	var z []int
	println("nil-spread:", append(z, z...) == nil)
	e := []int{7}
	println("empty-spread:", append(e, []int{}...)[0] == 7)

	// Zero-arg append is the identity, preserving nil-ness.
	var n []int
	println("zero-nil:", append(n) == nil)
	m := []int{1, 2}
	m2 := append(m)
	println("zero-nonil:", m2[0] == 1 && m2[1] == 2 && len(m2) == 2)

	// Arguments are evaluated before stores: w[3] must read v[2]'s
	// original value, not the value just stored at index 2.
	u := make([]int, 2, 10)
	v := u[:10]
	v[2], v[3] = 99, 77
	w := append(u, v[3], v[2])
	println("interleave:", w[0], w[1], w[2], w[3])
}
