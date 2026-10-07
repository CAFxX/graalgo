package main

import (
	"fmt"
	"strings"
)

func main() {
	// Title is fail-fast (deprecated; needs unicode tables)
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic:", r)
		}
	}()
	fmt.Println(strings.Title("hello"))
}
