package main

func main() {
	s := make([]byte, 1<<62)
	_ = s
}

// panic: panic: runtime error: makeslice: len out of range
