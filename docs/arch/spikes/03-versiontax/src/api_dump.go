// api_dump dumps the exported API of every package under a Go tree's src/go,
// one canonical line per exported declaration (funcs incl. methods, types,
// vars, consts; struct fields and interface methods flattened).
// Usage: api_dump <go_src_root> <out>
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func render(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	printer.Fprint(&sb, fset, n)
	return sb.String()
}

func main() {
	root, out := os.Args[1], os.Args[2]
	var lines []string
	fset := token.NewFileSet()
	err := filepath.Walk(filepath.Join(root, "src/go"), func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || strings.Contains(path, "testdata") {
			return nil
		}
		pkgs, err := parser.ParseDir(fset, path, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, parser.SkipObjectResolution)
		if err != nil || len(pkgs) == 0 {
			return nil
		}
		for pname, pkg := range pkgs {
			if strings.HasSuffix(pname, "_test") {
				continue
			}
			imp := strings.TrimPrefix(path, filepath.Join(root, "src")+"/")
			for _, f := range pkg.Files {
				for _, d := range f.Decls {
					switch decl := d.(type) {
					case *ast.FuncDecl:
						if !decl.Name.IsExported() {
							continue
						}
						var recv string
						if decl.Recv != nil && len(decl.Recv.List) > 0 {
							recv = "(" + render(fset, decl.Recv.List[0].Type) + ")."
						}
						lines = append(lines, fmt.Sprintf("func %s%s%s", imp, recv, decl.Name.Name) +
							"(" + params(fset, decl.Type.Params) + ")" + results(fset, decl.Type.Results))
					case *ast.GenDecl:
						for _, s := range decl.Specs {
							switch sp := s.(type) {
							case *ast.TypeSpec:
								if !sp.Name.IsExported() {
									continue
								}
								lines = append(lines, fmt.Sprintf("type %s.%s %s", imp, sp.Name.Name, render(fset, sp.Type)))
								switch t := sp.Type.(type) {
								case *ast.StructType:
									for _, fld := range t.Fields.List {
										for _, n := range fld.Names {
											if n.IsExported() {
												lines = append(lines, fmt.Sprintf("field %s.%s.%s %s", imp, sp.Name.Name, n.Name, render(fset, fld.Type)))
											}
										}
									}
								case *ast.InterfaceType:
									for _, m := range t.Methods.List {
										if len(m.Names) > 0 && m.Names[0].IsExported() {
											lines = append(lines, fmt.Sprintf("method %s.%s.%s%s", imp, sp.Name.Name, m.Names[0].Name, render(fset, m.Type)))
										}
									}
								}
							case *ast.ValueSpec:
								for _, n := range sp.Names {
									if n.IsExported() {
										lines = append(lines, fmt.Sprintf("var %s.%s", imp, n.Name))
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Strings(lines)
	os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	fmt.Println(len(lines), "exported declarations")
}

func params(fset *token.FileSet, p *ast.FieldList) string {
	if p == nil {
		return ""
	}
	var parts []string
	for _, f := range p.List {
		t := render(fset, f.Type)
		for range f.Names {
			parts = append(parts, t)
		}
		if len(f.Names) == 0 {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ",")
}

func results(fset *token.FileSet, r *ast.FieldList) string {
	if r == nil {
		return ""
	}
	if len(r.List) == 1 && len(r.List[0].Names) == 0 {
		return " " + render(fset, r.List[0].Type)
	}
	return " (" + params(fset, r) + ")"
}
