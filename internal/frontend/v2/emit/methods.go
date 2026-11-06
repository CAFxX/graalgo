package emit

// Method tables for the v2 type table.
//
// The wire shape mirrors v1's (internal/frontend/export.go:
// methodTableEntries / methodTableEntry / embeddedInterfacePromotion,
// ADR-0003) — same entry fields, same implkey semantics — but only the
// general mechanism is reimplemented here. v1's generic-monomorphization,
// multi-result-helper, and disk-stub branches do not apply to v2 (v2 has
// no monomorphization, no MR helpers, no disk importer); a generic
// instantiation's method object resolves to no funcdecl in this program
// and fails loudly at dispatch with "unresolved method object N"
// (see docs/arch/frontend-v2-method-dispatch.md §7.5).

import (
	"fmt"
	"go/types"
)

// maxStructDepth caps struct value nesting for offset computation (v1 M10
// P1-4): go/types' Sizeof is exponential in nesting depth.
const maxStructDepth = 16

// methodTableEntries builds the wire "methods" list for a type: one entry
// per method in types.NewMethodSet(t), in go/types order (sorted by
// method name). NewMethodSet — not NumMethods — is the source: it covers
// promoted methods (via embedded fields) and, for pointer types, the
// pointer-receiver methods, which NumMethods on the named type would also
// list but which belong to *T's set, not T's.
//
// An entry is {"name","obj","sig","implkey","recv","index","addrOf"}, plus
// "embeddedIface" on redispatch entries (see embeddedInterfacePromotion).
// "obj" is the object-table id of the method's *types.Func — always
// resolvable via declareObject, even for methods with no funcdecl in this
// program (imported methods, interface methods promoted via an embedded
// interface field): dropping them would make the type look like it lacks
// the method, which is the R-P1-2 bug class. The Java side resolves obj
// lazily through the program funcTable and fails loudly when it is absent.
// The one exception is the embedded-interface promotion, where obj is -1
// by construction: no GoFunc exists for such a method, and the dispatcher
// must redispatch on the embedded interface value instead.
func (e *Emitter) methodTableEntries(t types.Type) ([]any, error) {
	ms := types.NewMethodSet(t)
	entries := make([]any, 0, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		mobj, ok := sel.Obj().(*types.Func)
		if !ok {
			return nil, fmt.Errorf("emit: method set entry %d of %v is not a func (%T)", i, t, sel.Obj())
		}
		en, err := e.methodTableEntry(t, sel, mobj)
		if err != nil {
			return nil, err
		}
		entries = append(entries, en)
	}
	return entries, nil
}

// methodTableEntry builds one wire method-table entry for mobj as selected
// by sel on type t.
func (e *Emitter) methodTableEntry(t types.Type, sel *types.Selection, mobj *types.Func) (map[string]any, error) {
	msig, ok := mobj.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("emit: method %s has non-signature type %T", mobj.Name(), mobj.Type())
	}
	sigID, err := e.typeID(mobj.Type())
	if err != nil {
		return nil, err
	}
	// The declared receiver kind: value or pointer. The signature's Recv
	// is the DECLARED receiver (the embedded type for promoted methods),
	// not the selecting type.
	recv := "value"
	if msig.Recv() != nil {
		if _, ok := msig.Recv().Type().(*types.Pointer); ok {
			recv = "pointer"
		}
	}
	// Selection.Index() for a method is [embedded field path...,
	// method index in the declaring type] (go/types selection.go):
	// the receiver adaptation path is everything but the last entry —
	// empty for direct methods, e.g. [0] for a method promoted via
	// embedded field 0. Always an array, never null.
	index := sel.Index()
	var path []any
	if len(index) > 1 {
		path = make([]any, len(index)-1)
		for i, v := range index[:len(index)-1] {
			path[i] = v
		}
	} else {
		path = []any{}
	}
	// A method promoted via an embedded interface field has no funcdecl:
	// its method object is the interface's declaration. Emit a
	// redispatch entry (obj -1) so the method is not silently dropped
	// from the table; the dispatcher reads the embedding path to the
	// interface field value and dispatches the method on it.
	if embIface, embPath := embeddedInterfacePromotion(t, sel); embIface != nil {
		embID, err := e.typeID(embIface)
		if err != nil {
			return nil, err
		}
		embPathAny := make([]any, len(embPath))
		for i, v := range embPath {
			embPathAny[i] = v
		}
		return map[string]any{
			"name":          mobj.Name(),
			"obj":           -1,
			"sig":           sigID,
			"implkey":       implKeyOf(mobj, msig),
			"recv":          recv,
			"index":         embPathAny,
			"addrOf":        false,
			"embeddedIface": embID,
		}, nil
	}
	// A pointer-receiver method promoted via a value-embedded field: the
	// dispatcher must take the address of the promoted field value (gc's
	// implicit &). Sound for struct-typed fields (read by reference —
	// the live field object), so mark supported with addrOf. For
	// non-struct, non-pointer fields, address-taking would need
	// writeback (out of scope): mark unsupported (index null) rather
	// than failing the emission — the failure fires only if the method
	// is actually dispatched (fail fast at dispatch, named scope error),
	// never for programs that merely declare the type.
	unsupported := false
	addrOf := false
	if len(path) > 0 && recv == "pointer" {
		if ft := embeddedFieldType(t, path); ft != nil {
			for {
				if nt, ok := ft.(*types.Named); ok {
					ft = nt.Underlying()
					continue
				}
				break
			}
			if _, ok := ft.(*types.Pointer); !ok {
				if _, ok := ft.(*types.Struct); ok {
					addrOf = true
				} else {
					unsupported = true
				}
			}
		}
	}
	objID, err := e.declareObject(mobj)
	if err != nil {
		return nil, err
	}
	entry := map[string]any{
		"name":    mobj.Name(),
		"obj":     objID,
		"sig":     sigID,
		"implkey": implKeyOf(mobj, msig),
		"recv":    recv,
		"index":   path,
		"addrOf":  addrOf,
	}
	if unsupported {
		// The method exists but cannot be dispatched through this
		// embedding (address-taking would need writeback). The entry
		// stays in the table so the type's method set is complete;
		// index null marks it undispatched-able — the dispatcher
		// fails fast with a named error if it is ever selected.
		entry["index"] = nil
	}
	return entry, nil
}

// embeddedFieldType follows a field-index path from type t and returns the
// type of the field the path lands on (nil when the path is invalid).
// Pointer and named wrappers are unwrapped at each step, mirroring
// go/types' selection semantics.
func embeddedFieldType(t types.Type, path []any) types.Type {
	cur := t
	for _, pi := range path {
		fi, ok := pi.(int)
		if !ok {
			return nil
		}
		if p, ok := cur.(*types.Pointer); ok {
			cur = p.Elem()
		}
		if n, ok := cur.(*types.Named); ok {
			cur = n.Underlying()
		}
		st, ok := cur.(*types.Struct)
		if !ok {
			return nil
		}
		if fi < 0 || fi >= st.NumFields() {
			return nil
		}
		cur = st.Field(fi).Type()
	}
	return cur
}

// embeddedInterfacePromotion reports whether the method selection sel on
// type t is a method promoted via an embedded interface field (e.g.
// (*crypto/tls.listener).Addr, promoted from the embedded net.Listener).
// It returns the embedded field's interface type (unwrapped to the
// *types.Interface) and the field path from t to that interface value.
// It returns (nil, nil) when the method is not promoted through an
// embedded interface — including embedding *I (pointer-to-interface),
// which keeps the old skip behavior (fail fast at dispatch, never
// silent).
func embeddedInterfacePromotion(t types.Type, sel *types.Selection) (types.Type, []int) {
	idx := sel.Index()
	if len(idx) < 2 {
		return nil, nil // not promoted
	}
	path := idx[:len(idx)-1]
	cur := t
	ifacePath := make([]int, 0, len(path))
	for _, fi := range path {
		if p, ok := cur.(*types.Pointer); ok {
			cur = p.Elem()
		}
		if n, ok := cur.(*types.Named); ok {
			cur = n.Underlying()
		}
		st, ok := cur.(*types.Struct)
		if !ok {
			return nil, nil
		}
		if fi < 0 || fi >= st.NumFields() {
			return nil, nil
		}
		f := st.Field(fi)
		ifacePath = append(ifacePath, fi)
		ft := types.Unalias(f.Type())
		if nt, ok := ft.(*types.Named); ok {
			if ui, ok := nt.Underlying().(*types.Interface); ok {
				return ui, ifacePath
			}
			// Named non-interface field: keep walking — a struct
			// field may embed the interface deeper.
			cur = ft
			continue
		}
		if ui, ok := ft.(*types.Interface); ok {
			return ui, ifacePath
		}
		cur = ft
	}
	return nil, nil
}

// sizesWouldPanic reports whether computing sizes for t would panic in
// go/types: bare type parameters (Alignof/Sizeof panic), reachable by
// descending through array element and struct field types. Field offsets
// of a generic struct are instantiation-dependent anyway, so they are not
// computed: the wire carries -1 ("unknown").
func sizesWouldPanic(t types.Type) bool {
	seen := make(map[types.Type]bool)
	var walk func(t types.Type) bool
	walk = func(t types.Type) bool {
		if t == nil {
			return false
		}
		if _, ok := t.(*types.TypeParam); ok {
			return true
		}
		if seen[t] {
			return false
		}
		seen[t] = true
		switch u := t.(type) {
		case *types.Named:
			return walk(u.Underlying())
		case *types.Array:
			return walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if walk(u.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
}

// structValueDepth returns the nesting depth of value-typed structs/arrays
// in t (0 for a flat type). Used with maxStructDepth to cap offset
// computation (go/types' Sizeof is exponential in nesting depth).
func structValueDepth(t types.Type, depth int) int {
	if depth > maxStructDepth {
		return depth
	}
	switch t := t.(type) {
	case *types.Named:
		return structValueDepth(t.Underlying(), depth)
	case *types.Struct:
		max := depth
		for i := 0; i < t.NumFields(); i++ {
			if d := structValueDepth(t.Field(i).Type(), depth+1); d > max {
				max = d
			}
			if max > maxStructDepth {
				return max
			}
		}
		return max
	case *types.Array:
		return structValueDepth(t.Elem(), depth+1)
	default:
		return depth
	}
}
