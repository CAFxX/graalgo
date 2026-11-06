package emit

// Method identity keys (implkeys) for the v2 type table.
//
// The algorithm is v1's (internal/frontend/export.go: implKeyOf /
// sigKeyOf / typeKeyOf, ADR-0003), reimplemented here because v2 is a
// separate module and must not import the v1 exporter. The two MUST stay
// in agreement: an implkey computed by v2 for a method must equal the one
// v1 computes for the same method, or the interface implements checks on
// the two sides would disagree about what "implements" means. Any change
// here must be mirrored in v1 (and vice versa) — the methodsets tests pin
// the shared cases (byte/uint8 identity, alias expansion, unexported
// qualification, variadic tails).
//
// An implkey identifies a method by (name, signature) — never by name
// alone. Same-name / wrong-signature methods do not satisfy an interface
// (R1 P0-1 in v1).

import (
	"fmt"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// implKeyOf returns the method-identity key: the method name (unexported
// names qualified with their package path, as gc compares them) plus a
// structural signature key WITHOUT the receiver (interface method
// signatures never declare one; the caller passes the receiver-stripped
// signature — sigKeyOf renders only params/results/variadic, so no
// stripping is needed). Two methods share an implkey iff gc considers
// their (name, signature) pairs identical: aliases are expanded — e.g.
// func (T) Write(s B) with `type B = []byte` implements
// interface{ Write([]byte) int } — and parameter names are ignored (gc
// compares types, not names). The "\x00" separator cannot collide:
// neither method names nor signature keys contain it.
func implKeyOf(mobj *types.Func, sig *types.Signature) string {
	name := mobj.Name()
	if !mobj.Exported() {
		if pkg := mobj.Pkg(); pkg != nil {
			name = pkg.Path() + "." + name
		}
	}
	return name + "\x00" + sigKeyOf(sig)
}

// sigKeyOf renders a method signature structurally (receiver excluded):
// "func(" params ")(" results ")", with "..." for the variadic tail.
// Parameter/result names are ignored.
func sigKeyOf(sig *types.Signature) string {
	var sb strings.Builder
	sb.WriteString("func(")
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		pt := params.At(i).Type()
		if sig.Variadic() && i == params.Len()-1 {
			// go/types represents the variadic tail as []T; render as
			// ...T so f(...int) and f([]int) key differently (gc
			// distinguishes them).
			if s, ok := pt.(*types.Slice); ok {
				sb.WriteString("..." + typeKeyOf(s.Elem()))
			} else {
				sb.WriteString(typeKeyOf(pt)) // unreachable per go/types invariant
			}
		} else {
			sb.WriteString(typeKeyOf(pt))
		}
	}
	sb.WriteString(")(")
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(typeKeyOf(results.At(i).Type()))
	}
	sb.WriteString(")")
	return sb.String()
}

// typeKeyOf renders a type structurally for signature comparison. Aliases
// are expanded (types.Unalias); named types render as qualified name +
// type arguments (never expanded — expansion would not terminate on
// recursive named types, and qualified names are unique within a program);
// composites recurse. Two types share a key iff they are identical in gc's
// sense; no position or naming trivia leaks in.
func typeKeyOf(t types.Type) string {
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		// Key by kind, not name: "byte" and "uint8" are identical types
		// (same BasicKind) but have different Basic names — keying by
		// name would wrongly reject `Write([]uint8)` as an implementation
		// of `interface{ Write([]byte) }` (gc: implements).
		return "basic:" + strconv.Itoa(int(t.Kind()))
	case *types.Named:
		var sb strings.Builder
		sb.WriteString("named:" + qualifiedTypeName(t.Obj()))
		if targs := t.TypeArgs(); targs != nil && targs.Len() > 0 {
			sb.WriteString("[")
			for i := 0; i < targs.Len(); i++ {
				if i > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(typeKeyOf(targs.At(i)))
			}
			sb.WriteString("]")
		}
		return sb.String()
	case *types.Pointer:
		return "ptr(" + typeKeyOf(t.Elem()) + ")"
	case *types.Slice:
		return "slice(" + typeKeyOf(t.Elem()) + ")"
	case *types.Array:
		return "array(" + strconv.FormatInt(t.Len(), 10) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Map:
		return "map(" + typeKeyOf(t.Key()) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Chan:
		return "chan(" + strconv.FormatInt(int64(t.Dir()), 10) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Signature:
		// Func-typed parameter/result: signatures nest without receivers.
		return sigKeyOf(t)
	case *types.Struct:
		var sb strings.Builder
		sb.WriteString("struct(")
		for i := 0; i < t.NumFields(); i++ {
			if i > 0 {
				sb.WriteString(";")
			}
			f := t.Field(i)
			sb.WriteString(f.Name() + ":" + typeKeyOf(f.Type()))
			if tag := t.Tag(i); tag != "" {
				sb.WriteString("`" + tag + "`")
			}
		}
		sb.WriteString(")")
		return sb.String()
	case *types.Interface:
		if t.Empty() {
			return "interface{}"
		}
		ms := types.NewMethodSet(t)
		keys := make([]string, 0, ms.Len())
		for i := 0; i < ms.Len(); i++ {
			sel := ms.At(i)
			mo, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}
			msig, ok := mo.Type().(*types.Signature)
			if !ok {
				continue
			}
			keys = append(keys, implKeyOf(mo, msig))
		}
		sort.Strings(keys)
		return "interface(" + strings.Join(keys, ";") + ")"
	case *types.Union:
		var sb strings.Builder
		sb.WriteString("union(")
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				sb.WriteString(";")
			}
			term := t.Term(i)
			if term.Tilde() {
				sb.WriteString("~")
			}
			sb.WriteString(typeKeyOf(term.Type()))
		}
		sb.WriteString(")")
		return sb.String()
	case *types.TypeParam:
		// Unreachable in practice: generic declarations fail loudly
		// before any implkey is computed. Total function anyway.
		return "typeparam:" + t.Obj().Name()
	}
	return "unknown:" + fmt.Sprintf("%T", t)
}

// qualifiedTypeName renders a named type's object as pkg.Path + "." + name
// (bare name when there is no package, e.g. universe predeclared types).
func qualifiedTypeName(obj *types.TypeName) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}
