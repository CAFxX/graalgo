// Copyright 2026 The graalgo authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
//
// fix-redmaster-r39: provenance-proven lowering of
// uintptr -> unsafe.Pointer conversions (the "unsafederive" wire node).
//
// D5 traps uintptr -> unsafe.Pointer because a bare integer carries no
// provenance: the value model cannot tell an address computed from a
// live pointer apart from any other integer, and materializing a handle
// from the summed integer would silently alias whatever referent the
// sum happens to collide with (handles are dense sequential integers).
//
// Some conversions, however, carry their provenance syntactically. The
// canonical shape is sync.Pool's indexLocal:
//
//	unsafe.Pointer(uintptr(l) + uintptr(i)*unsafe.Sizeof(poolLocal{}))
//
// where the operand is an additive tree with exactly one term that is a
// uintptr conversion of an unsafe.Pointer expression and every other
// term a pure integer expression. For that shape the exporter proves
// the provenance from the go/types-annotated AST and emits
//
//	unsafederive { base: <the unsafe.Pointer expression>,
//	               offset: <the remaining terms, combined>,
//	               type: <unsafe.Pointer type id> }
//
// so the backend derives the result from the LIVE base handle by the
// byte offset (UnsafeTable.derive) — the summed integer itself is never
// materialized into a handle. The proof is deliberately conservative;
// any shape it cannot prove falls back to the existing "unsafefromuint"
// trap node, byte-identical to the pre-r39 behavior:
//
//   - zero or multiple pointer-derived terms;
//   - a pointer-derived term with a negative sign (ptr - off inverts
//     the derivation direction; off - ptr is not a pointer at all);
//   - a non-additive operator over a pointer-derived term (it then
//     appears inside a non-base term and the descendant scan rejects);
//   - any unsafe.Pointer-typed subexpression inside an offset term
//     (a hidden second provenance source);
//   - any non-integer offset term.
//
// Evaluation order: the derive node evaluates the base first, then the
// offset terms in source order; gc evaluates the original additive tree
// left-to-right. Only side-effecting subexpressions inside unsafe
// arithmetic could observe the difference.

package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// safeSizeof returns sizes.Sizeof(t), or ok=false when t has no fixed
// size. go/types' gcSizes asserts (panics) on type parameters — a
// generic *T target has variable size by definition — so the panic is
// contained here and reported as "no size". Callers omit the wire field;
// a byte-derived pointer to such a target then traps loudly at the
// backend instead of crashing the exporter.
func safeSizeof(sizes types.Sizes, t types.Type) (sz int64, ok bool) {
	defer func() {
		if recover() != nil {
			sz, ok = 0, false
		}
	}()
	return sizes.Sizeof(t), true
}

// isUnsafePointerType reports whether t is unsafe.Pointer (alias-unwrapped).
func isUnsafePointerType(t types.Type) bool {
	return t != nil && types.Unalias(t) == types.Typ[types.UnsafePointer]
}

// uintptrConversionOfPointer reports whether e is a conversion
// uintptr(P) with P unsafe.Pointer-typed, returning P.
func (e *exporter) uintptrConversionOfPointer(x ast.Expr) (ast.Expr, bool) {
	ce, ok := unparen(x).(*ast.CallExpr)
	if !ok || len(ce.Args) != 1 {
		return nil, false
	}
	if !isUintptr(e.info.TypeOf(ce)) {
		return nil, false
	}
	arg := ce.Args[0]
	if !isUnsafePointerType(e.info.TypeOf(arg)) {
		return nil, false
	}
	return arg, true
}

// exprContainsUnsafePointer reports whether any subexpression of x is
// unsafe.Pointer-typed (per go/types). Used to reject offset terms with
// a hidden provenance source.
func (e *exporter) exprContainsUnsafePointer(x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found || n == nil {
			return !found
		}
		if ex, ok := n.(ast.Expr); ok && isUnsafePointerType(e.info.TypeOf(ex)) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isIntegerExpr reports whether x's type is an integer type (named or
// basic, alias-unwrapped).
func (e *exporter) isIntegerExpr(x ast.Expr) bool {
	t := e.info.TypeOf(x)
	if t == nil {
		return false
	}
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// signedTerm is one flattened additive term with its sign (+1 or -1).
type signedTerm struct {
	sign int
	expr ast.Expr
}

// flattenAdditive flattens an additive (+/-) expression tree into
// signed terms, unwrapping parentheses. Non-additive subtrees become
// single terms.
func flattenAdditive(x ast.Expr, sign int, terms *[]signedTerm) {
	x = unparen(x)
	if be, ok := x.(*ast.BinaryExpr); ok && (be.Op == token.ADD || be.Op == token.SUB) {
		flattenAdditive(be.X, sign, terms)
		ySign := sign
		if be.Op == token.SUB {
			ySign = -sign
		}
		flattenAdditive(be.Y, ySign, terms)
		return
	}
	*terms = append(*terms, signedTerm{sign: sign, expr: x})
}

// valuePointerCall reports whether x is a call to reflect.Value.Pointer
// (no arguments) and returns its receiver expression. Pointer() is the
// second pointer-derived uintptr shape in the language: its contract
// is "the Value's pointer as uintptr", so unsafe.Pointer(v.Pointer())
// has exactly the same provenance as unsafe.Pointer(uintptr(P)) — the
// Value's live pointer — and lowers to the same derive node. The
// receiver's type must be reflect.Value (after unaliasing and one
// pointer deref, matching the method call's own auto-deref); the
// method-set lookup discriminates the method from any func-valued
// field of the same name.
func (e *exporter) valuePointerCall(x ast.Expr) (ast.Expr, bool) {
	ce, ok := unparen(x).(*ast.CallExpr)
	if !ok || len(ce.Args) != 0 {
		return nil, false
	}
	sel, ok := unparen(ce.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Pointer" {
		return nil, false
	}
	if !isUintptr(e.info.TypeOf(ce)) {
		return nil, false
	}
	recvT := e.info.TypeOf(sel.X)
	if recvT == nil {
		return nil, false
	}
	if p, ok := types.Unalias(recvT).(*types.Pointer); ok {
		recvT = p.Elem()
	}
	named, ok := types.Unalias(recvT).(*types.Named)
	if !ok {
		return nil, false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "reflect" || obj.Name() != "Value" {
		return nil, false
	}
	if types.NewMethodSet(named).Lookup(nil, "Pointer") == nil {
		return nil, false
	}
	return sel.X, true
}

// valueUnsafePointerCall builds a call node for recv.UnsafePointer(),
// the pointer twin of recv.Pointer(). In gc's reflect/value.go the two
// methods return the same pointer for every kind (Pointer is
// uintptr(UnsafePointer) modulo the escapes no-op); only the
// ValueError method string differs on panic paths, so a program that
// recovers from unsafe.Pointer(v.Pointer()) on an invalid-kind Value
// observes "reflect.Value.UnsafePointer" where gc says
// "reflect.Value.Pointer". Success paths — including every use inside
// reflect itself (FuncOf, StructOf, MakeFunc) — are bit-exact. The
// call is built through the exporter's own methodref machinery, so
// receiver adaptation (value/pointer, promotion) matches a source
// call exactly.
func (e *exporter) valueUnsafePointerCall(c *ast.CallExpr, recv ast.Expr) (*Node, error) {
	recvNode, err := e.exportExpr(recv)
	if err != nil {
		return nil, err
	}
	recvT := e.info.TypeOf(recv)
	if recvT == nil {
		return nil, fmt.Errorf("gofrontend: no type info for Value.Pointer receiver")
	}
	if p, ok := types.Unalias(recvT).(*types.Pointer); ok {
		recvT = p.Elem()
	}
	named, ok := types.Unalias(recvT).(*types.Named)
	if !ok {
		return nil, fmt.Errorf("gofrontend: Value.Pointer receiver is not a named type")
	}
	selection := types.NewMethodSet(named).Lookup(nil, "UnsafePointer")
	if selection == nil || selection.Kind() != types.MethodVal {
		return nil, fmt.Errorf("gofrontend: reflect.Value has no UnsafePointer method")
	}
	sel := &ast.SelectorExpr{X: recv, Sel: ast.NewIdent("UnsafePointer")}
	fun, err := e.methodRefNode(c, sel, selection, recvNode)
	if err != nil {
		return nil, err
	}
	if fun.Kind == "ifacemethod" {
		return nil, fmt.Errorf("gofrontend: unexpected ifacemethod fallback for reflect.Value.UnsafePointer")
	}
	ptrID, err := e.typeID(types.Typ[types.UnsafePointer])
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": []*Node{}, "type": ptrID,
	}), nil
}

// P1-8: stored-uintptr provenance (single-assignment locals).
//
// A uintptr that round-trips through a local variable keeps its
// provenance when the exporter can prove, from the enclosing function
// body alone, that the variable still holds exactly the defined value
// at the conversion site. The proof is deliberately narrow (V1):
//
//   - the definition is a top-level statement of the function body,
//     either `u := uintptr(P)` or `var u uintptr = uintptr(P)`, where
//     P is a bare unsafe.Pointer variable (identifier);
//   - the stored variable has exactly one assignment in the whole
//     function (the definition itself for :=, none besides the
//     definition for a var spec), its address is never taken, and no
//     function literal in the body mentions it (a closure could
//     rewrite it after the definition);
//   - the base variable P is never assigned after the definition, its
//     address is never taken, and no function literal mentions it —
//     so re-evaluating P at the conversion site (the derive node's
//     base child) reads the same pointer the definition stored;
//   - P is declared within the function itself (a local, parameter,
//     or result): a package variable, a dot-imported variable, or an
//     outer function's local seen from a literal could be reassigned
//     by code this body cannot see;
//   - if P is a named result, no value-return after the
//     definition may reassign it (a bare return leaves results
//     untouched);
//   - the conversion site is textually after the definition.
//
// Why textual order plus a top-level definition is dominance: the
// definition is a direct statement of the body block, so every
// statement textually after it executes after it — except via a goto
// jumping over it, which go/types rejects ("goto jumps over
// declaration") before export ever runs. Chained copies (`v := u`,
// where u is itself a proven stored variable) compose: v's base is
// u's base, and v is dropped if u's proof is dropped.
//
// What V1 deliberately does NOT track (each keeps the trap, with the
// reason recorded in the P1-8 design note): multi-assignment
// definitions; non-identifier bases (field reads, calls, address-of
// expressions — re-evaluating them is not proven stable without a
// free-identifier stability analysis); definitions in nested blocks;
// stored values flowing through parameters, struct fields, globals,
// return values, or function values (interprocedural — unprovable from
// one function body); named uintptr types; and offset terms the prover
// KNOWS are pointer-derived (a second base shape, a Pointer() call, or
// a proven stored variable used as an offset — see
// exprHasPointerDerived). Unknown integer terms (plain variables,
// function returns) remain trusted as byte counts, exactly as in r39:
// the prover cannot know they are pointer-derived, and gc treats
// their values as byte offsets too.
type storedUintptrDef struct {
	// The unsafe.Pointer variable the stored value equals; exported
	// as the derive node's base child at the conversion site.
	base *ast.Ident
	// Position of the defining statement (for the textual-order rule).
	defPos token.Pos
}

// storedCandidate is one raw V1 definition found by the top-level scan,
// before whole-body validation.
type storedCandidate struct {
	obj     types.Object
	base    *ast.Ident   // direct base ident, or nil for a chain copy
	chain   types.Object // source stored var, or nil
	defPos  token.Pos
	varSpec bool // definition is a var spec (no AssignStmt counts it)
}

// storedBaseIdent returns the unsafe.Pointer variable P for a
// definition RHS of the restricted V1 form uintptr(P), or nil.
func (e *exporter) storedBaseIdent(rhs ast.Expr) *ast.Ident {
	p, ok := e.uintptrConversionOfPointer(unparen(rhs))
	if !ok {
		return nil
	}
	id, ok := unparen(p).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil
	}
	return id
}

// computeStoredUintptr proves the stored-uintptr definitions of one
// function body (see the storedUintptrDef contract above). decl is the
// enclosing *ast.FuncDecl or *ast.FuncLit, used for the base-locality
// rule below. It returns nil when the body defines none that survive
// validation. Callers install the result for the duration of the body
// export only.
func (e *exporter) computeStoredUintptr(decl ast.Node, body *ast.BlockStmt, ft *ast.FuncType) map[types.Object]*storedUintptrDef {
	if body == nil {
		return nil
	}
	var cands []*storedCandidate
	seen := map[types.Object]bool{}
	blocked := map[types.Object]bool{}
	add := func(obj types.Object, base *ast.Ident, chain types.Object, pos token.Pos, varSpec bool) {
		if obj == nil || blocked[obj] {
			return
		}
		if seen[obj] {
			// A second definition for the same object (invalid Go,
			// but fail closed): neither definition is tracked.
			blocked[obj] = true
			return
		}
		seen[obj] = true
		cands = append(cands, &storedCandidate{obj: obj, base: base, chain: chain, defPos: pos, varSpec: varSpec})
	}
	for _, stmt := range body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
				continue
			}
			id, ok := unparen(s.Lhs[0]).(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			obj, ok := e.info.ObjectOf(id).(*types.Var)
			if !ok || !isUintptr(obj.Type()) {
				continue
			}
			if base := e.storedBaseIdent(s.Rhs[0]); base != nil {
				add(obj, base, nil, s.Pos(), false)
			} else if src, ok := unparen(s.Rhs[0]).(*ast.Ident); ok && src.Name != "_" {
				if srcObj, ok := e.info.ObjectOf(src).(*types.Var); ok && isUintptr(srcObj.Type()) {
					add(obj, nil, srcObj, s.Pos(), false)
				}
			}
		case *ast.DeclStmt:
			gd, ok := s.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
				continue
			}
			vs, ok := gd.Specs[0].(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			id := vs.Names[0]
			if id.Name == "_" {
				continue
			}
			obj, ok := e.info.ObjectOf(id).(*types.Var)
			if !ok || !isUintptr(obj.Type()) {
				continue
			}
			if base := e.storedBaseIdent(vs.Values[0]); base != nil {
				add(obj, base, nil, s.Pos(), true)
			} else if src, ok := unparen(vs.Values[0]).(*ast.Ident); ok && src.Name != "_" {
				if srcObj, ok := e.info.ObjectOf(src).(*types.Var); ok && isUintptr(srcObj.Type()) {
					add(obj, nil, srcObj, s.Pos(), true)
				}
			}
		}
	}
	if len(cands) == 0 {
		return nil
	}
	// Whole-body validation facts. Assignment positions are recorded
	// per object so base stability can be checked against each
	// candidate's own definition position.
	assignCount := map[types.Object]int{}
	assignPos := map[types.Object][]token.Pos{}
	var returnPos []token.Pos
	var funcLits []*ast.FuncLit
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range s.Lhs {
				if id, ok := unparen(lhs).(*ast.Ident); ok {
					if obj := e.info.ObjectOf(id); obj != nil {
						assignCount[obj]++
						assignPos[obj] = append(assignPos[obj], s.Pos())
					}
				}
			}
		case *ast.IncDecStmt:
			if id, ok := unparen(s.X).(*ast.Ident); ok {
				if obj := e.info.ObjectOf(id); obj != nil {
					assignCount[obj]++
					assignPos[obj] = append(assignPos[obj], s.Pos())
				}
			}
		case *ast.RangeStmt:
			for _, kv := range []ast.Expr{s.Key, s.Value} {
				if kv == nil {
					continue
				}
				if id, ok := unparen(kv).(*ast.Ident); ok {
					if obj := e.info.ObjectOf(id); obj != nil {
						assignCount[obj]++
						assignPos[obj] = append(assignPos[obj], s.Pos())
					}
				}
			}
		case *ast.ReturnStmt:
			// Only a value-return reassigns named results; a bare
			// return leaves them untouched.
			if len(s.Results) > 0 {
				returnPos = append(returnPos, s.Pos())
			}
		case *ast.FuncLit:
			funcLits = append(funcLits, s)
		}
		return true
	})
	addrTaken := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
			ast.Inspect(u.X, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					if obj := e.info.ObjectOf(id); obj != nil {
						addrTaken[obj] = true
					}
				}
				return true
			})
		}
		return true
	})
	mentioned := map[types.Object]bool{}
	for _, lit := range funcLits {
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			if id, ok := m.(*ast.Ident); ok {
				if obj := e.info.ObjectOf(id); obj != nil {
					mentioned[obj] = true
				}
			}
			return true
		})
	}
	resultObjs := map[types.Object]bool{}
	if ft != nil && ft.Results != nil {
		for _, f := range ft.Results.List {
			for _, name := range f.Names {
				if obj := e.info.ObjectOf(name); obj != nil {
					resultObjs[obj] = true
				}
			}
		}
	}
	// Candidates are in body order, so a chain copy's source (defined
	// earlier) is resolved before its dependents; a dropped source
	// drops its dependents.
	out := map[types.Object]*storedUintptrDef{}
	dropped := map[types.Object]bool{}
	for _, c := range cands {
		if blocked[c.obj] || dropped[c.obj] {
			continue
		}
		var base *ast.Ident
		var baseObj types.Object
		if c.base != nil {
			base = c.base
			baseObj = e.info.ObjectOf(base)
		} else {
			src, ok := out[c.chain]
			if !ok || !(src.defPos < c.defPos) {
				dropped[c.obj] = true
				continue
			}
			base = src.base
			baseObj = e.info.ObjectOf(base)
		}
		if base == nil || baseObj == nil {
			dropped[c.obj] = true
			continue
		}
		// Base locality: the base variable must be declared within
		// this function (a local, parameter, or result). A package
		// variable — or a dot-imported one, or an outer function's
		// local seen from a literal — can be reassigned by code this
		// body cannot see, so re-evaluating it is not proven stable.
		if baseObj.Pos() < decl.Pos() || baseObj.Pos() > decl.End() {
			dropped[c.obj] = true
			continue
		}
		want := 0
		if !c.varSpec {
			want = 1 // the defining := itself
		}
		if assignCount[c.obj] != want {
			dropped[c.obj] = true
			continue
		}
		if addrTaken[c.obj] || addrTaken[baseObj] || mentioned[c.obj] || mentioned[baseObj] {
			dropped[c.obj] = true
			continue
		}
		stable := true
		for _, p := range assignPos[baseObj] {
			if p > c.defPos {
				stable = false
			}
		}
		if resultObjs[baseObj] {
			for _, p := range returnPos {
				if p > c.defPos {
					stable = false
				}
			}
		}
		if !stable {
			dropped[c.obj] = true
			continue
		}
		out[c.obj] = &storedUintptrDef{base: base, defPos: c.defPos}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// exprHasPointerDerived reports whether x contains a subexpression the
// prover KNOWS is pointer-derived: a uintptr(P) conversion, a
// reflect.Value.Pointer() call, or a proven stored-uintptr variable.
// Such a term used as a byte offset would add a handle number (a dense
// sequential integer, not a byte count) to a base pointer — a silent
// aliasing hole if it happened to land in bounds — so offset terms
// containing one are rejected. (Unknown integers — plain variables,
// function returns — stay trusted as byte counts, as in r39.)
func (e *exporter) exprHasPointerDerived(x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		switch t := n.(type) {
		case *ast.CallExpr:
			if _, ok := e.uintptrConversionOfPointer(t); ok {
				found = true
				return false
			}
			if _, ok := e.valuePointerCall(t); ok {
				found = true
				return false
			}
		case *ast.Ident:
			if _, ok := e.storedUintptr[e.info.ObjectOf(t)]; ok {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// exportUintptrToPointer attempts the provenance-proven lowering of a
// uintptr -> unsafe.Pointer conversion call c (whose target type id is
// to). handled=false means the shape is not provable and the caller must
// emit the regular unsafefromuint trap node.
func (e *exporter) exportUintptrToPointer(c *ast.CallExpr, to any) (*Node, bool, error) {
	var terms []signedTerm
	flattenAdditive(c.Args[0], +1, &terms)

	baseIdx := -1
	var baseNode *Node
	for i, t := range terms {
		var b *Node
		isBase := false
		if p, ok := e.uintptrConversionOfPointer(t.expr); ok {
			n, err := e.exportExpr(p)
			if err != nil {
				return nil, false, err
			}
			b, isBase = n, true
		} else if recv, ok := e.valuePointerCall(t.expr); ok {
			n, err := e.valueUnsafePointerCall(c, recv)
			if err != nil {
				return nil, false, err
			}
			b, isBase = n, true
		} else if id, ok := unparen(t.expr).(*ast.Ident); ok {
			// P1-8: a proven stored-uintptr variable as the base term.
			// The derive node's base child re-evaluates the stored
			// variable's base pointer (proven stable by
			// computeStoredUintptr); the conversion must be textually
			// after the definition, or the proof does not apply.
			if def, ok := e.storedUintptr[e.info.ObjectOf(id)]; ok && def.defPos < c.Pos() {
				n, err := e.exportExpr(def.base)
				if err != nil {
					return nil, false, err
				}
				b, isBase = n, true
			}
		}
		if isBase {
			if baseIdx >= 0 || t.sign < 0 {
				return nil, false, nil
			}
			baseIdx = i
			baseNode = b
		}
	}
	if baseIdx < 0 {
		return nil, false, nil
	}
	// Every non-base term must be a pure integer term with no hidden
	// unsafe.Pointer provenance — and no KNOWN pointer-derived value
	// (a proven stored variable, a Pointer() call, or a pointer
	// conversion nested inside the term): such a value is a handle
	// number, not a byte count, and adding it to the base would be a
	// silent aliasing hole if it landed in bounds.
	for i, t := range terms {
		if i == baseIdx {
			continue
		}
		if !e.isIntegerExpr(t.expr) || e.exprContainsUnsafePointer(t.expr) || e.exprHasPointerDerived(t.expr) {
			return nil, false, nil
		}
	}

	offsetNode, err := e.exportDeriveOffset(c, terms, baseIdx)
	if err != nil {
		return nil, false, err
	}
	return e.node("unsafederive", c, map[string]any{
		"base": baseNode, "offset": offsetNode, "type": to,
	}), true, nil
}

// exportDeriveOffset exports the non-base terms and combines them into
// one uintptr offset expression, in source order, honoring signs. With
// no offset terms the offset is the constant 0 (an exact round trip).
func (e *exporter) exportDeriveOffset(c *ast.CallExpr, terms []signedTerm, baseIdx int) (*Node, error) {
	uintptrID, err := e.typeID(types.Typ[types.Uintptr])
	if err != nil {
		return nil, err
	}
	var acc *Node
	for i, t := range terms {
		if i == baseIdx {
			continue
		}
		n, err := e.exportExpr(t.expr)
		if err != nil {
			return nil, err
		}
		if acc == nil {
			acc = n
			if t.sign < 0 {
				acc = e.node("unary", t.expr, map[string]any{
					"op": "-", "x": acc, "type": uintptrID,
				})
			}
			continue
		}
		op := "+"
		if t.sign < 0 {
			op = "-"
		}
		acc = e.node("binary", c, map[string]any{
			"op": op, "x": acc, "y": n, "type": uintptrID,
		})
	}
	if acc == nil {
		return e.foldedConst(c.Args[0], constant.MakeInt64(0))
	}
	return acc, nil
}
