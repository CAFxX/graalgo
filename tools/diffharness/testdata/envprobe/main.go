package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("gomaxprocs:", runtime.GOMAXPROCS(0))
}
