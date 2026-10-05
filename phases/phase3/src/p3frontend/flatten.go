package p3frontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/format"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// prefixFor returns the package rename prefix for an import path:
// p3i_<first 8 hex of sha256(importpath)>_. Content-derived, therefore
// deterministic and collision-resistant across packages.
func prefixFor(importPath string) string {
	sum := sha256.Sum256([]byte(importPath))
	return "p3i_" + hex.EncodeToString(sum[:])[:8] + "_"
}

// Flatten merges the dependency closure into one package-main source file.
// deps must be in dependency order (LoadClosure). Every package-level
// identifier of every dep is renamed with its content-derived prefix and
// every use is rewritten via go/types Info (shadowing-safe); import specs
// of flattened packages are dropped (runtime/fmt specs are kept).
// The result is gofmt-rendered and deterministic for a fixed input.
func Flatten(fset *token.FileSet, imp *Importer, mainRef *PackageRef, deps []*PackageRef) (string, error) {
	mainPkg := imp.Package(mainRef.ImportPath)
	if mainPkg == nil {
		return "", fmt.Errorf("p3frontend: main package %s was not type-checked", mainRef.ImportPath)
	}

	// Scope checks first (fail fast, before any rewriting).
	allRefs := append(append([]*PackageRef{}, deps...), mainRef)
	for _, ref := range allRefs {
		for _, f := range imp.Files(ref.ImportPath) {
			if err := rejectInitFunc(ref, f); err != nil {
				return "", err
			}
		}
	}
	for _, dep := range deps {
		if dep.PkgName == "main" {
			return "", fmt.Errorf("p3frontend: dependency %q declares package main", dep.ImportPath)
		}
	}
	// The p3i_ namespace belongs to the encoding: a user identifier there
	// would silently collide with a renamed one, so it is rejected.
	for _, ref := range allRefs {
		pkg := imp.Package(ref.ImportPath)
		for _, name := range pkg.Scope().Names() {
			if strings.HasPrefix(name, "p3i_") {
				return "", fmt.Errorf("p3frontend: package %q: identifier %q uses the reserved p3i_ prefix", ref.ImportPath, name)
			}
		}
	}

	// Rename map: every package-level object of every dep -> new name.
	renames := map[types.Object]string{}
	for _, dep := range deps {
		pkg := imp.Package(dep.ImportPath)
		prefix := prefixFor(dep.ImportPath)
		for _, name := range pkg.Scope().Names() {
			obj := pkg.Scope().Lookup(name)
			renames[obj] = prefix + name
		}
	}
	dropPaths := map[string]bool{}
	for _, dep := range deps {
		dropPaths[dep.ImportPath] = true
	}

	rw := &rewriter{renames: renames, dropPaths: dropPaths}

	merged := &ast.File{Name: ast.NewIdent("main")}
	var keptImports []*ast.ImportSpec
	seenImport := map[string]bool{} // path+"\x00"+name -> seen

	emit := func(ref *PackageRef) {
		rw.info = imp.Info(ref.ImportPath)
		for _, f := range imp.Files(ref.ImportPath) {
			merged.Comments = append(merged.Comments, retainedComments(f)...)
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok {
					merged.Decls = append(merged.Decls, rw.decl(decl))
					continue
				}
				if gd.Tok == token.IMPORT {
					for _, spec := range gd.Specs {
						ispec := spec.(*ast.ImportSpec)
						path := strings.Trim(ispec.Path.Value, "\"`")
						if dropPaths[path] {
							continue // flattened: the import is gone, uses rewritten
						}
						name := ""
						if ispec.Name != nil {
							name = ispec.Name.Name
						}
						key := path + "\x00" + name
						if seenImport[key] {
							continue
						}
						seenImport[key] = true
						keptImports = append(keptImports, ispec)
					}
					continue
				}
				merged.Decls = append(merged.Decls, rw.decl(decl))
			}
		}
	}
	for _, dep := range deps {
		emit(dep)
	}
	emit(mainRef)

	if len(keptImports) > 0 {
		sort.Slice(keptImports, func(i, j int) bool {
			pi := strings.Trim(keptImports[i].Path.Value, "\"`")
			pj := strings.Trim(keptImports[j].Path.Value, "\"`")
			if pi != pj {
				return pi < pj
			}
			ni, nj := "", ""
			if keptImports[i].Name != nil {
				ni = keptImports[i].Name.Name
			}
			if keptImports[j].Name != nil {
				nj = keptImports[j].Name.Name
			}
			return ni < nj
		})
		specs := make([]ast.Spec, len(keptImports))
		for i, s := range keptImports {
			specs[i] = s
		}
		merged.Decls = append([]ast.Decl{
			&ast.GenDecl{Tok: token.IMPORT, Specs: specs},
		}, merged.Decls...)
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, merged); err != nil {
		return "", fmt.Errorf("p3frontend: formatting merged source: %w", err)
	}
	return buf.String(), nil
}

// retainedComments returns f's comments minus build-constraint lines:
// //go:build and // +build lines are file-selection directives, not program
// content, and must not survive into the merged program.
func retainedComments(f *ast.File) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, cg := range f.Comments {
		keep := false
		for _, c := range cg.List {
			line := strings.TrimSpace(c.Text)
			if !constraint.IsGoBuild(line) && !constraint.IsPlusBuild(line) {
				keep = true
				break
			}
		}
		if keep {
			out = append(out, cg)
		}
	}
	return out
}

// rejectInitFunc fails fast on func init: the frozen exporter type-checks
// init functions but the runtime never executes them (probed 2026-10-06),
// so letting one through would be a silent gc divergence.
func rejectInitFunc(ref *PackageRef, f *ast.File) error {
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "init" {
			return fmt.Errorf("p3frontend: package %q: func init() is not supported in M1 (frontend/v1 exports init functions but the runtime never executes them)", ref.ImportPath)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// rewriter: type-driven identifier renaming.
// ---------------------------------------------------------------------------

// rewriter renames identifiers via go/types Info. renames maps a
// package-level object of a flattened dependency to its new name; info is
// the *types.Info of the file currently being rewritten; dropPaths are the
// flattened import paths whose import specs are dropped.
type rewriter struct {
	renames   map[types.Object]string
	info      *types.Info
	dropPaths map[string]bool
}

// identObj resolves an identifier to its object (declaration or use).
func (rw *rewriter) identObj(id *ast.Ident) types.Object {
	if obj, ok := rw.info.Defs[id]; ok {
		// Embedded field declaration: go/types records the field's type
		// identifier in BOTH Defs (the synthetic field *types.Var) and
		// Uses (the *types.TypeName). Preferring Defs would resolve
		// struct{ T } to the field Var, miss the rename map, and silently
		// rebind the embedded type to a colliding local T — prefer the
		// type whenever both are present.
		if v, ok := obj.(*types.Var); ok && v.IsField() && v.Embedded() {
			if u, ok := rw.info.Uses[id]; ok {
				return u
			}
		}
		return obj // may be nil (blank); the rename map lookup then misses
	}
	return rw.info.Uses[id]
}

// renameEmbeddedField renames id in place when it denotes an embedded
// struct field whose type was renamed. For struct{ T } the field name IS
// the type name, so every use of the field must track the renamed type:
//   - the field declaration's type identifier (via identObj's Defs/Uses
//     disambiguation above),
//   - selectors naming the field (o.T, including promoted selectors —
//     go/types records Sel in Uses as the field *types.Var, which is never
//     in the rename map),
//   - keyed composite-literal elements (Outer{T: ...} — keys are recorded
//     in Uses as the field *types.Var).
//
// No-op when the field's type was not renamed (e.g. main's own types).
func (rw *rewriter) renameEmbeddedField(id *ast.Ident) {
	v, ok := rw.info.Uses[id].(*types.Var)
	if !ok || !v.IsField() || !v.Embedded() {
		return
	}
	t := v.Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem() // struct{ *T }: the field is still named T
	}
	n, ok := t.(*types.Named)
	if !ok {
		return
	}
	if nn, ok := rw.renames[n.Obj()]; ok {
		id.Name = nn
	}
}

// ident renames id in place when it resolves to a renamed object.
func (rw *rewriter) ident(id *ast.Ident) *ast.Ident {
	if id == nil {
		return nil
	}
	if nn, ok := rw.renames[rw.identObj(id)]; ok {
		id.Name = nn
	}
	return id
}

// expr rewrites e, returning the (possibly replaced) expression. The only
// structural replacement is pkg.X -> p3i_..._X when X resolves to a renamed
// package-level object; everything else is in-place identifier renaming.
func (rw *rewriter) expr(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case nil:
		return nil
	case *ast.Ident:
		return rw.ident(x)
	case *ast.SelectorExpr:
		// Package-qualified use of a renamed object: collapse the whole
		// selector (the qualifier would dangle — its import spec is dropped).
		if nn, ok := rw.renames[rw.info.Uses[x.Sel]]; ok {
			return &ast.Ident{NamePos: x.Sel.NamePos, Name: nn}
		}
		// Embedded-field selector (o.T): rename the field to the renamed
		// type name so it keeps denoting the dependency's type.
		rw.renameEmbeddedField(x.Sel)
		x.X = rw.expr(x.X)
		return x
	case *ast.ParenExpr:
		x.X = rw.expr(x.X)
		return x
	case *ast.StarExpr:
		x.X = rw.expr(x.X)
		return x
	case *ast.UnaryExpr:
		x.X = rw.expr(x.X)
		return x
	case *ast.BinaryExpr:
		x.X = rw.expr(x.X)
		x.Y = rw.expr(x.Y)
		return x
	case *ast.CallExpr:
		x.Fun = rw.expr(x.Fun)
		for i, a := range x.Args {
			x.Args[i] = rw.expr(a)
		}
		return x
	case *ast.IndexExpr:
		x.X = rw.expr(x.X)
		x.Index = rw.expr(x.Index)
		return x
	case *ast.IndexListExpr:
		x.X = rw.expr(x.X)
		for i, ix := range x.Indices {
			x.Indices[i] = rw.expr(ix)
		}
		return x
	case *ast.SliceExpr:
		x.X = rw.expr(x.X)
		x.Low = rw.expr(x.Low)
		x.High = rw.expr(x.High)
		x.Max = rw.expr(x.Max)
		return x
	case *ast.TypeAssertExpr:
		x.X = rw.expr(x.X)
		x.Type = rw.expr(x.Type)
		return x
	case *ast.CompositeLit:
		x.Type = rw.expr(x.Type)
		for i, el := range x.Elts {
			x.Elts[i] = rw.expr(el)
		}
		return x
	case *ast.KeyValueExpr:
		// Keyed literal naming an embedded field (Outer{T: ...}): the key
		// denotes the field Var, so it needs the embedded-field treatment
		// to track the renamed type.
		if key, ok := x.Key.(*ast.Ident); ok {
			rw.renameEmbeddedField(key)
		}
		x.Key = rw.expr(x.Key)
		x.Value = rw.expr(x.Value)
		return x
	case *ast.FuncLit:
		x.Type = rw.funcType(x.Type)
		x.Body = rw.block(x.Body)
		return x
	case *ast.ArrayType:
		x.Len = rw.expr(x.Len)
		x.Elt = rw.expr(x.Elt)
		return x
	case *ast.StructType:
		x.Fields = rw.fieldList(x.Fields)
		return x
	case *ast.FuncType:
		return rw.funcType(x)
	case *ast.InterfaceType:
		x.Methods = rw.fieldList(x.Methods)
		return x
	case *ast.MapType:
		x.Key = rw.expr(x.Key)
		x.Value = rw.expr(x.Value)
		return x
	case *ast.ChanType:
		x.Value = rw.expr(x.Value)
		return x
	case *ast.Ellipsis:
		x.Elt = rw.expr(x.Elt)
		return x
	case *ast.BasicLit, *ast.BadExpr:
		return x
	default:
		return x
	}
}

func (rw *rewriter) funcType(ft *ast.FuncType) *ast.FuncType {
	if ft == nil {
		return nil
	}
	ft.TypeParams = rw.fieldList(ft.TypeParams)
	ft.Params = rw.fieldList(ft.Params)
	ft.Results = rw.fieldList(ft.Results)
	return ft
}

func (rw *rewriter) fieldList(fl *ast.FieldList) *ast.FieldList {
	if fl == nil {
		return nil
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			rw.ident(n)
		}
		f.Type = rw.expr(f.Type)
	}
	return fl
}

func (rw *rewriter) block(b *ast.BlockStmt) *ast.BlockStmt {
	if b == nil {
		return nil
	}
	for i, s := range b.List {
		b.List[i] = rw.stmt(s)
	}
	return b
}

func (rw *rewriter) stmt(s ast.Stmt) ast.Stmt {
	switch x := s.(type) {
	case nil:
		return nil
	case *ast.DeclStmt:
		x.Decl = rw.decl(x.Decl)
		return x
	case *ast.EmptyStmt, *ast.BadStmt:
		return x
	case *ast.LabeledStmt:
		rw.ident(x.Label)
		x.Stmt = rw.stmt(x.Stmt)
		return x
	case *ast.ExprStmt:
		x.X = rw.expr(x.X)
		return x
	case *ast.SendStmt:
		x.Chan = rw.expr(x.Chan)
		x.Value = rw.expr(x.Value)
		return x
	case *ast.IncDecStmt:
		x.X = rw.expr(x.X)
		return x
	case *ast.AssignStmt:
		for i, lhs := range x.Lhs {
			x.Lhs[i] = rw.expr(lhs)
		}
		for i, rhs := range x.Rhs {
			x.Rhs[i] = rw.expr(rhs)
		}
		return x
	case *ast.GoStmt:
		x.Call = rw.expr(x.Call).(*ast.CallExpr)
		return x
	case *ast.DeferStmt:
		x.Call = rw.expr(x.Call).(*ast.CallExpr)
		return x
	case *ast.ReturnStmt:
		for i, r := range x.Results {
			x.Results[i] = rw.expr(r)
		}
		return x
	case *ast.BranchStmt:
		rw.ident(x.Label)
		return x
	case *ast.BlockStmt:
		return rw.block(x)
	case *ast.IfStmt:
		x.Init = rw.stmt(x.Init)
		x.Cond = rw.expr(x.Cond)
		x.Body = rw.block(x.Body)
		x.Else = rw.stmt(x.Else)
		return x
	case *ast.CaseClause:
		for i, c := range x.List {
			x.List[i] = rw.expr(c)
		}
		for i, b := range x.Body {
			x.Body[i] = rw.stmt(b)
		}
		return x
	case *ast.SwitchStmt:
		x.Init = rw.stmt(x.Init)
		x.Tag = rw.expr(x.Tag)
		x.Body = rw.block(x.Body)
		return x
	case *ast.TypeSwitchStmt:
		x.Init = rw.stmt(x.Init)
		x.Assign = rw.stmt(x.Assign)
		x.Body = rw.block(x.Body)
		return x
	case *ast.CommClause:
		x.Comm = rw.stmt(x.Comm)
		for i, b := range x.Body {
			x.Body[i] = rw.stmt(b)
		}
		return x
	case *ast.SelectStmt:
		x.Body = rw.block(x.Body)
		return x
	case *ast.ForStmt:
		x.Init = rw.stmt(x.Init)
		x.Cond = rw.expr(x.Cond)
		x.Post = rw.stmt(x.Post)
		x.Body = rw.block(x.Body)
		return x
	case *ast.RangeStmt:
		x.Key = rw.expr(x.Key)
		x.Value = rw.expr(x.Value)
		x.X = rw.expr(x.X)
		x.Body = rw.block(x.Body)
		return x
	default:
		return x
	}
}

func (rw *rewriter) decl(d ast.Decl) ast.Decl {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil {
			x.Recv = rw.fieldList(x.Recv)
		}
		rw.ident(x.Name)
		x.Type = rw.funcType(x.Type)
		x.Body = rw.block(x.Body)
		return x
	case *ast.GenDecl:
		for _, spec := range x.Specs {
			switch sp := spec.(type) {
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					rw.ident(n)
				}
				sp.Type = rw.expr(sp.Type)
				for i, v := range sp.Values {
					sp.Values[i] = rw.expr(v)
				}
			case *ast.TypeSpec:
				rw.ident(sp.Name)
				sp.TypeParams = rw.fieldList(sp.TypeParams)
				sp.Type = rw.expr(sp.Type)
			case *ast.ImportSpec:
				// Import specs are filtered in Flatten; kept ones need
				// no identifier rewriting.
			}
		}
		return x
	default:
		return x
	}
}
