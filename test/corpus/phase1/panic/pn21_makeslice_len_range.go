package main

func main() {
	n := int64(1) << 62
	_ = make([]byte, n)
}

// panic: panic: runtime error: makeslice: len out of range
