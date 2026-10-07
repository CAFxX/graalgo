package main

import (
	"fmt"
	"strings"
)

func main() {
	// Split family
	fmt.Println(strings.Split("a,b,c", ","))
	fmt.Println(strings.Split("a,b,c", ";"))
	fmt.Println(strings.SplitN("a,b,c", ",", 2))
	fmt.Println(strings.SplitAfter("a,b,c", ","))
	fmt.Println(strings.SplitAfterN("a,b,c", ",", 2))

	// Fields family
	fmt.Println(strings.Fields("  a b\tc\n"))
	fmt.Println(strings.FieldsFunc("a1b2c", func(r rune) bool { return r >= '0' && r <= '9' }))

	// Join
	fmt.Println(strings.Join([]string{"a", "b", "c"}, "-"))
	fmt.Println(strings.Join([]string{}, "-"))

	// Replace
	fmt.Println(strings.Replace("aaa", "a", "b", 2))
	fmt.Println(strings.Replace("aaa", "a", "b", -1))
	fmt.Println(strings.ReplaceAll("aaa", "a", "b"))

	// Repeat
	fmt.Println(strings.Repeat("ab", 3))
	fmt.Println(strings.Repeat("ab", 0))

	// Trim family
	fmt.Println(strings.Trim("  hello  ", " "))
	fmt.Println(strings.Trim("xxhelloxx", "x"))
	fmt.Println(strings.TrimLeft("  hello", " "))
	fmt.Println(strings.TrimRight("hello  ", " "))
	fmt.Println(strings.TrimPrefix("hello", "he"))
	fmt.Println(strings.TrimSuffix("hello", "lo"))
	fmt.Println(strings.TrimFunc("123hello321", func(r rune) bool { return r >= '0' && r <= '9' }))
	fmt.Println(strings.TrimLeftFunc("123hello", func(r rune) bool { return r >= '0' && r <= '9' }))
	fmt.Println(strings.TrimRightFunc("hello321", func(r rune) bool { return r >= '0' && r <= '9' }))
}
