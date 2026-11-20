package main

// L11 (REVIEW-2026-11-18-05): go/format — zero corpus imports before
// this program. Formatting Go source: Source on unformatted code,
// Node on a parsed AST, and the error paths (bad syntax).

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
)

const messy = "package p\n\nfunc  f( x int )int{return x+1}\n\ntype T struct{A int; B string}\n"

func main() {
	out, err := format.Source([]byte(messy))
	if err != nil {
		fmt.Printf("source-err=%v\n", err != nil)
		return
	}
	fmt.Printf("formatted=%q\n", string(out))
	_, err = format.Source([]byte("package p\nfunc f( {\n"))
	fmt.Printf("bad-source-err=%v\n", err != nil)
	// format.Node on a parsed file writes the formatted source.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", messy, parser.ParseComments)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err != nil)
		return
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		fmt.Printf("node-err=%v\n", err != nil)
		return
	}
	fmt.Printf("node=%q\n", buf.String())
	fmt.Printf("node-equals-source=%v\n", buf.String() == string(out))
	// Partial source: a declaration fragment.
	frag, err := format.Source([]byte("func g() {}"))
	fmt.Printf("frag=%q err=%v\n", string(frag), err != nil)
	// A //go:build directive comment is preserved.
	out3, err := format.Source([]byte("//go:build linux\n\npackage p\n\nvar x=1\n"))
	fmt.Printf("directive=%q err=%v\n", string(out3), err != nil)
	// format.Node on a single declaration.
	decl := f.Decls[0]
	var buf2 bytes.Buffer
	if err := format.Node(&buf2, fset, decl); err != nil {
		fmt.Printf("decl-err=%v\n", err != nil)
		return
	}
	fmt.Printf("decl=%q\n", buf2.String())
}
