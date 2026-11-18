package main

// A goroutine panics, but its Error() is still running when main returns:
// gc's runtime.main yields briefly to an in-progress panic, then exits 0
// (the half-rendered panic never prints). The panic print never comes
// with exit status 0, and the process does not hang.

import (
	"fmt"
	"time"
)

type E struct{}

func (E) Error() string {
	time.Sleep(2 * time.Second)
	return "late"
}

func main() {
	go func() { panic(E{}) }()
	time.Sleep(10 * time.Millisecond)
	fmt.Println("main returns")
}
