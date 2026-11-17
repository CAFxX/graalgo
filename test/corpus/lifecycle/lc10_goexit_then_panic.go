package main

import "runtime"

func main() {
	defer func() { panic("after goexit") }()
	runtime.Goexit()
}
