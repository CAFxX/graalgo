package main

// P1-D7 pinning test (R1 atkP shape): struct-with-slice-field in a
// for-header. gc's per-iteration z := z' is a Go assignment: slice fields
// ALIAS (backing array shared). If the refresh deep-copied the backing
// array, arr[0] would stay 0.

type T struct{ s []int }

func main() {
	arr := []int{0}
	n := 0
	var sink func() int
	captured := false
	for t := (T{s: arr}); n < 2; n++ {
		t.s[0] = 7
		if !captured {
			sink = func() int { return t.s[0] }
			captured = true
		}
	}
	a := arr[0]
	b := sink()
	_, _, _, _ = a, b, n, captured
}

// observe: a b
