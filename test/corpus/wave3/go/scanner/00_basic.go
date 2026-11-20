package main

// L11 (REVIEW-2026-11-18-05): go/scanner — zero corpus imports before
// this program. Lexing an embedded Go source: token stream with
// positions and literals, comment scanning, and the error path on
// broken source (unterminated string, illegal rune).

import (
	"fmt"
	"go/scanner"
	"go/token"
)

const src = `package p

// Add returns x+y.
func Add(x, y int) int {
	return x + y
}

const Pi = 3.14
`

const badSrc = "package p\n\nfunc f() {\n\ts := \"unterminated\n}\n"

func scan(src string) {
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("src.go", -1, len(src))
	var errs []string
	s.Init(file, []byte(src), func(pos token.Position, msg string) {
		errs = append(errs, fmt.Sprintf("%d:%d %s", pos.Line, pos.Column, msg))
	}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		fmt.Printf("tok %s %q @%d:%d\n", tok, lit, fset.Position(pos).Line, fset.Position(pos).Column)
	}
	fmt.Printf("errors=%d\n", len(errs))
	for _, e := range errs {
		fmt.Printf("err %s\n", e)
	}
}

func main() {
	scan(src)
	scan(badSrc)
	// Illegal rune.
	scan("package p\nvar x = `\x00`\n")
}
