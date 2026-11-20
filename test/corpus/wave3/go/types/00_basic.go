package main

// L11 (REVIEW-2026-11-18-05): go/types — zero corpus imports before
// this program. Type-checking embedded source: declarations, types of
// expressions, generics, methods, and the error path on ill-typed
// source. The source imports nothing, so no importer is needed.

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
)

const src = `package demo

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }
func (c Counter) Value() int { return c.n }

func Add(a, b int) int { return a + b }

func Map[T any](s []T, f func(T) T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

const Pi = 3.14

var Global = Add(1, 2)
`

const badSrc = `package demo

func Broken() int { return "not an int" }
`

func check(src string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "demo.go", src, 0)
	if err != nil {
		fmt.Printf("parse-err=%v\n", err != nil)
		return
	}
	conf := types.Config{Importer: importer.Default()}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := conf.Check("demo", fset, []*ast.File{f}, info)
	if err != nil {
		fmt.Printf("check-err=%v\n", err != nil)
		return
	}
	fmt.Printf("pkg=%s complete=%v\n", pkg.Path(), pkg.Complete())
	names := pkg.Scope().Names()
	sort.Strings(names)
	fmt.Printf("scope=%q\n", names)
	for _, name := range names {
		obj := pkg.Scope().Lookup(name)
		fmt.Printf("decl %s: %s\n", name, obj.Type())
	}
	// Expression types of call expressions (sorted: map order is random).
	var calls []string
	for expr, tv := range info.Types {
		if _, ok := expr.(*ast.CallExpr); ok {
			calls = append(calls, fmt.Sprintf("%s value=%v", tv.Type, tv.IsValue()))
		}
	}
	sort.Strings(calls)
	for _, c := range calls {
		fmt.Printf("call-type=%s\n", c)
	}
	// Universe lookup.
	fmt.Printf("universe-int=%v\n", types.Universe.Lookup("int") != nil)
	fmt.Printf("universe-true=%v\n", types.Universe.Lookup("true") != nil)
	// Constructed types.
	fmt.Printf("slice-int=%s map=%s chan=%s\n",
		types.NewSlice(types.Typ[types.Int]),
		types.NewMap(types.Typ[types.String], types.Typ[types.Int]),
		types.NewChan(types.SendRecv, types.Typ[types.Int]))
	fmt.Printf("int-underlying=%s\n", types.Typ[types.Int].Underlying())
}

func main() {
	check(src)
	check(badSrc)
}
