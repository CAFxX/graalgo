package main

func main() {
	s := make([]byte, 0, 1<<62)
	_ = s
}

// panic: panic: runtime error: makeslice: cap out of range
