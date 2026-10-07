package main

import (
	"fmt"
	"strings"
)

func main() {
	// ToUpper/ToLower with ASCII and Unicode
	fmt.Println(strings.ToUpper("hello"))
	fmt.Println(strings.ToUpper("héllo"))
	fmt.Println(strings.ToLower("HELLO"))
	fmt.Println(strings.ToLower("HÉLLO"))
	fmt.Println(strings.ToTitle("hello world"))

	// Map
	fmt.Println(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, "hello"))

	// ToValidUTF8
	fmt.Println(strings.ToValidUTF8("hello\xffworld", "?"))
	fmt.Println(strings.ToValidUTF8("valid", "?"))

	// TrimSpace with unicode spaces
	fmt.Println(strings.TrimSpace(" \t\n hello \u00a0\u2000"))
}
