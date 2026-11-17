package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println(runtime.GOMAXPROCS(0))
	fmt.Println(runtime.GOMAXPROCS(3))
	fmt.Println(runtime.GOMAXPROCS(0))
	fmt.Println(runtime.GOMAXPROCS(-1))
	fmt.Println(runtime.GOMAXPROCS(1))
	fmt.Println(runtime.GOMAXPROCS(0))
}
