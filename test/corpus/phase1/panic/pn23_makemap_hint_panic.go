package main

func boom() int {
	panic("hint boom")
}

func main() {
	_ = make(map[string]int, boom())
}

// panic: panic: hint boom
