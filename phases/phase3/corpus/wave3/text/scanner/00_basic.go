package main

import (
	"fmt"
	"strings"
	"text/scanner"
)

func main() {
	// Scan Go-like source.
	src := `package main
import "fmt"
func hello() int {
	x := 42
	return x + 1
}`
	var s scanner.Scanner
	s.Init(strings.NewReader(src))
	s.Filename = "test.go"
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		fmt.Println(s.TokenText())
	}
	fmt.Println("scan done")
	// Position.
	var s2 scanner.Scanner
	s2.Init(strings.NewReader("a\nb\nc"))
	s2.Scan()
	fmt.Println(s2.Pos())
	s2.Scan()
	fmt.Println(s2.Pos())
	fmt.Println("pos ok")
}
