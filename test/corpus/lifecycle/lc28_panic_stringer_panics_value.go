package main

// String() panics with a non-string value while the crash report is
// rendered: gc names the inner value's type ("... value: type main.myErr").

type myErr int

type S struct{}

func (S) String() string { panic(myErr(3)) }

func main() { panic(S{}) }
