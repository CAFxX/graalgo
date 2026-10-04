package main

type P struct{ v int }

func main() {
	var p *P
	_ = p.v
}

// panic: panic: runtime error: invalid memory address or nil pointer dereference
