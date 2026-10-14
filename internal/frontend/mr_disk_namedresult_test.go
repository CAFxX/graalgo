package main

// P3-M9 (driver-harden, go-cmp lane): regression tests for the
// transformDiskMultiResult named-result fix. A multi-result function whose
// named results are not all referenced in the body is legal Go (named
// results need not be used), but the old transform declared every named
// result as a local in the __-helper, producing "declared and not used"
// for the unreferenced ones. The transform must declare only the names
// the body actually reads (or all of them when there is a bare return).

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// checkMRTransform parses src, runs transformDiskMultiResult, and
// type-checks the result, returning the type-check error (if any).
func checkMRTransform(t *testing.T, src string) error {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	transformDiskMultiResult("p", []*ast.File{f})
	conf := types.Config{Importer: importer.Default()}
	_, err = conf.Check("p", fset, []*ast.File{f}, nil)
	return err
}

// TestMRTransformUnusedNamedResult: the onePassPrefix shape — a named
// result (prefix) that the body never references must not be declared as
// an unused local in the helper.
func TestMRTransformUnusedNamedResult(t *testing.T) {
	src := `package p

func onePassPrefix() (prefix string, complete bool, pc uint32) {
	complete = true
	pc = 7
	return "", complete, pc
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformBareReturnKeepsAllNames: a bare return reads every named
// result, so all of them must be declared as locals.
func TestMRTransformBareReturnKeepsAllNames(t *testing.T) {
	src := `package p

func f(cond bool) (a string, b int) {
	if cond {
		a = "x"
		b = 1
		return
	}
	return "y", 2
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformSelectorNotAResultRef: a field access like re.prefix
// must not count as a reference to a named result named prefix (the
// regexp.Regexp.LiteralPrefix shape).
func TestMRTransformSelectorNotAResultRef(t *testing.T) {
	src := `package p

type T struct {
	prefix string
	prefixComplete bool
}

func (re *T) LiteralPrefix() (prefix string, complete bool) {
	return re.prefix, re.prefixComplete
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformAllNamedUsed: when every named result is referenced, the
// helper keeps working as before.
func TestMRTransformAllNamedUsed(t *testing.T) {
	src := `package p

func g() (x string, y string) {
	x = "a"
	y = "b"
	return x, y
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformNoNamedResults: unnamed results need no locals at all.
func TestMRTransformNoNamedResults(t *testing.T) {
	src := `package p

func h() (string, int) {
	return "a", 1
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformHelperOmitsUnusedName: pin the wire shape — the helper's
// leading var decls must not mention the unreferenced name.
func TestMRTransformHelperOmitsUnusedName(t *testing.T) {
	src := `package p

func onePassPrefix() (prefix string, complete bool, pc uint32) {
	complete = true
	pc = 7
	return "", complete, pc
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	transformDiskMultiResult("p", []*ast.File{f})
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fd.Name.Name, "__") {
			continue
		}
		for _, stmt := range fd.Body.List {
			ds, ok := stmt.(*ast.DeclStmt)
			if !ok {
				break // locals done; rest is body
			}
			gd, ok := ds.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, n := range vs.Names {
					if n.Name == "prefix" {
						t.Errorf("helper declares unused named result 'prefix' as local")
					}
				}
			}
		}
	}
}
