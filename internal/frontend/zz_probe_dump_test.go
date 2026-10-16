package main

// THROWAWAY probe harness for the FE-1 lane. Dumps transformDiskMultiResult
// output for probe sources so original vs transformed can be compared
// under gc. DELETED before commit — not part of the change.

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZZProbeDump(t *testing.T) {
	srcDir := os.Getenv("PROBE_SRC")
	outDir := os.Getenv("PROBE_OUT")
	if srcDir == "" || outDir == "" {
		t.Skip("PROBE_SRC/PROBE_OUT not set")
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		dumpProbeOne(t, srcDir, outDir, e.Name())
	}
}

func dumpProbeOne(t *testing.T, srcDir, outDir, name string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(srcDir, name))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("%s: parse: %v", name, err)
	}
	specs := transformDiskMultiResult("main", []*ast.File{f})
	removeDiskMRStubs([]*ast.File{f}, specs)
	spliceProbeCallSites(t, f, specs)
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		t.Fatalf("%s: format: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(outDir, name), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// spliceProbeCallSites rewrites `lhs := f(args)` (f a transformed
// package-level MR func) into
//
//	__probe_tN := __f(args); lhs := __probe_tN.f1, __probe_tN.f2
//
// and `lhs := r.m(args)` (method) into the __Recv_m(r, args) form. Only
// main's body is touched; anything unsplicable fails the test loudly.
func spliceProbeCallSites(t *testing.T, f *ast.File, specs []diskMRSpec) {
	t.Helper()
	byName := map[string]*diskMRSpec{}
	var methods []*diskMRSpec
	for i := range specs {
		s := &specs[i]
		if s.helper == "" {
			continue
		}
		if s.recvType == "" {
			byName[s.name] = s
		} else {
			methods = append(methods, s)
		}
	}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "main" || fd.Body == nil {
			continue
		}
		var out []ast.Stmt
		tmpN := 0
		for _, stmt := range fd.Body.List {
			as, ok := stmt.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				out = append(out, stmt)
				continue
			}
			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				out = append(out, stmt)
				continue
			}
			rep, ok := spliceProbeCall(t, call, as, byName, methods, tmpN)
			if !ok {
				t.Fatalf("unsplicable call site in probe main: %v", call.Fun)
			}
			tmpN++
			out = append(out, rep...)
		}
		fd.Body.List = out
	}
}

func spliceProbeCall(t *testing.T, call *ast.CallExpr, as *ast.AssignStmt, byName map[string]*diskMRSpec, methods []*diskMRSpec, tmpN int) ([]ast.Stmt, bool) {
	t.Helper()
	var spec *diskMRSpec
	var args []ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		spec = byName[fun.Name]
		if spec == nil {
			return nil, false
		}
		args = call.Args
	case *ast.SelectorExpr:
		for _, s := range methods {
			if s.name == fun.Sel.Name {
				spec = s
				break
			}
		}
		if spec == nil {
			return nil, false
		}
		// Prepend the receiver (matches the transform's prependRecv).
		args = append([]ast.Expr{fun.X}, call.Args...)
	default:
		return nil, false
	}
	tmp := ast.NewIdent(sprintfTmp(tmpN))
	first := &ast.AssignStmt{
		Lhs: []ast.Expr{tmp},
		Tok: token.DEFINE,
		Rhs: []ast.Expr{&ast.CallExpr{Fun: ast.NewIdent(spec.helper), Args: args}},
	}
	rhs := make([]ast.Expr, len(spec.pairFields))
	for i, pf := range spec.pairFields {
		rhs[i] = &ast.SelectorExpr{X: ast.NewIdent(tmp.Name), Sel: ast.NewIdent(pf)}
	}
	spread := &ast.AssignStmt{Lhs: as.Lhs, Tok: as.Tok, Rhs: rhs}
	return []ast.Stmt{first, spread}, true
}

func sprintfTmp(n int) string {
	if n == 0 {
		return "__probe_t"
	}
	return "__probe_t" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
