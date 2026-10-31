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
	"strings"
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
	// requiresIntrinsic names the backend intrinsic a bodyless
	// function needs (qualified original symbol, e.g.
	// "reflect.chanrecv"): set only when the __-helper is a panic
	// body for a bodyless declaration with no //go:linkname binding.
	// "" means the helper has a real Go body or is linkname-bound.
	// Export-time desugar (classifyDiskMultiResultCall) refuses to
	// plan a call to a spec with an unprovided required intrinsic,
	// so the call fails at export instead of emitting a call to a
	// panic helper that would explode at guest runtime.
	requiresIntrinsic string
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
			if !ok {
				continue
			}
			// Body-less declarations (e.g. //go:noescape assembly stubs
			// like reflect's chanrecv) are included: their call sites
			// still need desugaring, and the generated __-helper gets a
			// panic body (the real implementation is a backend intrinsic).
			if numResults(fd) > 1 {
				mrNames[fd.Name.Name] = true
			}
		}
	}
	if len(mrNames) == 0 {
		return nil
	}
	// Collision guard: collect every existing top-level name BEFORE the
	// transform runs, and mark each generated helper/pair name as it is
	// created. A FuncDecl whose generated helper (__F) or pair type
	// (__FPair) name already exists is NEVER silently skipped (a skip
	// drops the function from the desugar table: no funcdecl, no spec,
	// and the method vanishes from the type's method table). Instead
	// it is renamed deterministically by disambiguatedMRPrefix
	// (__F_mr2, __F_mr3, ...). Functions genuinely served by
	// hand-written __-helpers are still skipped — by the static-table
	// check below, which names the reason, not by collision.
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
			if !ok || numResults(fd) <= 1 {
				newDecls = append(newDecls, decl)
				continue
			}
			// Collision: rename deterministically, never skip (see
			// the collision-guard comment above).
			prefix := disambiguatedMRPrefix(fd, existing)
			// Skip functions served by the static Appendix-M tables
			// (hand-written __-helpers with ad-hoc names) — with two
			// tiers (fix-redmaster-r40). M5-owned entries
			// (context.WithCancel/Deadline, runtime.Caller,
			// time.ParseDuration) always skip: dedicated exporter
			// machinery desugars their call sites. The remaining
			// tables name helpers in the package's synthetic twin,
			// whose receiver representation differs from the disk
			// package's own values; a disk package's real-bodied
			// method (e.g. reflect's (*rtype).FieldByName) must
			// still be transformed, or it is emitted nowhere: no
			// funcdecl, no disk spec, and the method is missing
			// from the type's method table, so any interface
			// dispatch on the type fails when the itab is built.
			// Bodyless declarations keep the skip; fail-fast bodies
			// are skipped just below.
			recvType := ""
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recvType = recvBaseName(fd.Recv.List[0].Type)
			}
			if hasM5StaticEntry(pkgPath, fd.Name.Name, recvType) ||
				(fd.Body == nil && hasStaticMREntry(pkgPath, fd.Name.Name, recvType)) {
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
			spec, pairDecl, helperDecl, stubDecl := transformMRFuncWithPrefix(pkgPath, files, fd, mrNames, prefix)
			newDecls = append(newDecls, pairDecl, helperDecl, stubDecl)
			specs = append(specs, spec)
			existing[spec.helper] = true
			existing[spec.pairType] = true
		}
		f.Decls = newDecls
	}
	// BUG-MODPROBE-005: interface method specs with multi-result.
	// Generate a pair type per (interface, method) and register a spec
	// so interface dispatch (classifyDiskIfaceMultiResultCall) can
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
						specs = append(specs, diskMRSpec{
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
	specByName := map[string]*diskMRSpec{}
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
					Fun:  mrHelperFunForCall(inner.Fun, innerName),
					Args: inner.Args,
					// Preserve a variadic spread (f(xs...)): dropping
					// Ellipsis makes the exporter pack the slice as a
					// single element (fix-redmaster-r40).
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

// mrLinknameSymbol returns the backend symbol for a body-less
// declaration carrying a //go:linkname or //go:linknamestd directive,
// or ("", false) when there is none. A malformed directive also yields
// ("", false): this transform cannot report errors, and the panic stub
// is the safe fallback. linknamestd (gc's std-only linkname, no
// explicit symbol) resolves under the package's default object symbol,
// as for a single-field //go:linkname.
func mrLinknameSymbol(fd *ast.FuncDecl, pkgPath string) (string, bool) {
	if fd.Doc == nil {
		return "", false
	}
	sym, err := linknameDirective(fd, pkgPath)
	if err != nil {
		return "", false
	}
	if sym != "" {
		return sym, true
	}
	for _, c := range fd.Doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if !strings.HasPrefix(text, "go:linknamestd ") {
			continue
		}
		fields := strings.Fields(text[len("go:linknamestd "):])
		if len(fields) != 1 || fields[0] != fd.Name.Name {
			return "", false
		}
		return pkgPath + "." + fields[0], true
	}
	return "", false
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
func hoistNestedMRInBlock(b *ast.BlockStmt, specByName map[string]*diskMRSpec, counter *int) {
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
func hoistNestedMRInStmt(s ast.Stmt, specByName map[string]*diskMRSpec, counter *int, pre *[]ast.Stmt) ast.Stmt {
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
func hoistNestedMRExpr(e ast.Expr, specByName map[string]*diskMRSpec, counter *int, pre *[]ast.Stmt) ast.Expr {
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
						Fun:  ast.NewIdent(spec.helper),
						Args: inner.Args,
						// Preserve a variadic spread (f(xs...)); see
						// the hoist site above (fix-redmaster-r40).
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

// tparamNamesOf returns the type parameter names declared by a FuncType,
// or nil for a non-generic function.
func tparamNamesOf(ft *ast.FuncType) []string {
	if ft == nil || ft.TypeParams == nil {
		return nil
	}
	return tparamNamesOfFL(ft.TypeParams)
}

// tparamNamesOfFL returns the type parameter names declared by a
// type parameter list (used for receiver-resolved lists, which do not
// belong to a FuncType).
func tparamNamesOfFL(fl *ast.FieldList) []string {
	if fl == nil {
		return nil
	}
	var out []string
	for _, fld := range fl.List {
		for _, n := range fld.Names {
			out = append(out, n.Name)
		}
	}
	return out
}

// receiverTypeParams finds the type parameter list declared by the
// named type for a method receiver's base type, searching all of the
// package's files. Go methods cannot declare type parameters; a
// method on a generic receiver (e.g. func (m *Box[K, V]) Load(...))
// uses the receiver type's parameters. Returns nil when the base
// type is not generic or its declaration is not found (e.g. the
// receiver is a non-generic or imported type).
func receiverTypeParams(files []*ast.File, base string) *ast.FieldList {
	if base == "" {
		return nil
	}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != base {
					continue
				}
				return ts.TypeParams
			}
		}
	}
	return nil
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
// transformMRFunc is the default-prefix form of transformMRFuncWithPrefix:
// methods on different receivers are disambiguated by the receiver name
// (Recv_Name), plain functions use their own name.
func transformMRFunc(pkgPath string, files []*ast.File, fd *ast.FuncDecl, mrNames map[string]bool) (diskMRSpec, *ast.GenDecl, *ast.FuncDecl, *ast.FuncDecl) {
	prefix := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		if rt := recvBaseName(fd.Recv.List[0].Type); rt != "" {
			prefix = rt + "_" + fd.Name.Name
		}
	}
	return transformMRFuncWithPrefix(pkgPath, files, fd, mrNames, prefix)
}

func transformMRFuncWithPrefix(pkgPath string, files []*ast.File, fd *ast.FuncDecl, mrNames map[string]bool, prefix string) (diskMRSpec, *ast.GenDecl, *ast.FuncDecl, *ast.FuncDecl) {
	name := fd.Name.Name
	recvType := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recvType = recvBaseName(fd.Recv.List[0].Type)
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
	// Methods cannot declare their own type parameters in Go; a method
	// on a generic receiver (e.g. func (m *Box[K, V]) Load(...))
	// uses the receiver type's parameters. fd.Type.TypeParams is nil
	// for methods, so resolve them from the receiver's named type
	// declaration across the package's files.
	tparams := fd.Type.TypeParams
	if tparams == nil && recvType != "" {
		tparams = receiverTypeParams(files, recvType)
	}
	tparamNames := tparamNamesOfFL(tparams)

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
		// FIX: Go requires a parameter list to be consistently named or
		// anonymous. The prepended receiver always has a name, but the
		// original params may be anonymous (e.g. func (d *T) Read([]byte)).
		// Give synthetic names to anonymous params so the helper type-checks.
		helperParams = nameAnonymousParams(helperParams)
	} else {
		helperParams = fd.Type.Params
	}
	_ = recvTypeExpr
	// If the original had named results AND a bare return, declare
	// them as locals (the body references them; the pair return no
	// longer defines them). Declare ONLY the names the body actually
	// uses: a named result that the body never references is legal Go
	// (named results need not be used), but declaring it as an unused
	// local is a type error ("declared and not used").
	// The helper's signature drops the named results (it returns the pair
	// type instead), so any named result the body still references must be
	// declared as a local. Declare ONLY the results the body genuinely
	// needs: usedNamedResults is scope-aware — a bare return reads every
	// named result; otherwise a reference counts only if it resolves to
	// the result variable itself (shadowing := re-declarations,
	// declaration positions, struct field names, labels, selectors, and
	// bare returns inside nested func literals are not uses). Declaring
	// an unreferenced name trips go/types' "declared and not used" on
	// the rewritten AST (e.g. protowire.ConsumeVarint's n). Analyzed on
	// the original body, before rewriteMRReturns reshapes it.
	// Compute before rewriteMRReturns (which introduces its own __t :=
	// bindings).
	//
	// Body-less declarations (e.g. //go:noescape assembly stubs like
	// reflect's chanrecv): there is no body to rewrite. By default the
	// __-helper gets a fail-fast panic body — the real implementation
	// would be a backend intrinsic — and the spec records that
	// required intrinsic (see requiresIntrinsic): export-time desugar
	// refuses calls to it unless a provider is visible, so the panic
	// helper is only ever an unreachable marker, never a callable
	// implementation. EXCEPTION (fix-redmaster-r42): a
	// body-less declaration carrying a //go:linkname or //go:linknamestd
	// directive is served by a backend intrinsic, so its __-helper must
	// be linkname-bound, not a panic stub. That is possible for the
	// helper even though it is not possible for the original
	// multi-result declaration: the helper returns a single pair value,
	// which LinknameRootNode dispatch can carry. The helper is therefore
	// emitted body-less with a synthesized directive, and the exporter
	// binds it through the existing bodyless→linkname path. Body-less
	// declarations WITHOUT a directive, and generic ones (whose linkname
	// binding is out of M4 scope), keep the panic body exactly as before.
	var rewrittenBody *ast.BlockStmt
	var helperDoc *ast.CommentGroup
	if fd.Body == nil {
		if len(tparamNames) == 0 {
			if sym, ok := mrLinknameSymbol(fd, pkgPath); ok {
				helperDoc = &ast.CommentGroup{List: []*ast.Comment{
					{Text: "//go:linkname " + helper + " " + sym},
				}}
			}
		}
		if helperDoc == nil {
			rewrittenBody = &ast.BlockStmt{
				List: []ast.Stmt{
					&ast.ExprStmt{
						X: &ast.CallExpr{
							Fun: ast.NewIdent("panic"),
							Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING,
								Value: `"__` + helper + ` not implemented: no Go body (backend intrinsic)"`}},
						},
					},
				},
			}
		}
	} else {
		usedNames := usedNamedResults(fd)
		rewrittenBody = rewriteMRReturns(fd.Body, pairType, pairFields, mrNames, fd, recvType, recvVarName, tparamNames)
		var namedDecls []ast.Stmt
		if len(usedNames) > 0 && fd.Type.Results != nil {
			for _, fld := range fd.Type.Results.List {
				if len(fld.Names) == 0 {
					continue
				}
				var keep []*ast.Ident
				for _, n := range fld.Names {
					if usedNames[n.Name] {
						// Fresh ident: reusing n would share its *ast.Object
						// with the stub's named result (the stub reuses
						// fd.Type), and go/types then misattributes uses —
						// notably references from inside closures — as
						// "declared and not used".
						keep = append(keep, ast.NewIdent(n.Name))
					}
				}
				if len(keep) > 0 {
					namedDecls = append(namedDecls, &ast.DeclStmt{
						Decl: &ast.GenDecl{
							Tok: token.VAR,
							Specs: []ast.Spec{
								&ast.ValueSpec{Names: keep, Type: fld.Type},
							},
						},
					})
					// Mark each declared name as used: a named result that
					// is only assigned (e.g. in a defer closure) but never
					// read would trip go/types' "declared and not used" as
					// a local variable. The blank assignment is a no-op
					// that satisfies the use check.
					for _, n := range keep {
						namedDecls = append(namedDecls, &ast.AssignStmt{
							Lhs: []ast.Expr{ast.NewIdent("_")},
							Tok: token.ASSIGN,
							Rhs: []ast.Expr{ast.NewIdent(n.Name)},
						})
					}
				}
			}
		}
		if len(namedDecls) > 0 {
			rewrittenBody.List = append(namedDecls, rewrittenBody.List...)
		}
	} // end else (fd.Body != nil); body-less case used panic body above,
	// or a body-less linkname-bound helper when helperDoc is set
	helperDecl := &ast.FuncDecl{
		Doc:  helperDoc,
		Name: ast.NewIdent(helper),
		Type: &ast.FuncType{
			TypeParams: tparams,
			Params:     helperParams,
			Results: &ast.FieldList{
				// GAP-GENERIC-MR: the pair type is generic exactly when
				// the function is; reference it with explicit type
				// arguments.
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

	// A panic helper for a bodyless declaration (no //go:linkname
	// binding, so helperDoc is nil) records the backend intrinsic it
	// requires: the qualified original symbol. Real-bodied and
	// linkname-bound helpers require nothing.
	requiresIntrinsic := ""
	if fd.Body == nil && helperDoc == nil {
		if recvType != "" {
			requiresIntrinsic = pkgPath + "." + recvType + "." + name
		} else {
			requiresIntrinsic = pkgPath + "." + name
		}
	}
	spec := diskMRSpec{
		pkgPath:           pkgPath,
		name:              name,
		recvType:          recvType,
		helper:            helper,
		pairType:          pairType,
		pairFields:        pairFields,
		stub:              stubDecl,
		requiresIntrinsic: requiresIntrinsic,
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

// nameAnonymousParams ensures a FieldList is consistently named, as Go
// requires. If the list mixes named and anonymous parameters (e.g. after
// prepending a named receiver to anonymous params), the anonymous ones get
// synthetic __argN names. The helper is synthetic, so these names are never
// referenced by user code — the original body cannot name anonymous params.
func nameAnonymousParams(fl *ast.FieldList) *ast.FieldList {
	if fl == nil {
		return fl
	}
	var named, anonymous bool
	for _, field := range fl.List {
		if len(field.Names) > 0 {
			named = true
		} else {
			anonymous = true
		}
	}
	if !named || !anonymous {
		return fl // already consistent
	}
	// Mixed: name the anonymous fields. Copy the list to avoid mutating
	// the original AST nodes' sharing.
	out := &ast.FieldList{List: make([]*ast.Field, len(fl.List))}
	arg := 0
	for i, field := range fl.List {
		nf := *field // shallow copy
		if len(nf.Names) == 0 {
			nf.Names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("__arg%d", arg))}
			arg++
		}
		out.List[i] = &nf
	}
	return out
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
	// Named result names (and their types) for bare return.
	var namedResults []string
	var namedResultTypes []ast.Expr
	if fd.Type.Results != nil {
		for _, fld := range fd.Type.Results.List {
			for _, n := range fld.Names {
				namedResults = append(namedResults, n.Name)
				namedResultTypes = append(namedResultTypes, fld.Type)
			}
		}
	}
	ctx := &mrRewriteCtx{
		pairType:         pairType,
		pairFields:       pairFields,
		namedResults:     namedResults,
		namedResultTypes: namedResultTypes,
		mrNames:          mrNames,
		recvType:         recvType,
		recvVar:          recvVar,
		// GAP-GENERIC-MR: explicit type arguments for the pair literal
		// and recursive helper calls (go/types cannot infer them).
		// The names are resolved by the caller: for methods on generic
		// receivers they come from the receiver's type parameters.
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
	// namedResultTypes parallels namedResults: the declared type of each
	// named result (blank results need their type for the zero value).
	namedResultTypes []ast.Expr
	mrNames          map[string]bool
	recvType         string
	recvVar          string
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

// mrPrefixTaken reports whether either generated name for prefix
// (__prefix, __prefixPair) already exists as a top-level declaration.
func mrPrefixTaken(prefix string, existing map[string]bool) bool {
	return existing["__"+prefix] || existing["__"+prefix+"Pair"]
}

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
	return mrPrefixTaken(prefix, existing)
}

// disambiguatedMRPrefix returns the helper/pair prefix for fd: the usual
// Recv_Name (or Name) prefix when its __-names are free, otherwise the
// first suffixed variant (base_mr2, base_mr3, ...) whose __-names are
// both free. Deterministic — it depends only on the pre-transform
// top-level names plus names generated earlier in the same pass, which
// the caller must mark in existing after each transform — so the same
// package always yields the same helper names, and a colliding function
// is renamed, never silently dropped.
func disambiguatedMRPrefix(fd *ast.FuncDecl, existing map[string]bool) string {
	base := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		if rt := recvBaseName(fd.Recv.List[0].Type); rt != "" {
			base = rt + "_" + fd.Name.Name
		}
	}
	if !mrPrefixTaken(base, existing) {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s_mr%d", base, i)
		if !mrPrefixTaken(cand, existing) {
			return cand
		}
	}
}

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
			if n == "_" {
				// Blank-named result: `_` is not a value. gc semantics:
				// a blank result is always its zero value at a bare
				// return (assignments to it are discards). *new(T) is
				// the zero value for any type T.
				args[i] = &ast.StarExpr{
					X: &ast.CallExpr{
						Fun:  ast.NewIdent("new"),
						Args: []ast.Expr{ctx.namedResultTypes[i]},
					},
				}
				continue
			}
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
					Fun:  helperFun,
					Args: call.Args,
					// Preserve a variadic spread (return F(os.Stdout,
					// a...)): dropping Ellipsis makes the exporter
					// pack the slice as a single element
					// (fix-redmaster-r40).
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

// diskMRHasUnprovidedIntrinsic reports whether any disk spec in pkgPath
// for a method named name carries a required intrinsic no provider
// supplies (a bodyless declaration whose __-helper is a panic body).
// Interface dispatch is dynamic: at an interface call site the exporter
// cannot prove the call avoids such an implementation, so the
// interface-dispatch gates treat ANY such spec as refusing the call —
// fail-closed, at the cost of refusing programs that only ever box
// real-bodied implementors of the same interface (documented scope
// limit of the gate; see diskMRIntrinsicProvided). It scans directly
// under the table lock (lookupDiskMR takes the lock itself and must
// not be called while it is held).
func (e *exporter) diskMRHasUnprovidedIntrinsic(pkgPath, name string) bool {
	diskMRMu.Lock()
	defer diskMRMu.Unlock()
	for i := range diskMRSpecs {
		s := &diskMRSpecs[i]
		if s.pkgPath == pkgPath && s.name == name && s.requiresIntrinsic != "" && !e.diskMRIntrinsicProvided(s) {
			return true
		}
	}
	return false
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

// diskMRHelperFi maps a disk multi-result stub *types.Func to its
// __-helper's funcDeclInfo, for generic-instantiation enumeration
// (scanRoots, GAP-GENERIC-MR). The stub is skipped at synthetic import
// (never in funcByObj); the helper carries the real body. Returns
// ok=false when fn is not a disk multi-result stub with a helper.
func (e *exporter) diskMRHelperFi(fn *types.Func) (*funcDeclInfo, bool) {
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
	spec := lookupDiskMR(fn.Pkg().Path(), fn.Name(), recvName)
	if spec == nil || spec.helper == "" {
		return nil, false
	}
	// Same intrinsic gate as classifyDiskMultiResultCall: never
	// enumerate a panic helper for instantiation.
	if spec.requiresIntrinsic != "" && !e.diskMRIntrinsicProvided(spec) {
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
	if sp, err := getDiskPackage(fn.Pkg().Path()); err == nil && sp != nil {
		if obj, ok := sp.pkg.Scope().Lookup(spec.helper).(*types.Func); ok {
			if fi, ok := e.funcByObj[obj]; ok {
				return fi, true
			}
		}
	}
	return nil, false
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

// diskMRIntrinsicProvided reports whether the backend intrinsic required
// by a bodyless disk function is provided by a frontend-visible
// function: a synthetic-package function with the required qualified
// name (the same synthFuncIDs lookup exportLinknameDecl uses for
// linkname providers). Specs with no requirement are always provided.
// A bodyless declaration with its own //go:linkname directive never
// gets requiresIntrinsic (its helper is linkname-bound), so this only
// gates panic helpers.
//
// Scope limits of the intrinsic gate (veto fix-up P2d — stated, not
// redesigned; no concrete broken case is known for either):
// (i) The gate covers user call sites only. Calls inside the disk
// package itself are rewritten to the __-helper by the transform
// (mrRewriteCtx.rewriteBlock) before export, so they never pass
// through the gate; a bodyless declaration called only from within
// its own package still reaches its panic helper if the backend
// provides no intrinsic for it.
// (ii) This predicate consults only synthFuncIDs: a backend wire-node
// intrinsic with no synthetic function of the required qualified
// name would be false-refused (the call fails at export even though
// the backend could serve it). That is the documented assumption of
// the gate, not a redesign: no such provider is known today, and
// widening the predicate to backend wire nodes is future work if
// one appears.
// (iii) Matching is on the QUALIFIED symbol only (requiresIntrinsic
// itself). A short-key fallback (pkgPath.name) was removed: the
// method's own stub entry is registered under that key, so the
// fallback made every imported disk method its own provider and
// the gate never fired end to end (probed 2026-10-31).
func (e *exporter) diskMRIntrinsicProvided(spec *diskMRSpec) bool {
	if spec == nil || spec.requiresIntrinsic == "" {
		return true
	}
	if e.synthFuncIDs != nil {
		// Qualified name ONLY. A short-key fallback (pkgPath.name)
		// used to live here on the theory that synthFuncIDs keys
		// omit the receiver — but the method's OWN stub/method
		// entry is registered under exactly that short key
		// (importSyntheticPackage registers every processed decl
		// under sp.path+"."+obj.Name()), so the fallback matched
		// the method itself and the gate never fired end to end:
		// probed 2026-10-31, the iface + method-value veto tests
		// exported with the panic helper wired, while a bare
		// exporter (empty synthFuncIDs) correctly refused. For
		// function specs the fallback was identical to this key
		// anyway (requiresIntrinsic is pkgPath.name); for method
		// specs it is unsound (it also matches an unrelated
		// package-level func of the same name). A genuine provider
		// must register under the qualified symbol.
		if _, ok := e.synthFuncIDs[spec.requiresIntrinsic]; ok {
			return true
		}
	}
	return false
}

// classifyDiskMultiResultCall desugars multi-result calls to disk-package
// functions transformed by transformDiskMultiResult. Mirrors
// classifyM9MultiResultCall but consults the dynamic table and resolves
// the pair type from the disk-built package. It returns nil for a spec
// whose required intrinsic is unprovided: with no plan, the call site
// fails at export instead of emitting a call to the spec's panic
// helper (which would explode at guest runtime).
func (e *exporter) classifyDiskMultiResultCall(pkgPath string, fn *types.Func, targs []types.Type) *mrPlan {
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
	// BUG-MODPROBE-005: interface specs (helper == "") are only
	// reachable via classifyDiskIfaceMultiResultCall (the interface
	// dispatch path). A concrete-position desugar must never match
	// them — there is no helper to call.
	if spec.helper == "" {
		return nil
	}
	if sig.Results().Len() != len(spec.pairFields) {
		return nil
	}
	// Export-time intrinsic gate (review-bytes-mr): a panic helper is
	// not an implementation. Without a provider, refuse the plan.
	if spec.requiresIntrinsic != "" && !e.diskMRIntrinsicProvided(spec) {
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

// classifyDiskIfaceMultiResultCall recognizes WS G (P3-M9) multi-result
// calls on interface values whose method is a disk-package interface
// method registered by transformDiskMultiResult (BUG-MODPROBE-005). It
// mirrors classifyIfaceMultiResultCall but consults the dynamic
// diskMRSpecs table instead of m8IfaceMultiResultMethods. The itab maps
// the interface method to each concrete type's pair-returning __-helper
// (wired via multiResultHelperFunc's disk fallback), so the node yields
// the pair and field extraction proceeds like the concrete Appendix M
// desugar. Returns nil when the method is not a registered disk
// interface method.
func (e *exporter) classifyDiskIfaceMultiResultCall(selection *types.Selection) *mrPlan {
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
	spec := lookupDiskMR(pkgPath, f.Name(), nt.Obj().Name())
	if spec == nil {
		return nil
	}
	if sig.Results().Len() != len(spec.pairFields) {
		return nil
	}
	// Veto fix-up F2 (review-bytes-mr): the intrinsic gate the
	// direct-call classifier applies (classifyDiskMultiResultCall)
	// was missing here, so a bodyless disk method called through an
	// interface still dispatched to its panic helper at guest
	// runtime. Interface specs themselves never carry
	// requiresIntrinsic (they have no body and no helper), so the
	// gate must look at the concrete specs: dispatch is dynamic and
	// may land on a bodyless implementation, so any unprovided
	// intrinsic for this method in this package refuses the plan
	// (fail-closed; see diskMRHasUnprovidedIntrinsic). Returning nil
	// alone is not sufficient — classifyUserIfaceMultiResultCall
	// would otherwise synthesize a plan around it — so that classifier
	// applies the same refusal.
	if spec.requiresIntrinsic != "" && !e.diskMRIntrinsicProvided(spec) {
		return nil
	}
	if e.diskMRHasUnprovidedIntrinsic(pkgPath, f.Name()) {
		return nil
	}
	sp, err := getDiskPackage(pkgPath)
	if err != nil {
		return nil
	}
	pt := diskMRPairType(sp, spec)
	if pt == nil {
		return nil
	}
	return &mrPlan{
		kind: mrIfaceMethod, helperPkg: pkgPath,
		tmpType: pt, pairFields: spec.pairFields,
	}
}
