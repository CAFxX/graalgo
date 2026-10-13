package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func main() {
	s := "héllo, 世界"
	fmt.Println(len(s), utf8.RuneCountInString(s))
	for i, r := range s {
		fmt.Printf("%d %c %U\n", i, r, r)
		break // just the first, to keep output short
	}
	var b strings.Builder
	b.WriteString("foo")
	b.WriteRune('β')
	fmt.Println(b.String(), b.Len())
	fmt.Println(strings.ToUpper("abc"), strings.Contains(s, "世界"))
}
