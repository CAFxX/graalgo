package main

import "time"

// tc15: Sleep(0) and Sleep(negative) return immediately.
func main() {
	time.Sleep(0)
	println("zero-ok")
	time.Sleep(-time.Second)
	println("negative-ok")
}
