// Copyright 2026 The graalgo authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
//
// P3-M8 synthetic "unsafe" package (replaces the P3-M4 empty stub in
// export.go; see staged patch 01). Unlike the other synthetic packages
// (which ship real Go sources), "unsafe" is built programmatically because
// its members are compiler intrinsics: go/types recognizes them as
// *types.Builtin and applies gc's own checking (operand validation,
// constant folding), so the frontend inherits gc-exact behavior for free.
//
// Members (all nine, all real):
//
//	type Pointer = unsafe.Pointer            (alias to types.UnsafePointer)
//	Sizeof/Alignof/Offsetof                   (builtins: go/types folds them
//	                                           to constants — no pre-pass,
//	                                           no size tables; a non-constant
//	                                           operand, e.g. over a type
//	                                           parameter, is a scope error)
//	Slice/SliceData/String/StringData         (builtins: result types flow
//	                                           from go/types; lowered to the
//	                                           unsafe* wire nodes over the
//	                                           per-run handle table)
//	Add                                       (builtin: always the D5 trap)
//
// Design notes:
//
//   - go/types dispatches *types.Builtin by ID, so `unsafe.Slice[byte](p,n)`
//     is rejected ("cannot index unsafe.Slice (built-in)") and a constant
//     negative len is a compile error — exactly like gc. The exporter never
//     reimplements this checking.
//   - The shared builtin objects live in types.Unsafe's scope (populated by
//     go/types' own universe init); the synthetic package inserts the same
//     objects, exactly as universe builtins are shared across packages.
//   - `Pointer` conversions lower via exportUnsafeConversion: *T<->Pointer
//     intern/resolve over the handle table; uintptr->Pointer traps (D5);
//     Pointer->uintptr keeps the existing D5 ConvertNode trap.
//   - unsafe.Add always traps: handles admit no byte arithmetic (D5/GG-0015).
//   - DIV-US-1: unsafe.String copies the bytes into a fresh GoString.
//     GoString is immutable (hashCode/equals read the internal array), so
//     sharing the caller's array would corrupt map keys. Post-call writes
//     through the original pointer are invisible to the string — an
//     intentional, documented divergence from gc.
//   - Fail-fast (loud, with re-test triggers): Add; Pointer<->uintptr;
//     over-long Slice len and carrier-mismatched referents panic with
//     gc-exact messages at runtime.
package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// unsafeBuiltinNames are the eight unsafe builtins shared from types.Unsafe.
var unsafeBuiltinNames = []string{
	"Add", "Alignof", "Offsetof", "Sizeof",
	"Slice", "SliceData", "String", "StringData",
}

// syntheticUnsafePackage builds the P3-M8 synthetic "unsafe" package: the
// eight real compiler builtins (shared objects from types.Unsafe's scope —
// go/types populates it in its universe init, so this must run after the
// go/types package loads, which is always the case inside the exporter)
// plus `type Pointer = unsafe.Pointer`.
func syntheticUnsafePackage() *types.Package {
	pkg := types.NewPackage("unsafe", "unsafe")
	for _, name := range unsafeBuiltinNames {
		obj := types.Unsafe.Scope().Lookup(name)
		if obj == nil {
			panic(fmt.Sprintf("gofrontend: synthetic unsafe package: types.Unsafe lacks builtin %s", name))
		}
		pkg.Scope().Insert(obj)
	}
	ptrName := types.NewTypeName(token.NoPos, pkg, "Pointer", nil)
	types.NewAlias(ptrName, types.Typ[types.UnsafePointer])
	pkg.Scope().Insert(ptrName)
	pkg.MarkComplete()
	return pkg
}

// isUnsafeIntrinsic reports whether obj is one of the eight unsafe builtins.
// Universe builtins (append, len, ...) have a nil Pkg; the unsafe ones carry
// types.Unsafe (path "unsafe").
func isUnsafeIntrinsic(obj types.Object) bool {
	b, ok := obj.(*types.Builtin)
	return ok && b.Pkg() != nil && b.Pkg().Path() == "unsafe"
}

// exportUnsafeIntrinsic lowers a call to one of the eight unsafe builtins.
// Sizeof/Alignof/Offsetof are constant-folded by go/types itself: the call's
// recorded value is the constant. A non-constant operand (go/types reports
// value mode for types with variable size, e.g. over a type parameter) is a
// named scope error rather than a miscompile.
func (e *exporter) exportUnsafeIntrinsic(c *ast.CallExpr, b *types.Builtin) (*Node, error) {
	name := b.Name()
	switch name {
	case "Sizeof", "Alignof", "Offsetof":
		tv, ok := e.info.Types[c]
		if !ok || tv.Value == nil {
			return nil, scopeErrorf("unsafe.%s: operand is not a constant expression in frontend/v1 scope (M8)", name)
		}
		return e.foldedConst(c, tv.Value)
	case "Slice", "SliceData", "String", "StringData", "Add":
		if len(c.Args) != 2 && (name == "Slice" || name == "String" || name == "Add") {
			return nil, scopeErrorf("unsafe.%s: wrong arity (M8)", name)
		}
		if len(c.Args) != 1 && (name == "SliceData" || name == "StringData") {
			return nil, scopeErrorf("unsafe.%s: wrong arity (M8)", name)
		}
		tv, ok := e.info.Types[c]
		if !ok || tv.Type == nil {
			return nil, scopeErrorf("unsafe.%s: untyped call (M8)", name)
		}
		typ, err := e.typeID(tv.Type)
		if err != nil {
			return nil, err
		}
		return e.exportUnsafeRuntimeCall(c, name, typ)
	default:
		return nil, scopeErrorf("unsafe.%s: unsupported intrinsic (M8)", name)
	}
}

// exportUnsafeRuntimeCall exports the argument expressions and builds the
// unsafe* wire node for a runtime intrinsic.
func (e *exporter) exportUnsafeRuntimeCall(c *ast.CallExpr, name string, typ any) (*Node, error) {
	switch name {
	case "Slice":
		ptr, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		ln, err := e.exportExpr(c.Args[1])
		if err != nil {
			return nil, err
		}
		return e.node("unsafeslice", c, map[string]any{
			"ptr": ptr, "len": ln, "type": typ,
		}), nil
	case "SliceData":
		s, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		return e.node("unsafeslicedata", c, map[string]any{
			"slice": s, "type": typ,
		}), nil
	case "String":
		ptr, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		ln, err := e.exportExpr(c.Args[1])
		if err != nil {
			return nil, err
		}
		return e.node("unsafestring", c, map[string]any{
			"ptr": ptr, "len": ln, "type": typ,
		}), nil
	case "StringData":
		s, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		return e.node("unsafestringdata", c, map[string]any{
			"s": s, "type": typ,
		}), nil
	case "Add":
		// D5/GG-0015: handles admit no byte arithmetic — fail fast.
		// Both arguments are still exported and attached (evaluated
		// before the trap, matching the existing D5 conversion trap's
		// evaluate-then-trap order) so arity/operand errors and arg
		// side effects behave.
		ptr, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		ln, err := e.exportExpr(c.Args[1])
		if err != nil {
			return nil, err
		}
		return e.node("unsafeadd", c, map[string]any{
			"ptr": ptr, "len": ln, "type": typ,
		}), nil
	default:
		return nil, scopeErrorf("unsafe.%s: unsupported runtime intrinsic (M8)", name)
	}
}

// exportUnsafeConversion lowers conversions involving unsafe.Pointer.
// dstT is the conversion target type. It returns handled=false when
// neither side is unsafe.Pointer (the caller falls through to the existing
// conversion path — notably unsafe.Pointer->uintptr keeps the D5
// ConvertNode trap).
func (e *exporter) exportUnsafeConversion(c *ast.CallExpr, dstT types.Type, x *Node, to any) (*Node, bool, error) {
	isUnsafePointer := func(t types.Type) bool {
		return t != nil && types.Unalias(t) == types.Typ[types.UnsafePointer]
	}
	srcT := e.info.TypeOf(c.Args[0])
	srcIsPtr, dstIsPtr := isUnsafePointer(srcT), isUnsafePointer(dstT)
	if !srcIsPtr && !dstIsPtr {
		return nil, false, nil
	}
	// fix-redmaster-exitcode-drift: element-indexed pointer arithmetic.
	// unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T)) is the standard Go
	// idiom for &p[k] when p is an unsafe.Pointer to element 0 of an
	// array/slice. The D5 handle model cannot materialize a handle from
	// integer arithmetic, but when the offset is structurally a whole
	// number of T-sized elements, it lowers to the "unsafeptradd" wire
	// node (element-indexed ArrayRef arithmetic in the backend). The
	// matcher (splitPtrAdd) is adopted from the r6 conformance lane's more
	// capable version: it also handles SUB, bare unsafe.Sizeof(T), and
	// const 0, and resolves Sizeof via go/types (shadowing-immune).
	// This is general: any Go code using this idiom works, not just
	// sync.Pool. Byte-arithmetic (no Sizeof factor) still traps via
	// unsafefromuint.
	if dstIsPtr && isUintptr(srcT) {
		if ptrArg, indexArg := splitPtrAdd(e, c.Args[0]); ptrArg != nil {
			ptr, err := e.exportExpr(ptrArg)
			if err != nil {
				return nil, true, err
			}
			index, err := e.exportExpr(indexArg)
			if err != nil {
				return nil, true, err
			}
			return e.node("unsafeptradd", c, map[string]any{
				"ptr": ptr, "index": index, "type": to,
			}), true, nil
		}
	}
	switch {
	case srcIsPtr && dstIsPtr:
		// Pointer->Pointer: identity.
		return x, true, nil
	case dstIsPtr && !srcIsPtr:
		// *T->unsafe.Pointer, unsafe.Pointer(nil), or
		// uintptr->unsafe.Pointer (D5 trap).
		if srcT == types.Typ[types.UntypedNil] {
			// unsafe.Pointer(nil): the nil pointer (handle 0). The
			// fromptr node maps a null child to 0.
			return e.node("unsafefromptr", c, map[string]any{
				"x": x, "type": to,
			}), true, nil
		}
		if _, ok := types.Unalias(srcT).(*types.Pointer); ok {
			return e.node("unsafefromptr", c, map[string]any{
				"x": x, "type": to,
			}), true, nil
		}
		if isUintptr(srcT) {
			return e.node("unsafefromuint", c, map[string]any{
				"x": x, "type": to,
			}), true, nil
		}
		return nil, true, scopeErrorf("cannot convert %s to unsafe.Pointer in frontend/v1 scope (M8)", srcT)
	case srcIsPtr && !dstIsPtr:
		// unsafe.Pointer->*T: resolve.
		if _, ok := types.Unalias(dstT).(*types.Pointer); ok {
			return e.node("unsafetooptr", c, map[string]any{
				"x": x, "type": to,
			}), true, nil
		}
		// unsafe.Pointer->uintptr: NOT intercepted — the existing D5
		// ConvertNode trap (GoConvert.unsafePointerToUintptr) owns it.
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// isUintptr reports whether t is the uintptr basic type.
func isUintptr(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uintptr
}

// splitPtrAdd recognizes the standard unsafe pointer-arithmetic idiom in a
// uintptr expression:
//
//	unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T))
//	unsafe.Pointer(k*unsafe.Sizeof(T) + uintptr(p))
//	unsafe.Pointer(uintptr(p) - k*unsafe.Sizeof(T))
//
// where p has type unsafe.Pointer. It returns the unsafe.Pointer operand p
// and the element-count operand k.
//
// The unsafe.Sizeof(T) factor is load-bearing: it proves the offset is a
// whole number of T-sized elements, so the backend can implement the node
// as element indexing on the array referent (ArrayRef arithmetic) rather
// than byte arithmetic on an opaque handle. A uintptr->unsafe.Pointer
// conversion whose offset is not of this form keeps the D5 trap
// (unsafefromuint): a handle can never be materialized from an arbitrary
// integer.
//
// This is a structural, general mechanism — the Go unsafe idiom itself —
// not a package- or symbol-keyed special case.
func splitPtrAdd(e *exporter, uintptrExpr ast.Expr) (ptrArg, indexArg ast.Expr) {
	bin, ok := uintptrExpr.(*ast.BinaryExpr)
	if !ok {
		return nil, nil
	}
	var ptrConv, offset ast.Expr
	neg := false
	switch bin.Op {
	case token.ADD:
		switch {
		case isUintptrOfUnsafePointer(e, bin.X):
			ptrConv, offset = bin.X, bin.Y
		case isUintptrOfUnsafePointer(e, bin.Y):
			ptrConv, offset = bin.Y, bin.X
		default:
			return nil, nil
		}
	case token.SUB:
		// Only uintptr(p) - offset. (offset - uintptr(p) would negate the
		// base handle, which is meaningless.)
		if !isUintptrOfUnsafePointer(e, bin.X) {
			return nil, nil
		}
		ptrConv, offset = bin.X, bin.Y
		neg = true
	default:
		return nil, nil
	}
	// ptrConv is uintptr(p); extract p.
	call, ok := ptrConv.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil
	}
	p := call.Args[0]
	// The offset must be k*unsafe.Sizeof(T) (in either order), a bare
	// unsafe.Sizeof(T) (k=1), or the constant 0 (identity).
	k, ok := matchSizeofMultiple(e, offset)
	if !ok {
		return nil, nil
	}
	if neg {
		un := &ast.UnaryExpr{Op: token.SUB, X: k}
		// The synthesized node has no source position; give it the type
		// of its operand so the exporter can lower it.
		if kt := e.info.TypeOf(k); kt != nil {
			e.info.Types[un] = types.TypeAndValue{Type: kt}
		}
		k = un
	}
	return p, k
}

// isUintptrOfUnsafePointer reports whether expr is a conversion uintptr(p)
// where p has type unsafe.Pointer.
func isUintptrOfUnsafePointer(e *exporter, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	// The conversion result must be uintptr ...
	if !isUintptr(e.info.TypeOf(call)) {
		return false
	}
	// ... and the operand must be unsafe.Pointer.
	argT := e.info.TypeOf(call.Args[0])
	return argT != nil && types.Unalias(argT) == types.Typ[types.UnsafePointer]
}

// matchSizeofMultiple matches k*unsafe.Sizeof(T), unsafe.Sizeof(T)*k, a bare
// unsafe.Sizeof(T) (k=1), or the constant 0, returning the element-count
// operand k.
func matchSizeofMultiple(e *exporter, expr ast.Expr) (ast.Expr, bool) {
	if isUnsafeSizeofCall(e, expr) {
		// Bare unsafe.Sizeof(T): k=1. Synthesize the literal and give it
		// type info, since it has no source position.
		lit := &ast.BasicLit{Kind: token.INT, Value: "1"}
		e.info.Types[lit] = types.TypeAndValue{
			Type:  types.Typ[types.UntypedInt],
			Value: constant.MakeInt64(1),
		}
		return lit, true
	}
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "0" {
		return lit, true
	}
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.MUL {
		return nil, false
	}
	if isUnsafeSizeofCall(e, bin.X) {
		return bin.Y, true
	}
	if isUnsafeSizeofCall(e, bin.Y) {
		return bin.X, true
	}
	return nil, false
}

// isUnsafeSizeofCall reports whether expr is a call to the unsafe.Sizeof
// builtin. The builtin is resolved via go/types (like exportUnsafeIntrinsic),
// so package aliases are handled and a local identifier named "Sizeof" can
// never match.
func isUnsafeSizeofCall(e *exporter, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sizeof" {
		return false
	}
	obj := e.info.Uses[sel.Sel]
	b, ok := obj.(*types.Builtin)
	return ok && isUnsafeIntrinsic(b) && b.Name() == "Sizeof"
}
