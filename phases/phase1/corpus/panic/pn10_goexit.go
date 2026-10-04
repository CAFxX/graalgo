package main

import "runtime"

func main() {
	ran := 0
	defer func() { ran = 1 }()
	defer func() { ran = ran*10 + 2 }()
	runtime.Goexit()
	ran = 99
}

// panic: fatal error: no goroutines (main called runtime.Goexit) - deadlock!
