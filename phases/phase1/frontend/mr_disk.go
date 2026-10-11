package main

// WS G (P3-M9, module-probe lane): multi-result transformation for disk
// packages.
//
// The v1 frontend cannot emit multi-result functions (WS D). Synthetic
// packages handle this via hand-written __-helpers + the Appendix-M
// desugar table. For disk (third-party) packages we generate the same
// shape mechanically:
//
//   - For each multi-result FuncDecl F (func or method), generate:
//     type __F_pair struct { f1 T1; f2 T2 }   // f1..fN for N results
//     func __F(args...) __F_pair { ... }       // body with returns rewritten
//     func F(args...) (T1, T2) { panic("unreachable") }  // stub for type-check
//   - Register {pkg, F} -> __F in the dynamic desugar table; call sites
//     are rewritten during export by the Appendix-M machinery.
//   - After type-checking, the stubs are removed from the AST so the
//     exporter never sees them (it cannot emit multi-result bodies).
//
// This is general: any disk package with multi-result functions works,
// not just the module that exposed the gap.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sync"
)

// diskMRSpec describes one transformed multi-result function.
type diskMRSpec struct {
	pkgPath    string
	name       string // original name ("Parse")
	recvType   string // "" for funcs, receiver base name for methods
	helper     string // "__parse"
	pairType   string // "__parsePair"
	pairFields []string
	stub       *ast.FuncDecl // removed after type-check
}

var (
	diskMRMu    sync.Mutex
	diskMRSpecs []diskMRSpec
)

// transformDiskMultiResult rewrites multi-result FuncDecls in files to the
// __-helper + stub shape. Must run after parsing, before type-checking.
// Returns the specs for desugar registration (after type-check).
func transformDiskMultiResult(pkgPath string, files []*ast.File) []diskMRSpec {
	var specs []diskMRSpec
	// Collect multi-result funcs first (needed for return-call rewriting).
	mrNames := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if numResults(fd) > 1 {
				mrNames[fd.Name.Name] = true
			}
		}
	}
	if len(mrNames) == 0 {
		return nil
	}
	for _, f := range files {
		var newDecls []ast.Decl
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || numResults(fd) <= 1 {
				newDecls = append(newDecls, decl)
				continue
			}
			spec, pairDecl, helperDecl, stubDecl := transformMRFunc(pkgPath, f, fd, mrNames)
			newDecls = append(newDecls, pairDecl, helperDecl, stubDecl)
			specs = append(specs, spec)
		}
		f.Decls = newDecls
	}
	// Eta-expand intrinsic values: var x = time.Now -> var x = func() time.Time { return time.Now() }.
	// This is a general fix for intrinsics used in value position (M5 limitation).
	// Currently handles time.Now; extend as needed.
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 {
				return true
			}
			sel, ok := vs.Values[0].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Now" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "time" {
				// Rewrite to func lit.
				vs.Values[0] = &ast.FuncLit{
					Type: &ast.FuncType{
						Params: &ast.FieldList{},
						Results: &ast.FieldList{
							List: []*ast.Field{
								{Type: &ast.SelectorExpr{X: ast.NewIdent("time"), Sel: ast.NewIdent("Time")}},
							},
						},
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ReturnStmt{
								Results: []ast.Expr{
									&ast.CallExpr{Fun: sel},
								},
							},
						},
					},
				}
			}
			return true
		})
	}
	// Rewrite var initializers containing a multi-result call as the
	// sole argument to an outer call:
	//   var X = Must(Parse(args))
	// becomes:
	//   var __mr_tmp = __Parse(args)
	//   var X = Must(__mr_tmp.f1, __mr_tmp.f2)
	// This avoids needing export-time desugar for nested calls.
	for _, f := range files {
		var newDecls []ast.Decl
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				newDecls = append(newDecls, decl)
				continue
			}
			var newSpecs []ast.Spec
			var hoisted []ast.Decl
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					newSpecs = append(newSpecs, spec)
					continue
				}
				outer, ok := vs.Values[0].(*ast.CallExpr)
				if !ok || len(outer.Args) != 1 {
					newSpecs = append(newSpecs, spec)
					continue
				}
				inner, ok := outer.Args[0].(*ast.CallExpr)
				if !ok {
					newSpecs = append(newSpecs, spec)
					continue
				}
				innerName := mrCallName(inner)
				if innerName == "" || !mrNames[innerName] {
					newSpecs = append(newSpecs, spec)
					continue
				}
				// Hoist: var __mr_tmp_N = __Inner(args)
				tmpName := fmt.Sprintf("__mr_hoist_%d", len(hoisted))
				helperCall := &ast.CallExpr{
					Fun:  ast.NewIdent("__" + innerName),
					Args: inner.Args,
				}
				// Handle selector inner (pkg.Func -> pkg.__Func).
				if sel, ok := inner.Fun.(*ast.SelectorExpr); ok {
					helperCall.Fun = &ast.SelectorExpr{
						X:   sel.X,
						Sel: ast.NewIdent("__" + innerName),
					}
				}
				hoisted = append(hoisted, &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{
						&ast.ValueSpec{
							Names:  []*ast.Ident{ast.NewIdent(tmpName)},
							Values: []ast.Expr{helperCall},
						},
					},
				})
				// Rewrite outer args to spread pair fields.
				// Assume 2 fields (f1, f2); general case needs spec lookup.
				tmpIdent := ast.NewIdent(tmpName)
				outer.Args = []ast.Expr{
					&ast.SelectorExpr{X: tmpIdent, Sel: ast.NewIdent("f1")},
					&ast.SelectorExpr{X: tmpIdent, Sel: ast.NewIdent("f2")},
				}
				newSpecs = append(newSpecs, spec)
			}
			// Emit hoisted decls before the var decl.
			newDecls = append(newDecls, hoisted...)
			if len(newSpecs) > 0 {
				gd.Specs = newSpecs
				newDecls = append(newDecls, gd)
			}
		}
		f.Decls = newDecls
	}
	return specs
}

// numResults counts the result values of a FuncDecl.
func numResults(fd *ast.FuncDecl) int {
	if fd.Type.Results == nil {
		return 0
	}
	n := 0
	for _, fld := range fd.Type.Results.List {
		if len(fld.Names) == 0 {
			n++
		} else {
			n += len(fld.Names)
		}
	}
	return n
}

// resultTypes returns the result type exprs in order.
func resultTypes(fd *ast.FuncDecl) []ast.Expr {
	var out []ast.Expr
	if fd.Type.Results == nil {
		return out
	}
	for _, fld := range fd.Type.Results.List {
		if len(fld.Names) == 0 {
			out = append(out, fld.Type)
		} else {
			for range fld.Names {
				out = append(out, fld.Type)
			}
		}
	}
	return out
}

// transformMRFunc transforms one multi-result FuncDecl.
func transformMRFunc(pkgPath string, file *ast.File, fd *ast.FuncDecl, mrNames map[string]bool) (diskMRSpec, *ast.GenDecl, *ast.FuncDecl, *ast.FuncDecl) {
	name := fd.Name.Name
	recvType := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recvType = recvBaseName(fd.Recv.List[0].Type)
	}
	// Disambiguate methods on different receivers with the same name.
	prefix := name
	if recvType != "" {
		prefix = recvType + "_" + name
	}
	helper := "__" + prefix
	pairType := "__" + prefix + "Pair"

	rtypes := resultTypes(fd)
	pairFields := make([]string, len(rtypes))
	for i := range rtypes {
		pairFields[i] = fmt.Sprintf("f%d", i+1)
	}

	// Pair struct: type __F_pair struct { f1 T1; f2 T2 }.
	pairDecl := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(pairType),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: func() []*ast.Field {
							var fl []*ast.Field
							for i, rt := range rtypes {
								fl = append(fl, &ast.Field{
									Names: []*ast.Ident{ast.NewIdent(pairFields[i])},
									Type:  rt,
								})
							}
							return fl
						}(),
					},
				},
			},
		},
	}

	// Helper: for funcs, func __F(args...) __F_pair; for methods,
	// func __Recv_F(recv RecvType, args...) __Recv_FPair (receiver as
	// first param; the desugar uses prependRecv). The body uses the
	// receiver variable name, which we preserve as the param name.
	var helperParams *ast.FieldList
	var recvVarName, recvTypeExpr string
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		rv := fd.Recv.List[0]
		if len(rv.Names) > 0 {
			recvVarName = rv.Names[0].Name
		} else {
			recvVarName = "__recv"
		}
		recvTypeExpr = recvType
		// Prepend receiver param.
		helperParams = &ast.FieldList{
			List: append([]*ast.Field{
				{Names: []*ast.Ident{ast.NewIdent(recvVarName)}, Type: rv.Type},
			}, fd.Type.Params.List...),
		}
	} else {
		helperParams = fd.Type.Params
	}
	_ = recvTypeExpr
	// If the original had named results AND a bare return, declare
	// them as locals (the body references them; the pair return no
	// longer defines them). Only when needed to avoid "declared and
	// not used".
	rewrittenBody := rewriteMRReturns(fd.Body, pairType, pairFields, mrNames, fd, recvType, recvVarName)
	// Collect named result names.
	var resultNames []string
	if fd.Type.Results != nil {
		for _, fld := range fd.Type.Results.List {
			for _, n := range fld.Names {
				resultNames = append(resultNames, n.Name)
			}
		}
	}
	// Check if named results are used (bare return, or referenced by name).
	needsLocals := false
	if len(resultNames) > 0 {
		nameSet := map[string]bool{}
		for _, n := range resultNames {
			nameSet[n] = true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if rs, ok := n.(*ast.ReturnStmt); ok && len(rs.Results) == 0 {
				needsLocals = true
				return false
			}
			if id, ok := n.(*ast.Ident); ok && nameSet[id.Name] {
				needsLocals = true
				return false
			}
			return true
		})
	}
	var namedDecls []ast.Stmt
	if needsLocals && fd.Type.Results != nil {
		for _, fld := range fd.Type.Results.List {
			if len(fld.Names) > 0 {
				namedDecls = append(namedDecls, &ast.DeclStmt{
					Decl: &ast.GenDecl{
						Tok: token.VAR,
						Specs: []ast.Spec{
							&ast.ValueSpec{Names: fld.Names, Type: fld.Type},
						},
					},
				})
			}
		}
	}
	if len(namedDecls) > 0 {
		rewrittenBody.List = append(namedDecls, rewrittenBody.List...)
	}
	helperDecl := &ast.FuncDecl{
		Name: ast.NewIdent(helper),
		Type: &ast.FuncType{
			Params: helperParams,
			Results: &ast.FieldList{
				List: []*ast.Field{{Type: ast.NewIdent(pairType)}},
			},
		},
		Body: rewrittenBody,
	}

	// Stub: func F(args...) (T1, T2) { panic("unreachable") }.
	stubDecl := &ast.FuncDecl{
		Recv: fd.Recv,
		Name: ast.NewIdent(name),
		Type: fd.Type,
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun:  ast.NewIdent("panic"),
						Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"unreachable"`}},
					},
				},
			},
		},
	}

	spec := diskMRSpec{
		pkgPath:    pkgPath,
		name:       name,
		recvType:   recvType,
		helper:     helper,
		pairType:   pairType,
		pairFields: pairFields,
		stub:       stubDecl,
	}
	// The original fd is DISCARDED (replaced by helper+stub). We return
	// the three new decls; the caller splices them in.
	return spec, pairDecl, helperDecl, stubDecl
}

// recvBaseName extracts the base type name from a receiver type expr.
func recvBaseName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		case *ast.IndexExpr: // generic receiver: Box[T]
			return recvBaseName(t.X)
		case *ast.IndexListExpr:
			return recvBaseName(t.X)
		default:
			return ""
		}
	}
}

// rewriteMRReturns rewrites return statements in a multi-result body to
// pair construction. Handles:
//   - return a, b        -> return __Pair{a, b}
//   - return             -> return __Pair{n1, n2} (named results)
//   - return F(args)     -> __t := __F(args); return __Pair{__t.f1, __t.f2}
//     (F multi-result; spliced as two statements)
func rewriteMRReturns(body *ast.BlockStmt, pairType string, pairFields []string, mrNames map[string]bool, fd *ast.FuncDecl, recvType, recvVar string) *ast.BlockStmt {
	// Named result names for bare return.
	var namedResults []string
	if fd.Type.Results != nil {
		for _, fld := range fd.Type.Results.List {
			for _, n := range fld.Names {
				namedResults = append(namedResults, n.Name)
			}
		}
	}
	ctx := &mrRewriteCtx{
		pairType:     pairType,
		pairFields:   pairFields,
		namedResults: namedResults,
		mrNames:      mrNames,
		recvType:     recvType,
		recvVar:      recvVar,
	}
	ctx.rewriteBlock(body)
	return body
}

// mrRewriteCtx carries the rewrite context through nested blocks.
type mrRewriteCtx struct {
	pairType     string
	pairFields   []string
	namedResults []string
	mrNames      map[string]bool
	recvType     string
	recvVar      string
}

func (ctx *mrRewriteCtx) rewriteBlock(b *ast.BlockStmt) {
	var out []ast.Stmt
	for _, stmt := range b.List {
		out = append(out, ctx.rewriteStmt(stmt)...)
	}
	b.List = out
}

// rewriteStmt rewrites one statement, returning 1+ statements.
func (ctx *mrRewriteCtx) rewriteStmt(stmt ast.Stmt) []ast.Stmt {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return ctx.rewriteReturn(s)
	case *ast.BlockStmt:
		ctx.rewriteBlock(s)
		return []ast.Stmt{s}
	case *ast.IfStmt:
		ctx.rewriteBlock(s.Body)
		if s.Else != nil {
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				ctx.rewriteBlock(e)
			case *ast.IfStmt:
				// else-if: rewrite as a statement
				rewritten := ctx.rewriteStmt(e)
				if len(rewritten) == 1 {
					s.Else = rewritten[0]
				}
			}
		}
		return []ast.Stmt{s}
	case *ast.ForStmt:
		ctx.rewriteBlock(s.Body)
		return []ast.Stmt{s}
	case *ast.RangeStmt:
		ctx.rewriteBlock(s.Body)
		return []ast.Stmt{s}
	case *ast.SwitchStmt:
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if ccc, ok := cc.(*ast.CaseClause); ok {
					var out []ast.Stmt
					for _, st := range ccc.Body {
						out = append(out, ctx.rewriteStmt(st)...)
					}
					ccc.Body = out
				}
			}
		}
		return []ast.Stmt{s}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if ccc, ok := cc.(*ast.CaseClause); ok {
					var out []ast.Stmt
					for _, st := range ccc.Body {
						out = append(out, ctx.rewriteStmt(st)...)
					}
					ccc.Body = out
				}
			}
		}
		return []ast.Stmt{s}
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if ccc, ok := cc.(*ast.CommClause); ok {
					var out []ast.Stmt
					for _, st := range ccc.Body {
						out = append(out, ctx.rewriteStmt(st)...)
					}
					ccc.Body = out
				}
			}
		}
		return []ast.Stmt{s}
	default:
		return []ast.Stmt{stmt}
	}
}

// rewriteReturn rewrites one return statement, returning 1-2 statements.
func (ctx *mrRewriteCtx) rewriteReturn(rs *ast.ReturnStmt) []ast.Stmt {
	pairType, pairFields, namedResults := ctx.pairType, ctx.pairFields, ctx.namedResults
	mkPair := func(args []ast.Expr) *ast.ReturnStmt {
		return &ast.ReturnStmt{
			Return: rs.Return,
			Results: []ast.Expr{
				&ast.CompositeLit{
					Type: ast.NewIdent(pairType),
					Elts: args,
				},
			},
		}
	}
	switch len(rs.Results) {
	case 0:
		// Bare return: use named results.
		if len(namedResults) != len(pairFields) {
			// Should not happen for valid multi-result; leave as-is
			// (type-check will catch it).
			return []ast.Stmt{rs}
		}
		args := make([]ast.Expr, len(namedResults))
		for i, n := range namedResults {
			args[i] = ast.NewIdent(n)
		}
		return []ast.Stmt{mkPair(args)}
	case 1:
		// Single expr which is a call: in the ORIGINAL code this
		// type-checked as a multi-result call forwarding its values
		// (e.g. return vr.Value()). It must be multi-result, so rewrite
		// it. We don't need mrNames here.
		if call, ok := rs.Results[0].(*ast.CallExpr); ok {
			// For methods (selector calls): use two-value assignment
			// __t1, __t2 := x.M(args); return Pair{__t1, __t2}. This
			// works for both concrete and interface receivers; the
			// desugar rewrites the call during export.
			if _, ok := call.Fun.(*ast.SelectorExpr); ok {
				tmps := make([]ast.Expr, len(pairFields))
				for i := range pairFields {
					tmps[i] = ast.NewIdent(fmt.Sprintf("__mr_t%d", i+1))
				}
				assign := &ast.AssignStmt{
					Lhs: tmps,
					Tok: token.DEFINE,
					Rhs: []ast.Expr{call},
				}
				args := make([]ast.Expr, len(pairFields))
				for i := range pairFields {
					// Use fresh Ident nodes for the uses (not the def sites).
					args[i] = ast.NewIdent(fmt.Sprintf("__mr_t%d", i+1))
				}
				return []ast.Stmt{assign, mkPair(args)}
			}
			// Package-level func: __t := __F(args); return Pair{__t.f1, __t.f2}.
			var fname string
			if id, ok := call.Fun.(*ast.Ident); ok {
				fname = id.Name
			} else if id := callTargetIdent(call.Fun); id != nil {
				fname = id.Name
			}
			tmp := ast.NewIdent("__mr_tmp")
			helperCall := &ast.CallExpr{
				Fun:  ast.NewIdent("__" + fname),
				Args: call.Args,
			}
			assign := &ast.AssignStmt{
				Lhs: []ast.Expr{tmp},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{helperCall},
			}
			args := make([]ast.Expr, len(pairFields))
			for i, f := range pairFields {
				args[i] = &ast.SelectorExpr{X: tmp, Sel: ast.NewIdent(f)}
			}
			return []ast.Stmt{assign, mkPair(args)}
		}
		return []ast.Stmt{rs}
	default:
		// return a, b, ... -> return Pair{a, b, ...}
		return []ast.Stmt{mkPair(rs.Results)}
	}
}

// removeDiskMRStubs removes stub FuncDecls from files after type-checking.
func removeDiskMRStubs(files []*ast.File, specs []diskMRSpec) {
	stubSet := map[*ast.FuncDecl]bool{}
	for _, s := range specs {
		stubSet[s.stub] = true
	}
	for _, f := range files {
		var out []ast.Decl
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && stubSet[fd] {
				continue
			}
			out = append(out, decl)
		}
		f.Decls = out
	}
}

// registerDiskMRSpecs adds specs to the dynamic desugar table.
func registerDiskMRSpecs(specs []diskMRSpec) {
	diskMRMu.Lock()
	diskMRSpecs = append(diskMRSpecs, specs...)
	diskMRMu.Unlock()
}

// lookupDiskMR finds a dynamic desugar entry.
func lookupDiskMR(pkgPath, name, recvType string) *diskMRSpec {
	diskMRMu.Lock()
	defer diskMRMu.Unlock()
	for i := range diskMRSpecs {
		s := &diskMRSpecs[i]
		if s.pkgPath == pkgPath && s.name == name && s.recvType == recvType {
			return s
		}
	}
	return nil
}

// diskMRPairType returns the *types.Named pair type from the disk package.
func diskMRPairType(sp *syntheticPackage, spec *diskMRSpec) *types.Named {
	obj := sp.pkg.Scope().Lookup(spec.pairType)
	if obj == nil {
		return nil
	}
	tn, _ := obj.Type().(*types.Named)
	return tn
}

// classifyDiskMultiResultCall desugars multi-result calls to disk-package
// functions transformed by transformDiskMultiResult. Mirrors
// classifyM9MultiResultCall but consults the dynamic table and resolves
// the pair type from the disk-built package.
func classifyDiskMultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	spec := lookupDiskMR(pkgPath, fn.Name(), recvName)
	if spec == nil {
		return nil
	}
	if sig.Results().Len() != len(spec.pairFields) {
		return nil
	}
	sp, err := getDiskPackage(pkgPath)
	if err != nil {
		return nil
	}
	nt := diskMRPairType(sp, spec)
	if nt == nil {
		return nil
	}
	nt = instantiatePairType(nt, targs)
	return &mrPlan{
		kind:        mrM7,
		helperPkg:   pkgPath,
		helperName:  spec.helper,
		tmpType:     nt,
		pairFields:  spec.pairFields,
		prependRecv: recvName != "",
		isDisk:      true,
	}
}

// mrCallName returns the function/method name of a call.
func mrCallName(call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		return id.Name
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	if id := callTargetIdent(call.Fun); id != nil {
		return id.Name
	}
	return ""
}
