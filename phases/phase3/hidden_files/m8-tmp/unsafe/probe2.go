package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
)

const src = `package main

import "unsafe"

func main() {
	p := new(int)
	s := unsafe.Slice(p, 3)
	_ = s
	var _ []int = s
}
`

func main() {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "p.go", src, 0)
	conf := types.Config{Importer: importer.Default()}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	_, err := conf.Check("main", fset, []*ast.File{f}, info)
	fmt.Println("err:", err)
	for id, obj := range info.Uses {
		if id.Name == "Slice" {
			fmt.Printf("Slice obj: %T %v\n", obj, obj)
		}
	}
}
