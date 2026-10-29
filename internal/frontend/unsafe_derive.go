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
	// unsafe.Pointer provenance.
	for i, t := range terms {
		if i == baseIdx {
			continue
		}
		if !e.isIntegerExpr(t.expr) || e.exprContainsUnsafePointer(t.expr) {
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
