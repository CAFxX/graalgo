package main

import "runtime"

func main() {
	saw := 0
	defer func() {
		r := recover()
		if r == nil {
			saw = 1
		} else {
			saw = 2
		}
		_ = saw
	}()
	runtime.Goexit()
}

// panic: fatal error: no goroutines (main called runtime.Goexit) - deadlock!
