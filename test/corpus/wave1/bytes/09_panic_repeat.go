package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Repeat with negative count panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	fmt.Println(bytes.Repeat([]byte("ab"), -1))
}
