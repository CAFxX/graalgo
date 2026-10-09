package main

import (
	"fmt"
	"regexp/syntax"
)

func main() {
	// Parse a simple regexp.
	re, err := syntax.Parse(`a(b|c)*d`, syntax.Perl)
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}
	fmt.Println(re.Op)
	// Simplify.
	simp := re.Simplify()
	fmt.Println(simp.Op)
	fmt.Println("syntax ok")
}
