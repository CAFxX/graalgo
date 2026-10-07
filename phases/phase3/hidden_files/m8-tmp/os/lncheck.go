package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

const src = `package os

import _ "unsafe"

//go:linkname osOpen graalgo.os.open
func osOpen(path string, flags int64, perm uint32) int64
`

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "os.go", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}
	conf := types.Config{}
	_, err = conf.Check("os", fset, []*ast.File{f}, nil)
	fmt.Println("types error:", err)
}
