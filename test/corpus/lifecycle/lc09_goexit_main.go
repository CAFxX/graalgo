package main

import (
	"fmt"
	"runtime"
)

func main() {
	go func() { fmt.Println("other goroutine done") }()
	defer fmt.Println("deferred in main")
	runtime.Goexit()
}
