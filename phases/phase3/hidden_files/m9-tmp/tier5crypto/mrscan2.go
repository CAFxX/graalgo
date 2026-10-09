// Scans Go files (parser only) for func/method decls with >=2 results.
// Prints: pkgpath|recvtype|name|nresults|r0type|r1type|...
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	fset := token.NewFileSet()
	for _, path := range os.Args[1:] {
		src, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse error %s: %v\n", path, err)
			continue
		}
		pkg := f.Name.Name
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Type.Results == nil {
				continue
			}
			n := 0
			var rtypes []string
			for _, fld := range fd.Type.Results.List {
				t := exprString(fld.Type)
				if len(fld.Names) == 0 {
					n++
					rtypes = append(rtypes, t)
				} else {
					for range fld.Names {
						n++
						rtypes = append(rtypes, t)
					}
				}
			}
			if n < 2 {
				continue
			}
			recv := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recv = exprString(fd.Recv.List[0].Type)
				recv = strings.TrimPrefix(recv, "*")
				// strip type params: nistCurve[Point] -> nistCurve
				if i := strings.Index(recv, "["); i >= 0 {
					recv = recv[:i]
				}
			}
			fmt.Printf("%s|%s|%s|%d|%s\n", pkg, recv, fd.Name.Name, n, strings.Join(rtypes, "|"))
		}
	}
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + exprString(t.Elt)
		}
		return "[" + exprString(t.Len) + "]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *ast.FuncType:
		return "func"
	case *ast.InterfaceType:
		return "interface"
	case *ast.ChanType:
		return "chan"
	case *ast.BasicLit:
		return t.Value
	default:
		return fmt.Sprintf("%T", e)
	}
}
