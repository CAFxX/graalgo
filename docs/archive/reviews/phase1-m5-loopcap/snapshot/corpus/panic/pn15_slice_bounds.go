package main

func main() {
	s := []int{1, 2, 3}
	_ = s[:10]
}

// panic: panic: runtime error: slice bounds out of range [:10] with capacity 3
