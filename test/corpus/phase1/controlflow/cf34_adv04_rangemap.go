package main

// R2 adv04 (review remediation): range-over-map capture — RangeMapNode has
// a separate boxing path from RangeSliceNode (cf15 covers slice only).
// Sums keep it order-independent (no len(): the controlflow corpus avoids
// builtins; the key capture is observed through a second map). gc: 6 60 3.
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	m2 := map[string]int{"a": 10, "b": 20, "c": 30}
	var fns [3]func() int
	var keys [3]func() int
	k := 0
	for kk, v := range m {
		fns[k] = func() int { return v }
		keys[k] = func() int { return m2[kk] }
		k++
	}
	sum := 0
	ksum := 0
	for _, f := range fns {
		sum += f()
	}
	for _, kf := range keys {
		ksum += kf()
	}
	_ = sum
	_ = ksum
	_ = k
}

// observe: sum ksum k
