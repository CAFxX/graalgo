package main

// Multi-result transformation for disk AND synthetic packages
// (real-fmt2 lane: generalized from the WS G disk-only transform).
//
// The v1 frontend cannot emit multi-result functions (WS D). Synthetic
// packages historically handled this via hand-written __-helpers + the
// Appendix-M desugar table; disk (third-party) packages get the same
// shape generated mechanically. The transform now runs on both:
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
// Collision rule: a FuncDecl whose generated helper (__F) or pair type
// (__FPair) name already exists as a top-level declaration is SKIPPED.
// Those are the packages with hand-written __-helpers, whose calls keep
// flowing through the static Appendix-M tables (checked before the
// dynamic table). This keeps the mechanical transform from clobbering
// hand-written helpers.
//
// This is general: any package with multi-result functions works,
// not just the module that exposed the gap.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"sync"
)

// mrSpec describes one transformed multi-result function.
type mrSpec struct {
	pkgPath    string
	name       string // original name ("Parse")
	recvType   string // "" for funcs, receiver base name for methods
	helper     string // "__parse"
	pairType   string // "__parsePair"
	pairFields []string
	stub       *ast.FuncDecl // removed after type-check
}

var (
	mrSpecsMu    sync.Mutex
	mrSpecs []mrSpec
)

// transformTargetCollides reports whether transforming fd would generate
// a helper or pair-type name that already exists as a top-level
// declaration (a hand-written __-helper). Mirrors transformMRFunc's
// naming: __F / __FPair for funcs, __Recv_F / __Recv_FPair for methods.
func transformTargetCollides(fd *ast.FuncDecl, existing map[string]bool) bool {
	prefix := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		if rt := recvBaseName(fd.Recv.List[0].Type); rt != "" {
			prefix = rt + "_" + fd.Name.Name
		}
	}
	return existing["__"+prefix] || existing["__"+prefix+"Pair"]
}

// transformMultiResult rewrites multi-result FuncDecls in files to the
// __-helper + stub shape. Must run after parsing, before type-checking.
// Returns the specs for desugar registration (after type-check).
func transformMultiResult(pkgPath string, files []*ast.File) []mrSpec {
	var specs []mrSpec
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
	// Collision guard: collect every existing top-level name BEFORE the
	// transform runs. A FuncDecl whose generated helper (__F) or pair
	// type (__FPair) name already exists is skipped — those packages
	// carry hand-written __-helpers served by the static Appendix-M
	// tables, and generating a second __F would be a redeclaration.
	existing := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				existing[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						existing[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							existing[n.Name] = true
						}
					}
				}
			}
		}
	}
	for _, f := range files {
		var newDecls []ast.Decl
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || numResults(fd) <= 1 {
				newDecls = append(newDecls, decl)
				continue
			}
			if transformTargetCollides(fd, existing) {
				newDecls = append(newDecls, decl)
				continue
			}
			// Skip functions served by the static Appendix-M tables
			// (hand-written __-helpers with ad-hoc names).
			recvType := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recvType = recvBaseName(fd.Recv.List[0].Type)
			}
			if hasStaticMREntry(pkgPath, fd.Name.Name, recvType) {
				newDecls = append(newDecls, decl)
				continue
			}
			// Skip synthetic fail-fast stubs: a body that is a single
			// panic("... not in M{5,7,...} scope ...") must be preserved.
			// The mechanical transform would replace the fail-fast with
			// a working __-helper, breaking the fail-fast contract.
			if isFailFastStub(fd) {
				newDecls = append(newDecls, decl)
				continue
			}
			spec, pairDecl, helperDecl, stubDecl := transformMRFunc(pkgPath, f, fd, mrNames)
			newDecls = append(newDecls, pairDecl, helperDecl, stubDecl)
			specs = append(specs, spec)
		}
		f.Decls = newDecls
	}
	// BUG-MODPROBE-005: interface method specs with multi-result.
	// Generate a pair type per (interface, method) and register a spec
	// so interface dispatch (classifyTransformedIfaceMRCall) can
	// desugar calls through the interface. The interface declaration
	// itself is left unchanged (like synthetic io.Reader); only call
	// sites are desugared. The itab maps the interface method to each
	// concrete type's pair-returning __-helper (via
	// multiResultHelperFunc's disk fallback); field extraction is by
	// name (f1..fN), which is uniform across all disk pair types.
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, gspec := range gd.Specs {
				ts, ok := gspec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				it, ok := ts.Type.(*ast.InterfaceType)
				if !ok || it.Methods == nil {
					continue
				}
				ifaceName := ts.Name.Name
				for _, m := range it.Methods.List {
					ft, ok := m.Type.(*ast.FuncType)
					if !ok {
						continue
					}
					if numResultsFL(ft.Results) <= 1 {
						continue
					}
					rtypes := resultTypesFL(ft.Results)
					for _, mname := range m.Names {
						// Skip methods served by the static
						// interface-dispatch table.
						if hasStaticIfaceMREntry(mname.Name) {
							continue
						}
						pairType := fmt.Sprintf("__%s_%sPair", ifaceName, mname.Name)
						pairFields := make([]string, len(rtypes))
						for i := range rtypes {
							pairFields[i] = fmt.Sprintf("f%d", i+1)
						}
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
						f.Decls = append(f.Decls, pairDecl)
						specs = append(specs, mrSpec{
							pkgPath:  pkgPath,
							name:     mname.Name,
							recvType: ifaceName,
							// No helper: interface methods have no body.
							// The itab dispatches to each concrete
							// type's __-helper at runtime.
							helper:     "",
							pairType:   pairType,
							pairFields: pairFields,
							stub:       nil,
						})
					}
				}
			}
		}
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
	// GAP-GENERIC-MR: index the package-level specs by name for the
	// hoists below (shared by both).
	specByName := map[string]*mrSpec{}
	for i := range specs {
		s := &specs[i]
		if s.recvType == "" && s.helper != "" {
			specByName[s.name] = s
		}
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
				mrspec := specByName[innerName]
				if mrspec == nil {
					// Method or interface-method MR call: not hoisted
					// here (the export desugar handles call sites).
					newSpecs = append(newSpecs, spec)
					continue
				}
				// Hoist: var __mr_tmp_N = __Inner(args), preserving the
				// package qualifier and explicit type arguments
				// (GAP-GENERIC-MR: F[T](args) -> __F[T](args)).
				tmpName := fmt.Sprintf("__mr_hoist_%d", len(hoisted))
				helperCall := &ast.CallExpr{
					Fun:      mrHelperFunForCall(inner.Fun, innerName),
					Args:     inner.Args,
					Ellipsis: inner.Ellipsis,
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
				// Rewrite outer args to spread the pair fields.
				outer.Args = nil
				for _, pf := range mrspec.pairFields {
					outer.Args = append(outer.Args, &ast.SelectorExpr{
						X:   ast.NewIdent(tmpName),
						Sel: ast.NewIdent(pf),
					})
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
	// BUG-MODPROBE-006: hoist multi-result calls nested in argument
	// position (e.g., return Must(NewRandom())). The export desugar
	// only handles top-level 2-target assignments; a nested MR call
	// would reach export as a call to the (kept-for-resolution) stub.
	// Hoist to a temp and splice the pair fields. General: works for
	// any package-level MR function of any disk package, not just the
	// module that exposed the gap. (Selector/method MR calls nested in
	// args are a separate case, filed if encountered.)
	if len(specs) > 0 {
		if len(specByName) > 0 {
			counter := 0
			for _, f := range files {
				for _, decl := range f.Decls {
					fd, ok := decl.(*ast.FuncDecl)
					if !ok || fd.Body == nil {
						continue
					}
					// Skip generated helpers (already rewritten) and
					// stubs (panic bodies, no calls to hoist).
					if len(fd.Name.Name) > 2 && fd.Name.Name[:2] == "__" {
						continue
					}
					hoistNestedMRInBlock(fd.Body, specByName, &counter)
				}
			}
		}
	}
	return specs
}

// mrHelperFunForCall builds the __-helper call target for an original
// multi-result call fun expression: __-prefixes the function name while
// preserving package qualification (pkg.F -> pkg.__F) and explicit type
// arguments (F[T] -> __F[T], pkg.F[T] -> pkg.__F[T]). GAP-GENERIC-MR:
// dropping the type arguments would leave the helper call uninferrable.
func mrHelperFunForCall(fun ast.Expr, innerName string) ast.Expr {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return &ast.SelectorExpr{X: f.X, Sel: ast.NewIdent("__" + innerName)}
	case *ast.IndexExpr:
		return &ast.IndexExpr{X: mrHelperFunForCall(f.X, innerName), Index: f.Index}
	case *ast.IndexListExpr:
		return &ast.IndexListExpr{X: mrHelperFunForCall(f.X, innerName), Indices: f.Indices}
	case *ast.ParenExpr:
		return &ast.ParenExpr{X: mrHelperFunForCall(f.X, innerName)}
	default:
		return ast.NewIdent("__" + innerName)
	}
}

// hoistNestedMRInBlock hoists multi-result calls in argument position
// throughout a block, inserting temp assignments before the statement.
func hoistNestedMRInBlock(b *ast.BlockStmt, specByName map[string]*mrSpec, counter *int) {
	var out []ast.Stmt
	for _, stmt := range b.List {
		var pre []ast.Stmt
		stmt = hoistNestedMRInStmt(stmt, specByName, counter, &pre)
		out = append(out, pre...)
		out = append(out, stmt)
	}
	b.List = out
}

// hoistNestedMRInStmt rewrites one statement, collecting hoisted
// temporaries in pre.
func hoistNestedMRInStmt(s ast.Stmt, specByName map[string]*mrSpec, counter *int, pre *[]ast.Stmt) ast.Stmt {
	switch stmt := s.(type) {
	case *ast.ReturnStmt:
		for i, e := range stmt.Results {
			var p []ast.Stmt
			stmt.Results[i] = hoistNestedMRExpr(e, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		return stmt
	case *ast.AssignStmt:
		for i, e := range stmt.Rhs {
			// Skip direct MR calls (the export desugar handles
			// 2-target assignments); hoist only nested ones.
			if call, ok := unparen(e).(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && specByName[id.Name] != nil {
					continue
				}
			}
			var p []ast.Stmt
			stmt.Rhs[i] = hoistNestedMRExpr(e, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		return stmt
	case *ast.ExprStmt:
		var p []ast.Stmt
		stmt.X = hoistNestedMRExpr(stmt.X, specByName, counter, &p)
		*pre = append(*pre, p...)
		return stmt
	case *ast.IfStmt:
		if stmt.Init != nil {
			var p []ast.Stmt
			stmt.Init = hoistNestedMRInStmt(stmt.Init, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		var p []ast.Stmt
		stmt.Cond = hoistNestedMRExpr(stmt.Cond, specByName, counter, &p)
		*pre = append(*pre, p...)
		hoistNestedMRInBlock(stmt.Body, specByName, counter)
		switch e := stmt.Else.(type) {
		case *ast.BlockStmt:
			hoistNestedMRInBlock(e, specByName, counter)
		case *ast.IfStmt:
			var p2 []ast.Stmt
			stmt.Else = hoistNestedMRInStmt(e, specByName, counter, &p2)
			*pre = append(*pre, p2...)
		}
		return stmt
	case *ast.ForStmt:
		if stmt.Init != nil {
			var p []ast.Stmt
			stmt.Init = hoistNestedMRInStmt(stmt.Init, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		if stmt.Cond != nil {
			var p []ast.Stmt
			stmt.Cond = hoistNestedMRExpr(stmt.Cond, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		if stmt.Post != nil {
			var p []ast.Stmt
			stmt.Post = hoistNestedMRInStmt(stmt.Post, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		hoistNestedMRInBlock(stmt.Body, specByName, counter)
		return stmt
	case *ast.RangeStmt:
		var p []ast.Stmt
		stmt.X = hoistNestedMRExpr(stmt.X, specByName, counter, &p)
		*pre = append(*pre, p...)
		hoistNestedMRInBlock(stmt.Body, specByName, counter)
		return stmt
	case *ast.SwitchStmt:
		if stmt.Init != nil {
			var p []ast.Stmt
			stmt.Init = hoistNestedMRInStmt(stmt.Init, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		if stmt.Tag != nil {
			var p []ast.Stmt
			stmt.Tag = hoistNestedMRExpr(stmt.Tag, specByName, counter, &p)
			*pre = append(*pre, p...)
		}
		if stmt.Body != nil {
			for _, cc := range stmt.Body.List {
				if ccc, ok := cc.(*ast.CaseClause); ok {
					hoistNestedMRInBlock(&ast.BlockStmt{List: ccc.Body}, specByName, counter)
					// Copy back (BlockStmt was a view).
					ccc.Body = (&ast.BlockStmt{List: ccc.Body}).List
				}
			}
		}
		return stmt
	case *ast.DeferStmt:
		var p []ast.Stmt
		stmt.Call = hoistNestedMRExpr(stmt.Call, specByName, counter, &p).(*ast.CallExpr)
		*pre = append(*pre, p...)
		return stmt
	case *ast.GoStmt:
		var p []ast.Stmt
		stmt.Call = hoistNestedMRExpr(stmt.Call, specByName, counter, &p).(*ast.CallExpr)
		*pre = append(*pre, p...)
		return stmt
	case *ast.BlockStmt:
		hoistNestedMRInBlock(stmt, specByName, counter)
		return stmt
	default:
		return s
	}
}

// hoistNestedMRExpr rewrites an expression, hoisting direct
// multi-result calls in argument position to temps. Returns the
// rewritten expression; hoisted statements accumulate in pre.
func hoistNestedMRExpr(e ast.Expr, specByName map[string]*mrSpec, counter *int, pre *[]ast.Stmt) ast.Expr {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return e
	}
	// Process the function part first (handles chained calls like
	// Must(NewRandom()).String()).
	switch fun := call.Fun.(type) {
	case *ast.CallExpr:
		var p []ast.Stmt
		call.Fun = hoistNestedMRExpr(fun, specByName, counter, &p)
		*pre = append(*pre, p...)
	case *ast.SelectorExpr:
		var p []ast.Stmt
		fun.X = hoistNestedMRExpr(fun.X, specByName, counter, &p)
		*pre = append(*pre, p...)
	}
	// Process args, hoisting direct MR calls.
	var newArgs []ast.Expr
	for _, arg := range call.Args {
		var p []ast.Stmt
		newArg := hoistNestedMRExpr(arg, specByName, counter, &p)
		*pre = append(*pre, p...)
		if inner, ok := unparen(newArg).(*ast.CallExpr); ok {
			if id, ok := inner.Fun.(*ast.Ident); ok {
				if spec := specByName[id.Name]; spec != nil {
					tmpName := fmt.Sprintf("__mr_arg_%d", *counter)
					*counter++
					helperCall := &ast.CallExpr{
						Fun:      ast.NewIdent(spec.helper),
						Args:     inner.Args,
						Ellipsis: inner.Ellipsis,
					}
					*pre = append(*pre, &ast.AssignStmt{
						Lhs: []ast.Expr{ast.NewIdent(tmpName)},
						Tok: token.DEFINE,
						Rhs: []ast.Expr{helperCall},
					})
					for _, f := range spec.pairFields {
						newArgs = append(newArgs, &ast.SelectorExpr{
							X:   ast.NewIdent(tmpName),
							Sel: ast.NewIdent(f),
						})
					}
					continue
				}
			}
		}
		newArgs = append(newArgs, newArg)
	}
	call.Args = newArgs
	return call
}

// numResults counts the result values of a FuncDecl.
func numResults(fd *ast.FuncDecl) int {
	return numResultsFL(fd.Type.Results)
}

// numResultsFL counts the result values of a results field list.
func numResultsFL(results *ast.FieldList) int {
	if results == nil {
		return 0
	}
	n := 0
	for _, fld := range results.List {
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
	return resultTypesFL(fd.Type.Results)
}

// resultTypesFL returns the result type exprs of a results field list in
// order.
func resultTypesFL(results *ast.FieldList) []ast.Expr {
	var out []ast.Expr
	if results == nil {
		return out
	}
	for _, fld := range results.List {
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

// ensureNamedParams assigns fresh names to unnamed fields in fl,
// avoiding collisions with bound. Used when the transform prepends a
// named receiver to possibly-unnamed params.
func ensureNamedParams(fl *ast.FieldList, bound map[string]bool) {
	if fl == nil {
		return
	}
	n := 0
	for _, fld := range fl.List {
		if len(fld.Names) > 0 {
			continue
		}
		for {
			cand := fmt.Sprintf("__arg%d", n)
			n++
			if !bound[cand] {
				fld.Names = []*ast.Ident{ast.NewIdent(cand)}
				bound[cand] = true
				break
			}
		}
	}
}

// renameBlankResults renames blank (`_`) result names in fd to fresh
// identifiers, avoiding collisions with names already bound in fd.
// See transformMRFunc for why.
func renameBlankResults(fd *ast.FuncDecl) {
	if fd.Type.Results == nil {
		return
	}
	bound := locallyBoundNames(fd)
	n := 0
	for _, fld := range fd.Type.Results.List {
		for _, name := range fld.Names {
			if name.Name != "_" {
				continue
			}
			for {
				cand := fmt.Sprintf("__mrblank%d", n)
				n++
				if !bound[cand] {
					name.Name = cand
					bound[cand] = true
					break
				}
			}
		}
	}
}

// tparamNamesOf returns the type parameter names declared by a FuncType,
// or nil for a non-generic function.
func tparamNamesOf(ft *ast.FuncType) []string {
	if ft == nil || ft.TypeParams == nil {
		return nil
	}
	var out []string
	for _, fld := range ft.TypeParams.List {
		for _, n := range fld.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

// genericRef builds a reference to a possibly-generic type or function:
// Name for the non-generic case, Name[T] / Name[T1, T2, ...] for the
// generic case, from the given type parameter names. Needed because
// go/types cannot infer type arguments from composite-literal elements
// or from helper-call arguments that mention only results.
func genericRef(name string, tparamNames []string) ast.Expr {
	base := ast.NewIdent(name)
	switch len(tparamNames) {
	case 0:
		return base
	case 1:
		return &ast.IndexExpr{X: base, Index: ast.NewIdent(tparamNames[0])}
	default:
		idx := &ast.IndexListExpr{X: base}
		for _, n := range tparamNames {
			idx.Indices = append(idx.Indices, ast.NewIdent(n))
		}
		return idx
	}
}

// transformMRFunc transforms one multi-result FuncDecl.
func transformMRFunc(pkgPath string, file *ast.File, fd *ast.FuncDecl, mrNames map[string]bool) (mrSpec, *ast.GenDecl, *ast.FuncDecl, *ast.FuncDecl) {
	name := fd.Name.Name
	// Blank result names (`_ T`) cannot be declared, guarded, or
	// returned by name (`_ = _` and `Pair{_, _}` are illegal). Rename
	// them to fresh identifiers up front; the body cannot reference
	// `_` by name, so the rename is semantics-preserving and every
	// downstream step (named decls, guards, bare-return rewrite) just
	// works.
	renameBlankResults(fd)
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

	// GAP-GENERIC-MR: the pair struct and the helper are generic exactly
	// when the original function is — propagate the type parameter list
	// so fields, params, and bodies referencing T type-check. The stub
	// keeps fd.Type (which owns the list); the pair and helper share the
	// node. go/types gives each declaration its own scope and
	// type-parameter objects, so the sharing is sound.
	//
	// Methods on a generic instantiated type (func (c *Cache[K, V]) Get)
	// reference the TYPE's parameters, which fd.Type.TypeParams does not
	// contain (methods cannot declare their own). Without lifting, the
	// generated helper and pair struct reference K, V undeclared
	// ("undefined: V"). Lift the parameter list, constraints included,
	// from the receiver type's declaration in the same file.
	tparams := fd.Type.TypeParams
	tparamNames := tparamNamesOf(fd.Type)
	if len(tparamNames) == 0 && fd.Recv != nil && len(fd.Recv.List) > 0 {
		if rtp, names := receiverTypeParams(file, fd.Recv.List[0].Type); rtp != nil {
			tparams = rtp
			tparamNames = names
		}
	}

	// Pair struct: type __F_pair[T ...] struct { f1 T1; f2 T2 }.
	pairDecl := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name:       ast.NewIdent(pairType),
				TypeParams: tparams,
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
		// The prepended receiver is named; if the original params are
		// unnamed (e.g. Read([]byte)), the list would mix named and
		// anonymous — illegal. Name the unnamed params freshly (the
		// body cannot reference them by name, so this is safe).
		ensureNamedParams(helperParams, locallyBoundNames(fd))
	} else {
		helperParams = fd.Type.Params
	}
	_ = recvTypeExpr
	// If the original had named results, declare them as locals (the
	// body references them; the pair return no longer defines them).
	// Declare ALL of them unconditionally, each guarded by `_ = name`:
	// a named result that the body never references is legal Go
	// (named results need not be used), but declaring it as an unused
	// local is a type error ("declared and not used"). The old
	// use-analysis was unsound in the other direction too: a read of a
	// name SHADOWED by an inner := (e.g. fmt's parseArgNumber: the
	// `width, ok, newi := parsenum(...)` read of the inner ok) was
	// counted as a read of the named result, declaring a local that
	// nothing reads — a spurious "declared and not used" on real
	// stdlib sources. The `_ =` guard makes every declaration used by
	// construction, so shadowing can no longer break type-checking.
	rewrittenBody := rewriteMRReturns(fd.Body, pairType, pairFields, mrNames, fd, recvType, recvVarName, tparamNames)
	var namedDecls []ast.Stmt
	if fd.Type.Results != nil {
		var guardNames []*ast.Ident
		for _, fld := range fd.Type.Results.List {
			if len(fld.Names) == 0 {
				continue
			}
			namedDecls = append(namedDecls, &ast.DeclStmt{
				Decl: &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{
						&ast.ValueSpec{Names: fld.Names, Type: fld.Type},
					},
				},
			})
			for _, n := range fld.Names {
				guardNames = append(guardNames, ast.NewIdent(n.Name))
			}
		}
		// One `_ = name` per name: `_ = a, b, c` is illegal (1 variable,
		// 3 values), but a blank assignment per name is always legal.
		for _, g := range guardNames {
			namedDecls = append(namedDecls, &ast.AssignStmt{
				Lhs: []ast.Expr{ast.NewIdent("_")},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{g},
			})
		}
	}
	if len(namedDecls) > 0 {
		rewrittenBody.List = append(namedDecls, rewrittenBody.List...)
	}
	helperDecl := &ast.FuncDecl{
		Name: ast.NewIdent(helper),
		Type: &ast.FuncType{
			TypeParams: tparams,
			Params:     helperParams,
			Results: &ast.FieldList{
				// GAP-GENERIC-MR: the pair type is generic exactly when
				// the function is (including lifted receiver type params
				// for methods on generic types); reference it with
				// explicit type arguments.
				List: []*ast.Field{{Type: genericRef(pairType, tparamNames)}},
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

	spec := mrSpec{
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

// receiverTypeParams lifts the type parameter list (with constraints)
// from the declaration of a method's receiver base type, for methods on
// generic instantiated types (e.g. func (c *Cache[K, V]) Get). Returns
// the type's TypeParams node and the argument names from the receiver
// expression. Returns (nil, nil) when the receiver is not a generic
// instantiation with plain identifier arguments, the arity mismatches,
// or the declaration is not found in the file — the caller then leaves
// the method untransformed-safe (no tparams to propagate).
func receiverTypeParams(file *ast.File, recvType ast.Expr) (*ast.FieldList, []string) {
	e := recvType
	for {
		if s, ok := e.(*ast.StarExpr); ok {
			e = s.X
			continue
		}
		break
	}
	var args []ast.Expr
	var base *ast.Ident
	switch t := e.(type) {
	case *ast.IndexExpr:
		base, _ = t.X.(*ast.Ident)
		args = []ast.Expr{t.Index}
	case *ast.IndexListExpr:
		base, _ = t.X.(*ast.Ident)
		args = t.Indices
	default:
		return nil, nil
	}
	if base == nil {
		return nil, nil
	}
	names := make([]string, 0, len(args))
	for _, a := range args {
		id, ok := a.(*ast.Ident)
		if !ok {
			return nil, nil
		}
		names = append(names, id.Name)
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != base.Name {
				continue
			}
			if ts.TypeParams == nil || len(ts.TypeParams.List) == 0 {
				return nil, nil
			}
			if len(tparamNamesOfTypeParams(ts.TypeParams)) != len(names) {
				return nil, nil
			}
			return ts.TypeParams, names
		}
	}
	return nil, nil
}

// tparamNamesOfTypeParams returns the names declared by a type parameter
// list (the shared logic behind tparamNamesOf, which takes a FuncType).
func tparamNamesOfTypeParams(tpl *ast.FieldList) []string {
	if tpl == nil {
		return nil
	}
	var out []string
	for _, fld := range tpl.List {
		for _, n := range fld.Names {
			out = append(out, n.Name)
		}
	}
	return out
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

// usedNamedResults reports which of fd's named result variables the
// pair-returning helper body needs as locals. A bare return in fd's body
// reads every named result (rewriteMRReturns expands it to
// return Pair{v, n, ...}), so all of them are needed. Otherwise only the
// result names genuinely referenced by the body are needed: a reference
// counts only if it resolves to the result variable itself — a shadowing
// re-declaration (v := 5), a declaration-position occurrence (var v int),
// a struct field name, a label, or a selector is not a use. Blank result
// names (_) can never be referenced and are never reported.
func usedNamedResults(fd *ast.FuncDecl) map[string]bool {
	used := map[string]bool{}
	if fd.Type.Results == nil {
		return used
	}
	var names []string
	for _, fld := range fd.Type.Results.List {
		for _, n := range fld.Names {
			if n.Name != "_" {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) == 0 {
		return used
	}
	if hasBareReturn(fd.Body) {
		for _, n := range names {
			used[n] = true
		}
		return used
	}
	w := &mrUseWalker{
		results: map[string]bool{},
		used:    used,
		scope:   &mrScope{names: map[string]bool{}},
	}
	for _, n := range names {
		w.results[n] = true
		w.scope.names[n] = true
	}
	// The receiver and params share the function scope with the results;
	// recording them keeps resolution correct (they can never collide
	// with result names in valid Go).
	if fd.Recv != nil {
		for _, fld := range fd.Recv.List {
			for _, n := range fld.Names {
				w.scope.names[n.Name] = true
			}
		}
	}
	if fd.Type.Params != nil {
		for _, fld := range fd.Type.Params.List {
			for _, n := range fld.Names {
				w.scope.names[n.Name] = true
			}
		}
	}
	w.walkBlock(fd.Body)
	return used
}

// hasBareReturn reports whether body contains a bare `return` statement.
// Function literals are not descended into: a bare return inside one
// belongs to the literal and does not read the enclosing named results.
func hasBareReturn(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(n.Results) == 0 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// mrScope is one lexical scope in the mrUseWalker name-resolution walk.
// The outermost scope (parent == nil) is the function scope holding the
// receiver, params, and named results.
type mrScope struct {
	parent *mrScope
	names  map[string]bool
}

// mrUseWalker determines which named result variables a function body
// genuinely references, with scope-aware name resolution.
type mrUseWalker struct {
	results map[string]bool // named result names of interest
	used    map[string]bool // result names found genuinely referenced
	scope   *mrScope        // innermost scope
}

func (w *mrUseWalker) push() {
	w.scope = &mrScope{parent: w.scope, names: map[string]bool{}}
}

func (w *mrUseWalker) pop() {
	w.scope = w.scope.parent
}

// declare binds name in the current (innermost) scope.
func (w *mrUseWalker) declare(name string) {
	if name == "_" || name == "" {
		return
	}
	w.scope.names[name] = true
}

// useIdent records a use of id if it resolves to a named result: the
// innermost scope binding the name must be the function scope, where the
// results live. A use resolving to any shadowing inner binding is not a
// use of the result.
func (w *mrUseWalker) useIdent(id *ast.Ident) {
	name := id.Name
	if name == "_" || name == "" || !w.results[name] {
		return
	}
	for s := w.scope; s != nil; s = s.parent {
		if s.names[name] {
			if s.parent == nil {
				w.used[name] = true
			}
			return
		}
	}
}

// declareOrUse implements := semantics for one LHS target: an identifier
// already bound in the current scope is a re-assignment (a use);
// otherwise it declares a new variable, shadowing any outer binding (not
// a use of the result). Non-identifier targets (a[i], s.f) are walked for
// uses.
func (w *mrUseWalker) declareOrUse(e ast.Expr) {
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" || id.Name == "" {
			return
		}
		if w.scope.names[id.Name] {
			w.useIdent(id)
		} else {
			w.declare(id.Name)
		}
		return
	}
	w.walkExpr(e)
}

func (w *mrUseWalker) walkBlock(b *ast.BlockStmt) {
	w.push()
	for _, s := range b.List {
		w.walkStmt(s)
	}
	w.pop()
}

func (w *mrUseWalker) walkStmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.DeclStmt:
		w.walkDecl(s.Decl)
	case *ast.EmptyStmt:
		// nothing
	case *ast.LabeledStmt:
		// Labels live in a separate namespace; only the statement matters.
		w.walkStmt(s.Stmt)
	case *ast.ExprStmt:
		w.walkExpr(s.X)
	case *ast.SendStmt:
		w.walkExpr(s.Chan)
		w.walkExpr(s.Value)
	case *ast.IncDecStmt:
		w.walkExpr(s.X)
	case *ast.AssignStmt:
		w.walkAssign(s)
	case *ast.GoStmt:
		w.walkExpr(s.Call)
	case *ast.DeferStmt:
		w.walkExpr(s.Call)
	case *ast.ReturnStmt:
		for _, e := range s.Results {
			w.walkExpr(e)
		}
	case *ast.BranchStmt:
		// break/continue/goto/fallthrough: label only, no variable use.
	case *ast.BlockStmt:
		w.walkBlock(s)
	case *ast.IfStmt:
		w.push()
		if s.Init != nil {
			w.walkStmt(s.Init)
		}
		if s.Cond != nil {
			w.walkExpr(s.Cond)
		}
		w.walkBlock(s.Body)
		if s.Else != nil {
			w.walkStmt(s.Else)
		}
		w.pop()
	case *ast.ForStmt:
		w.push()
		if s.Init != nil {
			w.walkStmt(s.Init)
		}
		if s.Cond != nil {
			w.walkExpr(s.Cond)
		}
		if s.Post != nil {
			w.walkStmt(s.Post)
		}
		w.walkBlock(s.Body)
		w.pop()
	case *ast.RangeStmt:
		w.push()
		w.walkExpr(s.X)
		if s.Tok == token.DEFINE {
			if s.Key != nil {
				w.declareOrUse(s.Key)
			}
			if s.Value != nil {
				w.declareOrUse(s.Value)
			}
		} else {
			if s.Key != nil {
				w.walkExpr(s.Key)
			}
			if s.Value != nil {
				w.walkExpr(s.Value)
			}
		}
		w.walkBlock(s.Body)
		w.pop()
	case *ast.SwitchStmt:
		w.push()
		if s.Init != nil {
			w.walkStmt(s.Init)
		}
		if s.Tag != nil {
			w.walkExpr(s.Tag)
		}
		w.walkCaseClauses(s.Body)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		if s.Init != nil {
			w.walkStmt(s.Init)
		}
		w.walkTypeSwitchGuard(s.Assign)
		w.walkCaseClauses(s.Body)
		w.pop()
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, cc := range s.Body.List {
				ccc, ok := cc.(*ast.CommClause)
				if !ok {
					continue
				}
				w.push()
				if ccc.Comm != nil {
					w.walkStmt(ccc.Comm)
				}
				for _, st := range ccc.Body {
					w.walkStmt(st)
				}
				w.pop()
			}
		}
	}
}

func (w *mrUseWalker) walkCaseClauses(body *ast.BlockStmt) {
	if body == nil {
		return
	}
	for _, cc := range body.List {
		ccc, ok := cc.(*ast.CaseClause)
		if !ok {
			continue
		}
		w.push()
		for _, e := range ccc.List {
			w.walkExpr(e)
		}
		for _, st := range ccc.Body {
			w.walkStmt(st)
		}
		w.pop()
	}
}

// walkTypeSwitchGuard walks `x := y.(type)` or `y.(type)`: the RHS is
// evaluated first, then x (if present) is declared in the switch scope.
func (w *mrUseWalker) walkTypeSwitchGuard(a ast.Stmt) {
	if as, ok := a.(*ast.AssignStmt); ok {
		for _, e := range as.Rhs {
			w.walkExpr(e)
		}
		for _, e := range as.Lhs {
			w.declareOrUse(e)
		}
		return
	}
	w.walkStmt(a)
}

// walkAssign walks an assignment. The RHS is evaluated before any LHS
// declaration takes effect (x, y := y, x reads the outer bindings).
func (w *mrUseWalker) walkAssign(s *ast.AssignStmt) {
	for _, e := range s.Rhs {
		w.walkExpr(e)
	}
	if s.Tok == token.DEFINE {
		for _, e := range s.Lhs {
			w.declareOrUse(e)
		}
		return
	}
	for _, e := range s.Lhs {
		w.walkExpr(e)
	}
}

func (w *mrUseWalker) walkDecl(d ast.Decl) {
	gd, ok := d.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gd.Specs {
		switch spec := spec.(type) {
		case *ast.ValueSpec:
			// var x = x: the RHS reads the outer binding, so walk
			// values before declaring the names.
			for _, e := range spec.Values {
				w.walkExpr(e)
			}
			if spec.Type != nil {
				w.walkType(spec.Type)
			}
			for _, n := range spec.Names {
				w.declare(n.Name)
			}
		case *ast.TypeSpec:
			// The name is in scope inside its own definition
			// (type T *T) and inside its type parameters.
			w.declare(spec.Name.Name)
			if spec.TypeParams != nil {
				w.push()
				for _, fld := range spec.TypeParams.List {
					for _, n := range fld.Names {
						w.declare(n.Name)
					}
				}
				if spec.Type != nil {
					w.walkType(spec.Type)
				}
				w.pop()
			} else if spec.Type != nil {
				w.walkType(spec.Type)
			}
		case *ast.ImportSpec:
			// no variable bindings
		}
	}
}

func (w *mrUseWalker) walkType(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
		w.useIdent(e)
	case *ast.ParenExpr:
		w.walkType(e.X)
	case *ast.StarExpr:
		w.walkType(e.X)
	case *ast.ArrayType:
		if e.Len != nil {
			w.walkExpr(e.Len)
		}
		w.walkType(e.Elt)
	case *ast.MapType:
		w.walkType(e.Key)
		w.walkType(e.Value)
	case *ast.ChanType:
		w.walkType(e.Value)
	case *ast.IndexExpr:
		w.walkType(e.X)
		w.walkType(e.Index)
	case *ast.IndexListExpr:
		w.walkType(e.X)
		for _, ix := range e.Indices {
			w.walkType(ix)
		}
	case *ast.StructType:
		w.walkFieldList(e.Fields, false)
	case *ast.InterfaceType:
		w.walkFieldList(e.Methods, true)
	case *ast.FuncType:
		w.walkFuncType(e)
	}
}

// walkFieldList walks a struct field list or interface method list.
// Field and method names are never variable uses; embedded types are.
func (w *mrUseWalker) walkFieldList(fl *ast.FieldList, iface bool) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			w.walkType(f.Type)
			continue
		}
		if iface {
			if ft, ok := f.Type.(*ast.FuncType); ok {
				w.walkFuncType(ft)
			} else {
				w.walkType(f.Type)
			}
			continue
		}
		w.walkType(f.Type)
	}
}

func (w *mrUseWalker) walkFuncType(ft *ast.FuncType) {
	w.push()
	if ft.Params != nil {
		for _, f := range ft.Params.List {
			for _, n := range f.Names {
				w.declare(n.Name)
			}
		}
		for _, f := range ft.Params.List {
			w.walkType(f.Type)
		}
	}
	if ft.Results != nil {
		for _, f := range ft.Results.List {
			for _, n := range f.Names {
				w.declare(n.Name)
			}
		}
		for _, f := range ft.Results.List {
			w.walkType(f.Type)
		}
	}
	w.pop()
}

func (w *mrUseWalker) walkExpr(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
		w.useIdent(e)
	case *ast.Ellipsis:
		w.walkExpr(e.Elt)
	case *ast.BasicLit:
		// nothing
	case *ast.FuncLit:
		w.walkFuncType(e.Type)
		w.walkBlock(e.Body)
	case *ast.CompositeLit:
		w.walkCompositeLit(e, mrKeyField)
	case *ast.ParenExpr:
		w.walkExpr(e.X)
	case *ast.SelectorExpr:
		// Sel is a field/method name, not a variable.
		w.walkExpr(e.X)
	case *ast.IndexExpr:
		w.walkExpr(e.X)
		w.walkExpr(e.Index)
	case *ast.IndexListExpr:
		w.walkExpr(e.X)
		for _, ix := range e.Indices {
			w.walkExpr(ix)
		}
	case *ast.SliceExpr:
		w.walkExpr(e.X)
		if e.Low != nil {
			w.walkExpr(e.Low)
		}
		if e.High != nil {
			w.walkExpr(e.High)
		}
		if e.Max != nil {
			w.walkExpr(e.Max)
		}
	case *ast.TypeAssertExpr:
		w.walkExpr(e.X)
		if e.Type != nil {
			w.walkType(e.Type)
		}
	case *ast.CallExpr:
		w.walkExpr(e.Fun)
		for _, a := range e.Args {
			w.walkExpr(a)
		}
	case *ast.StarExpr:
		w.walkExpr(e.X)
	case *ast.UnaryExpr:
		w.walkExpr(e.X)
	case *ast.BinaryExpr:
		w.walkExpr(e.X)
		w.walkExpr(e.Y)
	case *ast.KeyValueExpr:
		// A bare KeyValueExpr outside a composite literal cannot occur
		// in valid Go; treat conservatively as uses.
		w.walkExpr(e.Key)
		w.walkExpr(e.Value)
	}
}

// mrKeyKind classifies composite-literal keys for the use walk.
type mrKeyKind int

const (
	mrKeyField mrKeyKind = iota // struct field name: not a variable use
	mrKeyUse                    // map key: a variable use
	mrKeyIndex                  // array/slice index: a constant, never an identifier use
)

// mrKeyKindOfType classifies the keys of a composite literal by its type
// syntax: map keys are variable uses, struct field names are not.
func mrKeyKindOfType(t ast.Expr) mrKeyKind {
	for {
		p, ok := t.(*ast.ParenExpr)
		if !ok {
			break
		}
		t = p.X
	}
	switch t.(type) {
	case *ast.MapType:
		return mrKeyUse
	case *ast.ArrayType:
		return mrKeyIndex
	default:
		return mrKeyField
	}
}

// mrElemKeyKind returns the key kind for composite literals nested in
// element position (array elements, map values) of type t.
func mrElemKeyKind(t ast.Expr) mrKeyKind {
	for {
		p, ok := t.(*ast.ParenExpr)
		if !ok {
			break
		}
		t = p.X
	}
	switch tt := t.(type) {
	case *ast.MapType:
		return mrKeyKindOfType(tt.Value)
	case *ast.ArrayType:
		return mrKeyKindOfType(tt.Elt)
	default:
		return mrKeyField
	}
}

func (w *mrUseWalker) walkCompositeLit(lit *ast.CompositeLit, kk mrKeyKind) {
	childKK := kk
	if lit.Type != nil {
		kk = mrKeyKindOfType(lit.Type)
		childKK = mrElemKeyKind(lit.Type)
		w.walkType(lit.Type)
	}
	for _, e := range lit.Elts {
		w.walkLitElem(e, kk, childKK)
	}
}

func (w *mrUseWalker) walkLitElem(e ast.Expr, kk, childKK mrKeyKind) {
	if kv, ok := e.(*ast.KeyValueExpr); ok {
		if kk == mrKeyUse {
			w.walkExpr(kv.Key)
		}
		// Otherwise the key is a struct field name or an array index:
		// not a variable use.
		if inner, ok := kv.Value.(*ast.CompositeLit); ok {
			w.walkCompositeLit(inner, childKK)
		} else {
			w.walkExpr(kv.Value)
		}
		return
	}
	if inner, ok := e.(*ast.CompositeLit); ok {
		w.walkCompositeLit(inner, childKK)
		return
	}
	w.walkExpr(e)
}

// rewriteMRReturns rewrites return statements in a multi-result body to
// pair construction. Handles:
//   - return a, b        -> return __Pair{a, b}
//   - return             -> return __Pair{n1, n2} (named results)
//   - return F(args)     -> __t := __F(args); return __Pair{__t.f1, __t.f2}
//     (F multi-result; spliced as two statements)
func rewriteMRReturns(body *ast.BlockStmt, pairType string, pairFields []string, mrNames map[string]bool, fd *ast.FuncDecl, recvType, recvVar string, tparamNames []string) *ast.BlockStmt {
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
		// GAP-GENERIC-MR: explicit type arguments for the pair literal
		// and recursive helper calls (go/types cannot infer them).
		// tparamNames carries the lifted receiver type parameters for
		// methods on generic types.
		tparamNames: tparamNames,
		funcName:    fd.Name.Name,
		// Names bound as locals (receiver, params, var/:=): a
		// return-position call to such a name is a func value, not a
		// reference to the package-level multi-result function.
		boundNames: locallyBoundNames(fd),
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
	// GAP-GENERIC-MR: the enclosing function's type parameter names
	// (nil for non-generic functions) and its name (to recognize
	// recursion in return-position call forwarding).
	tparamNames []string
	funcName    string
	// boundNames are the names bound as locals in the enclosing
	// function (receiver, params, var/:= declarations). A
	// return-position call to such a name is a func value call, never a
	// reference to the package-level multi-result function, so it must
	// not be rewritten to a __-helper call.
	boundNames map[string]bool
}

// locallyBoundNames returns the names bound as locals anywhere in fd
// (receiver, params, var and := declarations, range variables). It is a
// conservative over-approximation: a name bound in any nested block
// counts. Used by the return rewrite to distinguish a call to the
// package-level multi-result function from a call to a shadowing func
// value (e.g. a decodeFunc parameter named decode).
func locallyBoundNames(fd *ast.FuncDecl) map[string]bool {
	bound := map[string]bool{}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				bound[n.Name] = true
			}
		}
	}
	if fd.Recv != nil {
		addFields(fd.Recv)
	}
	addFields(fd.Type.Params)
	if fd.Body == nil {
		return bound
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, e := range n.Lhs {
					if id, ok := unparen(e).(*ast.Ident); ok {
						bound[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				bound[id.Name] = true
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e == nil {
						continue
					}
					if id, ok := unparen(e).(*ast.Ident); ok {
						bound[id.Name] = true
					}
				}
			}
		}
		return true
	})
	return bound
}

// transformTargetCollides reports whether transforming fd would generate
// a helper or pair-type name that already exists as a top-level
// declaration (a hand-written __-helper). Mirrors transformMRFunc's
// naming: __F / __FPair for funcs, __Recv_F / __Recv_FPair for methods.
// isFailFastStub reports whether fd's body is a synthetic fail-fast stub:
// a single panic statement with "not in M{2,4,5,7,...} scope" in the message.
// Such bodies must be preserved by the MR transform; rewriting them would
// replace the fail-fast with a working helper.
func isFailFastStub(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	stmt, ok := fd.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "panic" {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.Contains(lit.Value, "not in M") && strings.Contains(lit.Value, "scope")
}
// pairTypeExpr builds the pair composite-literal type: __Pair for the
// non-generic case, __Pair[T, ...] for the generic case.
func (ctx *mrRewriteCtx) pairTypeExpr() ast.Expr {
	return genericRef(ctx.pairType, ctx.tparamNames)
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
	case *ast.LabeledStmt:
		// Descend into the labeled statement (e.g. parseLoop: for ...).
		// A labeled return expanding to two statements drops the label
		// (labels are semantically irrelevant on return).
		rewritten := ctx.rewriteStmt(s.Stmt)
		if len(rewritten) == 1 {
			s.Stmt = rewritten[0]
			return []ast.Stmt{s}
		}
		return rewritten
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
	pairFields, namedResults := ctx.pairFields, ctx.namedResults
	mkPair := func(args []ast.Expr) *ast.ReturnStmt {
		return &ast.ReturnStmt{
			Return: rs.Return,
			Results: []ast.Expr{
				&ast.CompositeLit{
					Type: ctx.pairTypeExpr(),
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
		// (e.g. return vr.Value()).
		if call, ok := rs.Results[0].(*ast.CallExpr); ok {
			var fname string
			if id, ok := call.Fun.(*ast.Ident); ok {
				fname = id.Name
			} else if id := callTargetIdent(call.Fun); id != nil {
				fname = id.Name
			}
			// Direct __F form only when fname names a known
			// multi-result function AND no local shadows it: a
			// return-position call to a func value (a parameter or
			// variable, e.g. a decodeFunc named decode) must keep the
			// original call — there is no __-helper for it. The
			// export desugar rewrites named MR functions in the
			// two-value form below.
			if fname != "" && ctx.mrNames[fname] && !ctx.boundNames[fname] {
				// Package-level func: __t := __F(args);
				// return Pair{__t.f1, __t.f2}.
				// GAP-GENERIC-MR: preserve the callee's explicit type
				// arguments on the helper call. When the call is recursive
				// (return F(args) inside F's own body) and carries no
				// explicit type arguments, forward the enclosing function's
				// type parameters — inference from the helper call's
				// arguments cannot recover them.
				var calleeTargs []ast.Expr
				switch fun := call.Fun.(type) {
				case *ast.IndexExpr:
					calleeTargs = []ast.Expr{fun.Index}
				case *ast.IndexListExpr:
					calleeTargs = fun.Indices
				}
				var calleeTparamNames []string
				for _, ta := range calleeTargs {
					if id, ok := ta.(*ast.Ident); ok {
						calleeTparamNames = append(calleeTparamNames, id.Name)
					} else {
						// Non-identifier type argument (e.g. []byte):
						// cannot name it; fall back to positional refs.
						calleeTparamNames = nil
						break
					}
				}
				if calleeTparamNames == nil && len(calleeTargs) == 0 && fname == ctx.funcName {
					calleeTparamNames = ctx.tparamNames
				}
				helperFun := ast.Expr(ast.NewIdent("__" + fname))
				if len(calleeTparamNames) > 0 {
					helperFun = genericRef("__"+fname, calleeTparamNames)
				} else if len(calleeTargs) > 0 {
					// Non-identifier type arguments: splice them as-is.
					switch len(calleeTargs) {
					case 1:
						helperFun = &ast.IndexExpr{X: ast.NewIdent("__" + fname), Index: calleeTargs[0]}
					default:
						helperFun = &ast.IndexListExpr{X: ast.NewIdent("__" + fname), Indices: calleeTargs}
					}
				}
				tmp := ast.NewIdent("__mr_tmp")
				helperCall := &ast.CallExpr{
					Fun:      helperFun,
					Args:     call.Args,
					Ellipsis: call.Ellipsis,
				}
				assign := &ast.AssignStmt{
					Lhs: []ast.Expr{tmp},
					Tok: token.DEFINE,
					Rhs: []ast.Expr{helperCall},
				}
				args := make([]ast.Expr, len(pairFields))
				for i, f := range pairFields {
					// Use fresh Ident nodes for the uses (not the def site).
					args[i] = &ast.SelectorExpr{X: ast.NewIdent("__mr_tmp"), Sel: ast.NewIdent(f)}
				}
				return []ast.Stmt{assign, mkPair(args)}
			}
			// General fallback: __t1, __t2 := <original call>;
			// return Pair{__t1, __t2}. Correct for func values, methods
			// (selector calls), and any other multi-valued call; the
			// export desugar rewrites calls to named multi-result
			// functions here.
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
		return []ast.Stmt{rs}
	default:
		// return a, b, ... -> return Pair{a, b, ...}
		return []ast.Stmt{mkPair(rs.Results)}
	}
}

// removeMRStubs removes stub FuncDecls from files after type-checking.
func removeMRStubs(files []*ast.File, specs []mrSpec) {
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

// registerMRSpecs adds specs to the dynamic desugar table.
func registerMRSpecs(specs []mrSpec) {
	mrSpecsMu.Lock()
	mrSpecs = append(mrSpecs, specs...)
	mrSpecsMu.Unlock()
}

// lookupMRSpec finds a dynamic desugar entry.
func lookupMRSpec(pkgPath, name, recvType string) *mrSpec {
	mrSpecsMu.Lock()
	defer mrSpecsMu.Unlock()
	for i := range mrSpecs {
		s := &mrSpecs[i]
		if s.pkgPath == pkgPath && s.name == name && s.recvType == recvType {
			return s
		}
	}
	return nil
}

// mrOwnerPackage resolves the *syntheticPackage owning a multi-result
// transformed package, whether synthetic (built by buildSyntheticPackage)
// or disk-built (built by buildDiskPackage). The transform runs on both,
// so every lookup keyed on a transformed package must resolve both.
func mrOwnerPackage(pkgPath string) (*syntheticPackage, error) {
	if _, ok := syntheticSources[pkgPath]; ok {
		return getSyntheticPackage(pkgPath), nil
	}
	return getDiskPackage(pkgPath)
}

// mrHelperFi maps a transformed multi-result stub *types.Func to its
// __-helper's funcDeclInfo, for generic-instantiation enumeration
// (scanRoots, GAP-GENERIC-MR). The stub is skipped at synthetic import
// (never in funcByObj); the helper carries the real body. Returns
// ok=false when fn is not a transformed multi-result stub with a helper.
func (e *exporter) mrHelperFi(fn *types.Func) (*funcDeclInfo, bool) {
	if fn == nil || fn.Pkg() == nil {
		return nil, false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil, false
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
	spec := lookupMRSpec(fn.Pkg().Path(), fn.Name(), recvName)
	if spec == nil || spec.helper == "" {
		return nil, false
	}
	// The helper is registered in funcByObj at import. The (path, name)
	// index is the robust lookup (the *types.Func identity can differ
	// across type-checks — cf. scanRoots' synthetic fallback); fall back
	// to the package scope object for funcByObj.
	if e.synthFuncIDs != nil {
		if fi, ok := e.synthFuncIDs[fn.Pkg().Path()+"."+spec.helper]; ok {
			return fi, true
		}
	}
	if sp, err := mrOwnerPackage(fn.Pkg().Path()); err == nil && sp != nil {
		if obj, ok := sp.pkg.Scope().Lookup(spec.helper).(*types.Func); ok {
			if fi, ok := e.funcByObj[obj]; ok {
				return fi, true
			}
		}
	}
	return nil, false
}

// mrPairType returns the *types.Named pair type from the transformed package.
func mrPairType(sp *syntheticPackage, spec *mrSpec) *types.Named {
	obj := sp.pkg.Scope().Lookup(spec.pairType)
	if obj == nil {
		return nil
	}
	tn, _ := obj.Type().(*types.Named)
	return tn
}

// classifyTransformedMRCall desugars multi-result calls to functions
// transformed by transformMultiResult (synthetic or disk-built). Mirrors
// classifyM9MultiResultCall but consults the dynamic table and resolves
// the pair type from the owning package.
func classifyTransformedMRCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
	spec := lookupMRSpec(pkgPath, fn.Name(), recvName)
	if spec == nil {
		return nil
	}
	// BUG-MODPROBE-005: interface specs (helper == "") are only
	// reachable via classifyTransformedIfaceMRCall (the interface
	// dispatch path). A concrete-position desugar must never match
	// them — there is no helper to call.
	if spec.helper == "" {
		return nil
	}
	if sig.Results().Len() != len(spec.pairFields) {
		return nil
	}
	sp, err := mrOwnerPackage(pkgPath)
	if err != nil {
		return nil
	}
	nt := mrPairType(sp, spec)
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

// classifyTransformedIfaceMRCall recognizes WS G (P3-M9) multi-result
// calls on interface values whose method is a disk-package interface
// method registered by transformMultiResult (BUG-MODPROBE-005). It
// mirrors classifyIfaceMultiResultCall but consults the dynamic
// mrSpecs table instead of m8IfaceMultiResultMethods. The itab maps
// the interface method to each concrete type's pair-returning __-helper
// (wired via multiResultHelperFunc's disk fallback), so the node yields
// the pair and field extraction proceeds like the concrete Appendix M
// desugar. Returns nil when the method is not a registered disk
// interface method.
func (e *exporter) classifyTransformedIfaceMRCall(selection *types.Selection) *mrPlan {
	if selection.Kind() != types.MethodVal {
		return nil
	}
	f, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() < 2 {
		return nil
	}
	if f.Pkg() == nil {
		return nil
	}
	pkgPath := f.Pkg().Path()
	// For an interface-declared method, the signature's receiver is the
	// declaring interface's named type (verified against go/types).
	recv := sig.Recv()
	if recv == nil {
		return nil
	}
	nt, ok := recv.Type().(*types.Named)
	if !ok || nt.Obj() == nil {
		return nil
	}
	spec := lookupMRSpec(pkgPath, f.Name(), nt.Obj().Name())
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
	pt := mrPairType(sp, spec)
	if pt == nil {
		return nil
	}
	return &mrPlan{
		kind: mrIfaceMethod, helperPkg: pkgPath,
		tmpType: pt, pairFields: spec.pairFields,
	}
}
