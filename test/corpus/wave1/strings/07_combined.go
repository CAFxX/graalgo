package main

import (
	"fmt"
	"strings"
)

func main() {
	// Combination: Replacer + Builder (via WriteByte loop)
	r := strings.NewReplacer([]string{"a", "X"}...)
	var b strings.Builder
	s := r.Replace("banana")
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
	}
	fmt.Println(b.String())

	// Combination: Split + Join round-trip
	parts := strings.Split("a,b,c", ",")
	fmt.Println(strings.Join(parts, ","))

	// Combination: Trim + ToUpper
	fmt.Println(strings.ToUpper(strings.TrimSpace("  hello  ")))

	// Combination: Fields + Join
	fmt.Println(strings.Join(strings.Fields("  a b c  "), "-"))

	// Combination: Map + ReplaceAll
	mapped := strings.Map(func(r rune) rune {
		if r == 'a' {
			return 'o'
		}
		return r
	}, "banana")
	fmt.Println(strings.ReplaceAll(mapped, "o", "0"))
}
