package main

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	// order-independent reductions: sums and count prove the multiset
	ksum := 0
	vsum := 0
	count := 0
	for k, v := range m {
		for _, r := range k {
			ksum += int(r)
		}
		vsum += v
		count++
	}
	// key-only range
	kcount := 0
	for k := range m {
		_ = k
		kcount++
	}
	// delete-free mutation during iteration is not tested (spec order
	// undefined); instead verify a second pass sees the same multiset
	vsum2 := 0
	for _, v := range m {
		vsum2 += v * v
	}
	_ = ksum
	_ = vsum
	_ = count
	_ = kcount
	_ = vsum2
}

// observe: ksum vsum count kcount vsum2
