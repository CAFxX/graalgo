package main

import (
	"fmt"
	"strings"
)

func main() {
	// Byte replacer (all single-byte)
	r := strings.NewReplacer([]string{"a", "X", "e", "Y"}...)
	fmt.Println(r.Replace("abcde"))
	fmt.Println(r.Replace("hello"))

	// Byte-string replacer (single-byte old, multi-byte new)
	r2 := strings.NewReplacer([]string{"a", "XYZ", "b", "Q"}...)
	fmt.Println(r2.Replace("abc"))

	// Generic replacer (multi-byte patterns)
	r3 := strings.NewReplacer([]string{"foo", "bar", "baz", "qux"}...)
	fmt.Println(r3.Replace("foo baz foo"))

	// Single-string replacer
	r4 := strings.NewReplacer([]string{"hello", "world"}...)
	fmt.Println(r4.Replace("hello there"))

	// Empty replacer
	r5 := strings.NewReplacer([]string{}...)
	fmt.Println(r5.Replace("hello"))
}
