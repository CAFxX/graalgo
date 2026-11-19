package main

// L11 (REVIEW-2026-11-18-05): go/printer — zero corpus imports before
// this program (bonus program alongside go/parser). Parse an embedded
// source, print it back with go/printer; output is deterministic.

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

const src = `package demo

func add(a, b int) int { return a + b }
`

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "demo.go", src, 0)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err)
		return
	}
	var buf bytes.Buffer
	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, f); err != nil {
		fmt.Printf("print-err=%v\n", err)
		return
	}
	out := buf.String()
	fmt.Printf("printed=%q\n", out)
	fmt.Printf("has-func=%v\n", strings.Contains(out, "func add(a, b int) int"))
	fmt.Printf("has-package=%v\n", strings.HasPrefix(out, "package demo"))
}
