package main

// A goroutine blocked reading an os.Pipe must not hold the only
// GOMAXPROCS slot: main keeps yielding and finishes (gc parity).

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	runtime.GOMAXPROCS(1)
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	defer w.Close()
	go func() {
		b := make([]byte, 10)
		n, err := r.Read(b) // blocks: nobody writes
		fmt.Println("read", n, err)
	}()
	s := 0
	for i := 0; i < 1000; i++ {
		runtime.Gosched()
		s += i
	}
	fmt.Println("main done", s)
}
