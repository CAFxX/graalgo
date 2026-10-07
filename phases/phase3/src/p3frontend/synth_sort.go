package p3frontend

// P3-M7 (wave 1): p3frontend mirror of the phase-1 synthetic "sort"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); this mirror pins the
// signatures so drift fails fast.
//
// The signatures match phases/phase1/frontend/synth_sort.go exactly,
// which carries the exact gc 1.27.1 public API (sort.go, search.go,
// slice.go). go1.27.1's sort has no generic top-level functions (those
// live in `slices`), so the whole surface is non-generic. Unexported
// implementation details (reverse, pdqsort internals, sortedHint,
// xorshift, carriers) are not visible to user code and are not mirrored.
// Find/Slice/SliceStable/SliceIsSorted keep exact gc signatures with
// fail-fast bodies in the synthetic source (multi-result/reflect).

import (
	"go/token"
	"go/types"
)

// syntheticSortPackage mirrors the P3-M7 synthetic "sort".
func syntheticSortPackage() *types.Package {
	pkg := types.NewPackage("sort", "sort")
	intT := types.Typ[types.Int]
	boolT := types.Typ[types.Bool]
	stringT := types.Typ[types.String]
	float64T := types.Typ[types.Float64]
	anyT := types.Universe.Lookup("any").Type()

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insertFunc := func(name string, params, results []*types.Var) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	// Interface: Len() int, Less(i, j int) bool, Swap(i, j int).
	ifaceMethods := []*types.Func{
		types.NewFunc(token.NoPos, pkg, "Len",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(newParam("", intT)), false)),
		types.NewFunc(token.NoPos, pkg, "Less",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("i", intT), newParam("j", intT)),
				types.NewTuple(newParam("", boolT)), false)),
		types.NewFunc(token.NoPos, pkg, "Swap",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("i", intT), newParam("j", intT)),
				types.NewTuple(), false)),
	}
	ifaceType := types.NewInterfaceType(ifaceMethods, nil).Complete()
	ifaceName := types.NewTypeName(token.NoPos, pkg, "Interface", nil)
	pkg.Scope().Insert(ifaceName)
	ifaceNamed := types.NewNamed(ifaceName, ifaceType, nil)

	// lessFunc = func(i, j int) bool (for Slice/SliceStable/SliceIsSorted).
	lessFunc := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("i", intT), newParam("j", intT)),
		types.NewTuple(newParam("", boolT)), false)
	// searchFunc = func(int) bool; cmpFunc = func(int) int.
	searchFunc := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("", intT)),
		types.NewTuple(newParam("", boolT)), false)
	cmpFunc := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(newParam("", intT)),
		types.NewTuple(newParam("", intT)), false)

	// Convenience slice types: IntSlice/Float64Slice/StringSlice with
	// Len/Less/Swap/Sort/Search methods.
	addSliceType := func(name string, elem types.Type, searchParam types.Type) {
		typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(typeName)
		named := types.NewNamed(typeName, types.NewSlice(elem), nil)
		recv := types.NewParam(token.NoPos, pkg, "", named)
		methods := []struct {
			mname   string
			params  []*types.Var
			results []*types.Var
		}{
			{"Len", nil, []*types.Var{newParam("", intT)}},
			{"Less", []*types.Var{newParam("i", intT), newParam("j", intT)},
				[]*types.Var{newParam("", boolT)}},
			{"Swap", []*types.Var{newParam("i", intT), newParam("j", intT)}, nil},
			{"Sort", nil, nil},
			{"Search", []*types.Var{newParam("x", searchParam)},
				[]*types.Var{newParam("", intT)}},
		}
		for _, m := range methods {
			sig := types.NewSignatureType(recv, nil, nil,
				types.NewTuple(m.params...), types.NewTuple(m.results...), false)
			named.AddMethod(types.NewFunc(token.NoPos, pkg, m.mname, sig))
		}
	}
	addSliceType("IntSlice", intT, intT)
	addSliceType("Float64Slice", float64T, float64T)
	addSliceType("StringSlice", stringT, stringT)

	intSlice := types.NewSlice(intT)
	float64Slice := types.NewSlice(float64T)
	stringSlice := types.NewSlice(stringT)

	// Package-level functions.
	insertFunc("Sort",
		[]*types.Var{newParam("data", ifaceNamed)},
		nil)
	insertFunc("Stable",
		[]*types.Var{newParam("data", ifaceNamed)},
		nil)
	insertFunc("IsSorted",
		[]*types.Var{newParam("data", ifaceNamed)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Reverse",
		[]*types.Var{newParam("data", ifaceNamed)},
		[]*types.Var{newParam("", ifaceNamed)})
	insertFunc("Ints",
		[]*types.Var{newParam("x", intSlice)},
		nil)
	insertFunc("Float64s",
		[]*types.Var{newParam("x", float64Slice)},
		nil)
	insertFunc("Strings",
		[]*types.Var{newParam("x", stringSlice)},
		nil)
	insertFunc("IntsAreSorted",
		[]*types.Var{newParam("x", intSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Float64sAreSorted",
		[]*types.Var{newParam("x", float64Slice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("StringsAreSorted",
		[]*types.Var{newParam("x", stringSlice)},
		[]*types.Var{newParam("", boolT)})
	insertFunc("Search",
		[]*types.Var{newParam("n", intT), newParam("f", searchFunc)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Find",
		[]*types.Var{newParam("n", intT), newParam("cmp", cmpFunc)},
		[]*types.Var{newParam("i", intT), newParam("found", boolT)})
	insertFunc("SearchInts",
		[]*types.Var{newParam("a", intSlice), newParam("x", intT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("SearchFloat64s",
		[]*types.Var{newParam("a", float64Slice), newParam("x", float64T)},
		[]*types.Var{newParam("", intT)})
	insertFunc("SearchStrings",
		[]*types.Var{newParam("a", stringSlice), newParam("x", stringT)},
		[]*types.Var{newParam("", intT)})
	insertFunc("Slice",
		[]*types.Var{newParam("x", anyT), newParam("less", lessFunc)},
		nil)
	insertFunc("SliceStable",
		[]*types.Var{newParam("x", anyT), newParam("less", lessFunc)},
		nil)
	insertFunc("SliceIsSorted",
		[]*types.Var{newParam("x", anyT), newParam("less", lessFunc)},
		[]*types.Var{newParam("", boolT)})

	return pkg
}
