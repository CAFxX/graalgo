package main

// panic wiring (WS G wires to worker-F's Panic.throwPanic).
// The program panics; the message appears on stderr (gc parity).
func main() {
	println("before")
	panic("boom")
	println("after")
}
