package main

import (
	"fmt"
	"runtime"
)

var finalized = false

type T struct{ v int }

func main() {
	x := &T{v: 42}
	runtime.SetFinalizer(x, func(o *T) { finalized = true })
	x = nil
	for i := 0; i < 200 && !finalized; i++ {
		runtime.GC()
		_ = make([]byte, 1024)
	}
	if finalized {
		fmt.Println("finalized")
	} else {
		fmt.Println("NOT finalized")
	}
}
