package main

import "fmt"

type Writer interface{ Write([]byte) int }

func main() {
	var w Writer
	fmt.Println("about to dispatch on nil")
	w.Write([]byte("boom"))
	fmt.Println("unreached")
}
