package main

// L11 (REVIEW-2026-11-18-05): go/parser — zero corpus imports before
// this program. ParseFile on an embedded Go source string, ast.Inspect
// walk, deterministic positions (line:col from the fixed source).

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

const src = `package demo

import "fmt"

type Point struct {
	X, Y int
}

func (p Point) String() string {
	return fmt.Sprintf("(%d,%d)", p.X, p.Y)
}

func add(a, b int) int { return a + b }

func main() {
	p := Point{X: 1, Y: 2}
	fmt.Println(add(p.X, p.Y))
}
`

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "demo.go", src, parser.ParseComments)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err)
		return
	}
	fmt.Printf("package=%s\n", f.Name.Name)
	fmt.Printf("imports=%d\n", len(f.Imports))

	// Collect declarations in source order via Inspect.
	var funcs, types []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.FuncDecl:
			funcs = append(funcs, t.Name.Name)
		case *ast.TypeSpec:
			types = append(types, t.Name.Name)
		}
		return true
	})
	fmt.Printf("funcs=%v\n", funcs)
	fmt.Printf("types=%v\n", types)

	// Positions are deterministic: fixed source, fixed offsets.
	pos := fset.Position(f.Package)
	fmt.Printf("package-pos=%d:%d\n", pos.Line, pos.Column)
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			p := fset.Position(fd.Pos())
			fmt.Printf("decl-%s=%d:%d\n", fd.Name.Name, p.Line, p.Column)
		}
	}

	// Sorted names (order-independent check).
	sorted := append([]string{}, funcs...)
	sort.Strings(sorted)
	fmt.Printf("funcs-sorted=%v\n", sorted)

	// Parse errors are reported, not panics.
	_, err = parser.ParseFile(fset, "bad.go", "package bad\nfunc (", 0)
	fmt.Printf("bad-err=%v\n", err != nil)
}
