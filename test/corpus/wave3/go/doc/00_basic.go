package main

// L11 (REVIEW-2026-11-18-05): go/doc — zero corpus imports before this
// program. Extracting documentation from an embedded package source:
// package doc, function/type/method listings with synopses, and the
// AllDecls mode (unexported decls appear).

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"sort"
)

const src = `// Package demo is a small example package for documentation.
//
// It has two sentences in its doc comment.
package demo

import "fmt"

// Answer is the answer to everything.
const Answer = 42

// Greeter greets a person by name.
type Greeter struct {
	// Name is the person to greet.
	Name string
}

// Greet returns a greeting for g's Name.
func (g Greeter) Greet() string {
	return fmt.Sprintf("Hello, %s", g.Name)
}

// Add adds a and b.
func Add(a, b int) int { return a + b }

// hidden is unexported and must not appear in the default listing.
func hidden() {}
`

func listNames(vs []*doc.Value) []string {
	names := make([]string, 0, len(vs))
	for _, v := range vs {
		names = append(names, v.Names...)
	}
	sort.Strings(names)
	return names
}

func listFuncs(fs []*doc.Func) []string {
	names := make([]string, 0, len(fs))
	for _, f := range fs {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "demo.go", src, parser.ParseComments)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err != nil)
		return
	}
	pkg, err := doc.NewFromFiles(fset, []*ast.File{f}, "example.com/demo")
	if err != nil {
		fmt.Printf("doc-err=%v\n", err != nil)
		return
	}
	fmt.Printf("pkg-name=%s synopsis=%q\n", pkg.Name, doc.Synopsis(pkg.Doc))
	fmt.Printf("funcs=%q\n", listFuncs(pkg.Funcs))
	fmt.Printf("types=%q\n", func() []string {
		names := make([]string, 0, len(pkg.Types))
		for _, t := range pkg.Types {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return names
	}())
	fmt.Printf("consts=%q\n", listNames(pkg.Consts))
	for _, t := range pkg.Types {
		fmt.Printf("type %s methods=%q\n", t.Name, listFuncs(t.Methods))
		fmt.Printf("type %s synopsis=%q\n", t.Name, doc.Synopsis(t.Doc))
	}
	all, err := doc.NewFromFiles(fset, []*ast.File{f}, "example.com/demo", doc.AllDecls)
	if err != nil {
		fmt.Printf("alldecls-err=%v\n", err != nil)
		return
	}
	fmt.Printf("alldecls-funcs=%q\n", listFuncs(all.Funcs))
}
