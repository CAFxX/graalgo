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
//	Add                                       (builtin: derives from its live
//	                                           pointer operand, fix-redmaster-r39)
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
//     intern/resolve over the handle table; uintptr->Pointer panics with
//     the documented D5 message (review-20261031 R-06) unless the
//     frontend proves the operand's provenance, in which case it lowers
//     to the "unsafederive" node (fix-redmaster-r39, see
//     unsafe_derive.go); Pointer->uintptr keeps the existing D5
//     ConvertNode behavior (handle as identity token).
//   - unsafe.Add derives from its live pointer operand by the byte
//     length (fix-redmaster-r39; formerly the D5 trap).
//   - DIV-US-1: unsafe.String copies the bytes into a fresh GoString.
//     GoString is immutable (hashCode/equals read the internal array), so
//     sharing the caller's array would corrupt map keys. Post-call writes
//     through the original pointer are invisible to the string — an
//     intentional, documented divergence from gc.
//   - Fail-fast (loud, with re-test triggers): unproven
//     uintptr->Pointer conversions (Go panic, D5); over-long Slice len
//     and carrier-mismatched referents panic with gc-exact messages at
//     runtime.
package main

import (
	"fmt"
	"go/ast"
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
		// fix-redmaster-r39: derives from the live pointer operand
		// (UnsafeTable.derive). Both arguments are still exported and
		// attached (evaluated before the derivation, matching the
		// D5 conversion node's evaluate-then-fail order) so
		// arity/operand errors and arg side effects behave.
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
			// fix-redmaster-r39: a provenance-proven operand (one
			// pointer-derived additive term plus pure integer terms)
			// lowers to the derive node; anything else keeps the D5
			// trap. See unsafe_derive.go.
			if n, handled, err := e.exportUintptrToPointer(c, to); err != nil {
				return nil, true, err
			} else if handled {
				return n, true, nil
			}
			return e.node("unsafefromuint", c, map[string]any{
				"x": x, "type": to,
			}), true, nil
		}
		return nil, true, scopeErrorf("cannot convert %s to unsafe.Pointer in frontend/v1 scope (M8)", srcT)
	case srcIsPtr && !dstIsPtr:
		// unsafe.Pointer->*T: resolve.
		if ptr, ok := types.Unalias(dstT).(*types.Pointer); ok {
			fields := map[string]any{
				"x": x, "type": to,
			}
			// fix-redmaster-r39: the byte size of T from the same
			// go/types Sizes oracle that folds unsafe.Sizeof, so a
			// byte-derived pointer (unsafederive) can be indexed at
			// the backend. Omitted when T has no fixed size.
			if sz, ok := safeSizeof(e.sizes, ptr.Elem()); ok && sz > 0 {
				fields["elemSize"] = sz
			}
			return e.node("unsafetooptr", c, fields), true, nil
		}
		// unsafe.Pointer->uintptr: NOT intercepted — the existing D5
		// ConvertNode trap (GoConvert.unsafePointerToUintptr) owns it.
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// isUintptr reports whether t is uintptr: the uintptr basic type or a
// named type whose underlying type is uintptr (e.g. `type addr uintptr`).
// Conversions to and from such types behave exactly like uintptr
// conversions (gc-verified), so the provenance proof and the conversion
// dispatch treat them identically.
func isUintptr(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uintptr
}
