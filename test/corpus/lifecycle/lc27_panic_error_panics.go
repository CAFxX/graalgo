package main

// The panic value's Error() itself panics while the crash report is
// rendered: gc throws "panic while printing panic value: <string>"
// (exit 2) instead of hanging or printing the original panic.

type E struct{}

func (E) Error() string { panic("x") }

func main() { panic(E{}) }
