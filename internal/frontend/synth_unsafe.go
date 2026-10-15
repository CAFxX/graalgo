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
		if ok && tv.Value != nil {
			return e.foldedConst(c, tv.Value)
		}
		// real-reflect (Phase 3): go/types cannot fold Sizeof/Alignof/
		// Offsetof over a type parameter (e.g. 8*unsafe.Sizeof(F(0)) in
		// generic internal/strconv uscale.shortFloat, reached through the
		// real reflect dependency closure). Templates only export via
		// monomorphized instantiations, so apply the active substitution
		// and compute the size directly when the substituted type is
		// concrete. A still-generic result is a genuine scope error.
		v, err := e.unfoldedSizeValue(c, name)
		if err != nil {
			return nil, err
		}
		return e.foldedConst(c, v)
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

// unfoldedSizeValue computes the constant value of an unsafe.Sizeof,
// unsafe.Alignof, or unsafe.Offsetof call that go/types could not fold
// because the operand mentions a type parameter. The active instantiation
// substitution (e.subst) is applied to the operand type first; the result
// must be concrete (mentionsTypeParam is false) or the call is a genuine
// scope error. The value is computed with e.sizes (gc/amd64, the same
// layout go/types folds against), so the emitted constant is identical to
// what gc would fold in the instantiated body.
func (e *exporter) unfoldedSizeValue(c *ast.CallExpr, name string) (constant.Value, error) {
	pos := "?"
	if e.fset != nil {
		pos = e.fset.Position(c.Pos()).String()
	}
	fail := func(format string, args ...any) (constant.Value, error) {
		args = append(args, pos)
		return nil, scopeErrorf("unsafe.%s: "+format+" at %s", append([]any{name}, args...)...)
	}
	if len(c.Args) != 1 {
		return fail("wrong arity")
	}
	if e.sizes == nil {
		return fail("no sizes configured")
	}
	substConcrete := func(t types.Type) (types.Type, error) {
		t = e.substType(t)
		if mentionsTypeParam(t) {
			return nil, scopeErrorf("unsafe.%s: operand is not a constant expression in frontend/v1 scope (M8) at %s (type parameter not substituted)", name, pos)
		}
		return t, nil
	}
	switch name {
	case "Sizeof", "Alignof":
		atv, ok := e.info.Types[c.Args[0]]
		if !ok || atv.Type == nil {
			return fail("operand type not recorded")
		}
		t, err := substConcrete(atv.Type)
		if err != nil {
			return nil, err
		}
		var n int64
		if name == "Sizeof" {
			n = e.sizes.Sizeof(t)
		} else {
			n = e.sizes.Alignof(t)
		}
		if n < 0 {
			return fail("type too large")
		}
		return constant.MakeInt64(n), nil
	case "Offsetof":
		// Operand is x.f; resolve x's struct type and the field index.
		sel, ok := c.Args[0].(*ast.SelectorExpr)
		if !ok {
			return fail("operand is not a selector")
		}
		xtv, ok := e.info.Types[sel.X]
		if !ok || xtv.Type == nil {
			return fail("receiver type not recorded")
		}
		xt, err := substConcrete(xtv.Type)
		if err != nil {
			return nil, err
		}
		if p, ok := xt.(*types.Pointer); ok {
			xt = p.Elem()
		}
		st, ok := types.Unalias(xt).(*types.Struct)
		if !ok {
			return fail("operand is not a struct")
		}
		selec, ok := e.info.Selections[sel]
		if !ok {
			return fail("field selection not recorded")
		}
		// types.Sizes.Offsetsof takes the field path as []*types.Var; walk
		// the embedded-field index path from the selection.
		var vars []*types.Var
		cur := st
		for _, i := range selec.Index() {
			f := cur.Field(i)
			vars = append(vars, f)
			if s, ok := types.Unalias(f.Type()).(*types.Struct); ok {
				cur = s
			}
		}
		offs := e.sizes.Offsetsof(vars)
		off := offs[len(offs)-1]
		if off < 0 {
			return fail("type too large")
		}
		return constant.MakeInt64(off), nil
	}
	return fail("unsupported")
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
