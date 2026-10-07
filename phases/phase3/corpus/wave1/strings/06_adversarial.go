package main

import (
	"fmt"
	"strings"
)

func main() {
	// Empty strings
	fmt.Println(strings.Contains("", ""))
	fmt.Println(strings.HasPrefix("", ""))
	fmt.Println(strings.Index("", ""))
	fmt.Println(strings.Split("", ","))
	fmt.Println(strings.Fields(""))
	fmt.Println(strings.TrimSpace(""))
	fmt.Println(strings.ToUpper(""))
	fmt.Println(strings.Repeat("", 5))

	// Unicode edge cases
	fmt.Println(strings.ToUpper("ß"))          // German sharp s
	fmt.Println(strings.ToLower("İ"))          // Turkish dotted I
	fmt.Println(strings.EqualFold("ﬁ", "FI"))  // fi ligature
	fmt.Println(strings.Index("héllo", "é"))
	fmt.Println(len("héllo"), strings.Count("héllo", ""))

	// Malformed UTF-8
	fmt.Println(strings.ToValidUTF8("\xff\xfe", "?"))
	fmt.Println(strings.Map(func(r rune) rune { return r }, "\xff"))

	// Huge input (moderate size for test speed)
	big := strings.Repeat("a", 10000)
	fmt.Println(len(big))
	fmt.Println(strings.Count(big, "a"))
	fmt.Println(strings.Index(big, "a"))
}
