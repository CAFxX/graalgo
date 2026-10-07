package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

const src = `package main

import "unsafe"

func Sizeof(x any) uintptr { return 0 }

const c = Sizeof(int(0))

var arr [Sizeof(int(0))]int

func main() {}
`

func main() {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "p.go", src, 0)
	conf := types.Config{
		Importer: nil,
		Error: func(e error) {
			fmt.Printf("ERR: %v\n", e)
		},
	}
	_, err := conf.Check("main", fset, []*ast.File{f}, nil)
	fmt.Println("check err:", err)
}
