package ir

import (
	"go/types"
	"slices"
)

// TypeSet is the type set of an interface as go/types defines interface
// identity (types.Identical compares the comparable flag, the normalized
// term list and the method set). go/types does not export it, so it is
// recomputed here from the interface's embedded elements, following the
// rules of go/types' typeset.go/termlist.go:
//
//   - the type set of an interface is the intersection of the type sets of
//     its embedded elements (a union is the union of its terms' sets; an
//     embedded interface contributes its own type set; any other type T
//     the single term T);
//   - if comparable is required and the term list is not "all types", the
//     terms are filtered to comparable ones and the flag is dropped;
//   - the term list is normalized: no term is a subset of another.
//
// Methods are not part of this value; they are the interface's method set.
type TypeSet struct {
	All        bool // no term restriction (only methods / comparable)
	Comparable bool // comparable required (only with All)
	Terms      []*types.Term
}

// InterfaceTypeSet computes the type set of iface.
func InterfaceTypeSet(iface *types.Interface) TypeSet {
	ts := TypeSet{All: true}
	for i := range iface.NumEmbeddeds() {
		ts = ts.intersect(elemTypeSet(iface.EmbeddedType(i)))
	}
	return ts
}

func elemTypeSet(t types.Type) TypeSet {
	t = types.Unalias(t)
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() == nil && n.Obj().Name() == "comparable" {
		return TypeSet{All: true, Comparable: true}
	}
	switch u := t.(type) {
	case *types.Union:
		var terms []*types.Term
		all := false
		for i := range u.Len() {
			tm := u.Term(i)
			if it, ok := tm.Type().Underlying().(*types.Interface); ok && !tm.Tilde() {
				sub := InterfaceTypeSet(it)
				if sub.All {
					all = true
				}
				terms = append(terms, sub.Terms...)
				continue
			}
			terms = append(terms, tm)
		}
		if all {
			return TypeSet{All: true}
		}
		return TypeSet{Terms: normTerms(terms)}
	}
	if it, ok := t.Underlying().(*types.Interface); ok {
		return InterfaceTypeSet(it)
	}
	return TypeSet{Terms: []*types.Term{types.NewTerm(false, t)}}
}

func (x TypeSet) intersect(y TypeSet) TypeSet {
	var r TypeSet
	switch {
	case x.All && y.All:
		r = TypeSet{All: true}
	case x.All:
		r = TypeSet{Terms: y.Terms}
	case y.All:
		r = TypeSet{Terms: x.Terms}
	default:
		var terms []*types.Term
		for _, a := range x.Terms {
			for _, b := range y.Terms {
				if c := termIntersect(a, b); c != nil {
					terms = append(terms, c)
				}
			}
		}
		r = TypeSet{Terms: normTerms(terms)}
	}
	comp := x.Comparable || y.Comparable
	if comp && !r.All {
		var keep []*types.Term
		for _, t := range r.Terms {
			if types.Comparable(t.Type()) {
				keep = append(keep, t)
			}
		}
		r.Terms = keep
		comp = false
	}
	r.Comparable = comp
	return r
}

// termDisjoint: go/types term.disjoint.
func termDisjoint(x, y *types.Term) bool {
	ux := x.Type()
	if y.Tilde() {
		ux = ux.Underlying()
	}
	uy := y.Type()
	if x.Tilde() {
		uy = uy.Underlying()
	}
	return !types.Identical(ux, uy)
}

// termIntersect: go/types term.intersect (nil = empty).
func termIntersect(x, y *types.Term) *types.Term {
	if termDisjoint(x, y) {
		return nil
	}
	if !x.Tilde() || y.Tilde() {
		return x
	}
	return y
}

// normTerms merges overlapping terms (go/types termlist.norm): the union
// of two non-disjoint terms is the larger one.
func normTerms(list []*types.Term) []*types.Term {
	used := make([]bool, len(list))
	var out []*types.Term
	for i, x := range list {
		if used[i] {
			continue
		}
		for j := i + 1; j < len(list); j++ {
			y := list[j]
			if used[j] || termDisjoint(x, y) {
				continue
			}
			if !x.Tilde() && y.Tilde() {
				x = y
			}
			used[j] = true
		}
		out = append(out, x)
	}
	return slices.Clip(out)
}
