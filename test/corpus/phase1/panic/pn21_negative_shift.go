package main

func main() {
	var n int64 = -1
	_ = int64(1) << n
}

// panic: panic: runtime error: negative shift amount
