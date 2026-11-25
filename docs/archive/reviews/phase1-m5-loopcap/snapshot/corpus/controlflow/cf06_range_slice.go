package main

func main() {
	s := []int{10, 20, 30, 40, 50}
	sum := 0
	ksum := 0
	for i, v := range s {
		sum += v
		ksum += i
	}
	// value-only range
	vs := 0
	for _, v := range s {
		vs += v * 2
	}
	// index-only range
	kc := 0
	for i := range s {
		kc += i * i
	}
	// range over slice expression, blank both
	bc := 0
	for range s {
		bc++
	}
	_ = sum
	_ = ksum
	_ = vs
	_ = kc
	_ = bc
}

// observe: sum ksum vs kc bc
