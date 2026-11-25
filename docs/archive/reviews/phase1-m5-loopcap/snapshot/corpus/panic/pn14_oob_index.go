package main

func main() {
	s := []int{1, 2, 3}
	_ = s[5]
}

// panic: panic: runtime error: index out of range [5] with length 3
