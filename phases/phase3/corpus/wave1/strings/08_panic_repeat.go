package main

import (
	"fmt"
	"strings"
)

func main() {
	// Repeat with negative count panics
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	fmt.Println(strings.Repeat("ab", -1))
}
