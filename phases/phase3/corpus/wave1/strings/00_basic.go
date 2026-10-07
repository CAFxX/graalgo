package main

import (
	"fmt"
	"strings"
)

func main() {
	// Contains family
	fmt.Println(strings.Contains("hello", "ll"))
	fmt.Println(strings.Contains("hello", "xx"))
	fmt.Println(strings.ContainsAny("hello", "aeiou"))
	fmt.Println(strings.ContainsAny("xyz", "aeiou"))
	fmt.Println(strings.ContainsRune("hello", 'l'))
	fmt.Println(strings.ContainsRune("hello", 'x'))
	fmt.Println(strings.ContainsFunc("hello1", func(r rune) bool { return r >= '0' && r <= '9' }))

	// HasPrefix/HasSuffix
	fmt.Println(strings.HasPrefix("hello", "he"))
	fmt.Println(strings.HasSuffix("hello", "lo"))

	// Index family
	fmt.Println(strings.Index("hello", "ll"))
	fmt.Println(strings.Index("hello", "xx"))
	fmt.Println(strings.IndexAny("hello", "aeiou"))
	fmt.Println(strings.IndexByte("hello", 'l'))
	fmt.Println(strings.IndexRune("héllo", 'é'))
	fmt.Println(strings.IndexFunc("hello1", func(r rune) bool { return r >= '0' && r <= '9' }))
	fmt.Println(strings.LastIndex("ababa", "aba"))
	fmt.Println(strings.LastIndexAny("hello", "aeiou"))
	fmt.Println(strings.LastIndexByte("hello", 'l'))
	fmt.Println(strings.LastIndexFunc("1hello", func(r rune) bool { return r >= '0' && r <= '9' }))

	// Count
	fmt.Println(strings.Count("mississippi", "ss"))
	fmt.Println(strings.Count("hello", ""))

	// Compare/EqualFold
	fmt.Println(strings.Compare("a", "b"))
	fmt.Println(strings.Compare("b", "a"))
	fmt.Println(strings.Compare("a", "a"))
	fmt.Println(strings.EqualFold("Straße", "STRASSE"))
	fmt.Println(strings.EqualFold("hello", "HELLO"))

	// Clone
	fmt.Println(strings.Clone("hello") == "hello")
}
