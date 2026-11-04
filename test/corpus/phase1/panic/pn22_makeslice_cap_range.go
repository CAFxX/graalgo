package main

func main() {
	n := int64(1) << 62
	_ = make([]byte, 0, n)
}

// panic: panic: runtime error: makeslice: cap out of range
