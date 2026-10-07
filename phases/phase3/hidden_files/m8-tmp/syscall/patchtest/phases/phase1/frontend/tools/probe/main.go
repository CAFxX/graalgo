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

type Stack[T any] struct{ elems []T }

func (s Stack[T]) Size() int { return len(s.elems) }

func main() {
	s := Stack[int]{}
	_ = s.Size()
}
`

func main() {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "p.go", src, 0)
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	conf := types.Config{Importer: importer.Default()}
	_, err := conf.Check("main", fset, []*ast.File{file}, info)
	fmt.Println("err:", err)
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil {
			continue
		}
		obj := info.Defs[fd.Name].(*types.Func)
		recvT := obj.Type().(*types.Signature).Recv().Type()
		rn := recvT.(*types.Named)
		fmt.Printf("recv type: %s (obj %p)\n", recvT.String(), recvT)
		fmt.Printf("origin tparams: %v\n", rn.Origin().TypeParams())
		for i := 0; i < rn.Origin().TypeParams().Len(); i++ {
			fmt.Printf("  origin tp[%d] = %p (%s)\n", i, rn.Origin().TypeParams().At(i), rn.Origin().TypeParams().At(i).Obj().Name())
		}
		args := rn.TypeArgs()
		for i := 0; i < args.Len(); i++ {
			fmt.Printf("  recv targ[%d] = %p (%s)\n", i, args.At(i), args.At(i).String())
		}
	}
	for sel, s := range info.Selections {
		if s.Kind() != types.MethodVal {
			continue
		}
		recv := s.Recv()
		fmt.Printf("sel %s recv=%s (%p)\n", sel.Sel.Name, recv.String(), recv)
		if rn, ok := recv.(*types.Named); ok {
			args := rn.TypeArgs()
			for i := 0; i < args.Len(); i++ {
				fmt.Printf("  sel recv targ[%d] = %p (%s)\n", i, args.At(i), args.At(i).String())
			}
		}
	}
}
