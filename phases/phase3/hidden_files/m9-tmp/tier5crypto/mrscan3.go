// Scans Go files (parser only) for func/method decls with >=2 results.
// Outputs JSON: [{pkg, recv, recvTypeParams, name, typeParams, params,
// results:[{name,type}], pos:{start,end,headerEnd}}]
// Positions are byte offsets into the file.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type Result struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type Member struct {
	Pkg           string   `json:"pkg"`
	Recv          string   `json:"recv"`
	RecvVar       string   `json:"recvVar"`
	RecvTypeParams string  `json:"recvTypeParams"`
	Name          string   `json:"name"`
	TypeParams    string   `json:"typeParams"`
	Params        string   `json:"params"`
	Results       []Result `json:"results"`
	Start         int      `json:"start"`
	End           int      `json:"end"`
	HeaderEnd     int      `json:"headerEnd"`
}

func main() {
	var all []Member
	for _, path := range os.Args[1:] {
		fset := token.NewFileSet()
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
			if !ok || fd.Type.Results == nil {
				continue
			}
			var results []Result
			auto := 0
			for _, fld := range fd.Type.Results.List {
				t := exprString(fld.Type)
				if len(fld.Names) == 0 {
					results = append(results, Result{fmt.Sprintf("v%d", auto), t})
					auto++
				} else {
					for _, nm := range fld.Names {
						results = append(results, Result{nm.Name, t})
					}
				}
			}
			if len(results) < 2 {
				continue
			}
			m := Member{Pkg: pkg, Name: fd.Name.Name, Results: results}
			m.Start = int(fd.Pos()) - 1
			m.End = int(fd.End()) - 1
			m.HeaderEnd = int(fd.Body.Lbrace) - 1
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				rf := fd.Recv.List[0]
				if len(rf.Names) > 0 {
					m.RecvVar = rf.Names[0].Name
				}
				rt := exprString(rf.Type)
				// split *nistCurve[Point] into *, nistCurve, [Point]
				rt = strings.TrimPrefix(rt, "*")
				if i := strings.Index(rt, "["); i >= 0 {
					m.Recv = rt[:i]
					m.RecvTypeParams = rt[i:]
				} else {
					m.Recv = rt
				}
			}
			if fd.Type.TypeParams != nil {
				var tps []string
				for _, tp := range fd.Type.TypeParams.List {
					var names []string
					for _, nm := range tp.Names {
						names = append(names, nm.Name)
					}
					tps = append(tps, strings.Join(names, ", ")+" "+exprString(tp.Type))
				}
				m.TypeParams = "[" + strings.Join(tps, ", ") + "]"
			}
			// params as source text
			if fd.Type.Params != nil {
				m.Params = string(src[int(fd.Type.Params.Opening)-1 : int(fd.Type.Params.Closing)])
			} else {
				m.Params = "()"
			}
			all = append(all, m)
		}
	}
	out, _ := json.MarshalIndent(all, "", " ")
	fmt.Println(string(out))
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
	case *ast.IndexExpr:
		return exprString(t.X) + "[" + exprString(t.Index) + "]"
	case *ast.IndexListExpr:
		var args []string
		for _, a := range t.Indices {
			args = append(args, exprString(a))
		}
		return exprString(t.X) + "[" + strings.Join(args, ", ") + "]"
	default:
		return fmt.Sprintf("<%T>", e)
	}
}
