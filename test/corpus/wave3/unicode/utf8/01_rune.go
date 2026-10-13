package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	// utf8.RuneLen, Valid, RuneCount
	fmt.Println(utf8.RuneLen('é'))              // 2
	fmt.Println(utf8.RuneLen('a'))              // 1
	fmt.Println(utf8.Valid([]byte("hello")))    // true
	fmt.Println(utf8.Valid([]byte{0xff, 0xfe})) // false
	fmt.Println(utf8.RuneCount([]byte("héllo"))) // 5
}
