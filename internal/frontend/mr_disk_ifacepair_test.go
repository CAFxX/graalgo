package main

// Regression tests for the disk multi-result transform's interface pair
// types (transformDiskMultiResult in mr_disk.go, BUG-MODPROBE-005).
//
// RV18 P1-1: the generated __Iface_MPair type dropped the interface's
// type parameters, so a generic interface with a multi-result method
// (e.g. type Foo[T any] interface { M() (T, error) }) failed to type-check
// with "undefined: T" when the disk package was built.
// RV18 P1-2: only direct FuncType members were enumerated; methods
// promoted from embedded interfaces got no pair type and no spec, so the
// interface dispatch lookup found nothing for calls through the outer
// interface.
//
// Each test parses a snippet, runs transformDiskMultiResult, and checks
// the rewritten AST / specs. transformAndCheckDiskMR (mr_disk_test.go)
// type-checks the rewritten AST.

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// transformDiskMRCollect parses src and runs the disk multi-result
// transform, returning the specs and the rewritten file.
func transformDiskMRCollect(t *testing.T, src string) ([]diskMRSpec, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	return specs, f
}

// findPairTypeSpec returns the generated pair *ast.TypeSpec with the given
// name in the rewritten file, or nil.
func findPairTypeSpec(f *ast.File, name string) *ast.TypeSpec {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == name {
				return ts
			}
		}
	}
	return nil
}

// specFor reports whether specs contains an entry for (method, recvType).
func specFor(specs []diskMRSpec, method, recvType string) *diskMRSpec {
	for i := range specs {
		if specs[i].name == method && specs[i].recvType == recvType {
			return &specs[i]
		}
	}
	return nil
}

func TestDiskMRInterfacePairTypeParams(t *testing.T) {
	// RV18 P1-1: a generic interface with a multi-result method. The
	// generated pair type must carry the interface's type parameters;
	// otherwise the field referencing T does not type-check.
	// (The probe func gets past the pre-RV18 mrNames early return so the
	// pair builder runs; the interface itself is what is under test.)
	src := `package p

func probe() (int, error) { return 1, nil }

type Box[T any] interface {
	Load(k string) (T, error)
}
`
	// Red pre-fix: "undefined: T".
	checkDiskMRClean(t, src)
	specs, f := transformDiskMRCollect(t, src)
	sp := specFor(specs, "Load", "Box")
	if sp == nil {
		t.Fatalf("no spec for (Load, Box)")
	}
	if sp.pairType != "__Box_LoadPair" {
		t.Fatalf("pairType = %q, want __Box_LoadPair", sp.pairType)
	}
	ts := findPairTypeSpec(f, "__Box_LoadPair")
	if ts == nil {
		t.Fatalf("__Box_LoadPair not generated")
	}
	if got := len(tparamNamesOfFL(ts.TypeParams)); got != 1 {
		t.Fatalf("__Box_LoadPair has %d type params, want 1", got)
	}
}

func TestDiskMREmbeddedInterfacePairSpec(t *testing.T) {
	// RV18 P1-2: an outer interface embedding an inner one with a
	// multi-result method. The promoted method must get a pair type and
	// a spec keyed by the OUTER interface, so a call through the outer
	// interface resolves.
	src := `package p

type Inner interface {
	M() (int, error)
}

type Outer interface {
	Inner
	N() (string, error)
}

func use(o Outer) (int, error) {
	return o.M()
}
`
	checkDiskMRClean(t, src)
	specs, f := transformDiskMRCollect(t, src)
	// The declaring interface keeps its own spec (pre-existing).
	if sp := specFor(specs, "M", "Inner"); sp == nil {
		t.Fatalf("no spec for (M, Inner)")
	}
	// Red pre-fix: no spec for the promoted method under Outer.
	sp := specFor(specs, "M", "Outer")
	if sp == nil {
		t.Fatalf("no spec for promoted (M, Outer)")
	}
	if sp.pairType != "__Outer_MPair" {
		t.Fatalf("pairType = %q, want __Outer_MPair", sp.pairType)
	}
	if len(sp.pairFields) != 2 || sp.pairFields[0] != "f1" || sp.pairFields[1] != "f2" {
		t.Fatalf("pairFields = %v, want [f1 f2]", sp.pairFields)
	}
	if ts := findPairTypeSpec(f, "__Outer_MPair"); ts == nil {
		t.Fatalf("__Outer_MPair not generated")
	}
	// The direct method still gets its own pair type.
	if ts := findPairTypeSpec(f, "__Outer_NPair"); ts == nil {
		t.Fatalf("__Outer_NPair not generated")
	}
}

func TestDiskMREmbeddedInterfaceCallThroughOuter(t *testing.T) {
	// RV18 P1-2, call side: for o.M() with o of type Outer, the
	// interface-dispatch lookup key derived from the call (the interface
	// the call is made through) must resolve to a spec.
	src := `package p

type Inner interface {
	M() (int, error)
}

type Outer interface {
	Inner
}

func use(o Outer) (int, error) {
	return o.M()
}
`
	specs, _ := transformDiskMRCollect(t, src)
	if errs := transformAndCheckDiskMR(t, src); len(errs) != 0 {
		t.Fatalf("type errors: %v", errs)
	}
	// Fresh parse for the selection: type-check the transformed file to
	// obtain the call's types.Selection.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("example.com/p", fset, []*ast.File{f}, info); err != nil {
		t.Fatalf("type-check error: %v", err)
	}
	var sel *types.Selection
	ast.Inspect(f, func(n ast.Node) bool {
		se, ok := n.(*ast.SelectorExpr)
		if !ok || se.Sel.Name != "M" {
			return true
		}
		if s, ok := info.Selections[se]; ok && s.Kind() == types.MethodVal {
			sel = s
		}
		return true
	})
	if sel == nil {
		t.Fatalf("no MethodVal selection for o.M()")
	}
	// The lookup key the fixed classifier derives from the call: the
	// interface the call is made through.
	rnt, ok := sel.Recv().(*types.Named)
	if !ok || rnt.Obj() == nil {
		t.Fatalf("selection recv not a named interface: %T", sel.Recv())
	}
	if got := rnt.Obj().Name(); got != "Outer" {
		t.Fatalf("call-through interface = %q, want Outer", got)
	}
	// Red pre-fix: no spec keyed by the outer interface.
	if sp := specFor(specs, "M", rnt.Obj().Name()); sp == nil {
		t.Fatalf("no spec for (M, %s): call through Outer would not desugar", rnt.Obj().Name())
	}
}

func TestDiskMREmbeddedInterfaceTransitive(t *testing.T) {
	// RV18 P1-2: embedding chains are walked transitively.
	src := `package p

type Inner interface {
	M() (int, error)
}

type Middle interface {
	Inner
}

type Outer interface {
	Middle
}
`
	checkDiskMRClean(t, src)
	specs, _ := transformDiskMRCollect(t, src)
	// Red pre-fix: no spec for (M, Outer).
	if sp := specFor(specs, "M", "Outer"); sp == nil {
		t.Fatalf("no spec for transitively promoted (M, Outer)")
	}
	if sp := specFor(specs, "M", "Middle"); sp == nil {
		t.Fatalf("no spec for transitively promoted (M, Middle)")
	}
}

func TestDiskMREmbeddedGenericInterface(t *testing.T) {
	// RV18 P1-1 + P1-2: a generic inner interface embedded (instantiated)
	// in a generic outer interface. The outer pair type carries the
	// outer's type parameters with the instantiation substituted.
	src := `package p

type Inner[T any] interface {
	M() (T, error)
}

type Outer[T any] interface {
	Inner[T]
}
`
	checkDiskMRClean(t, src)
	specs, f := transformDiskMRCollect(t, src)
	// Red pre-fix: no spec for the promoted method under Outer.
	sp := specFor(specs, "M", "Outer")
	if sp == nil {
		t.Fatalf("no spec for promoted (M, Outer)")
	}
	ts := findPairTypeSpec(f, "__Outer_MPair")
	if ts == nil {
		t.Fatalf("__Outer_MPair not generated")
	}
	if got := len(tparamNamesOfFL(ts.TypeParams)); got != 1 {
		t.Fatalf("__Outer_MPair has %d type params, want 1", got)
	}
}

func TestDiskMREmbeddedConcreteInstantiation(t *testing.T) {
	// RV18 P1-2: embedding a generic interface instantiated with a
	// concrete type. The outer pair type is non-generic with the
	// substituted result types.
	src := `package p

type Inner[T any] interface {
	M() (T, error)
}

type Outer interface {
	Inner[int]
}
`
	checkDiskMRClean(t, src)
	specs, f := transformDiskMRCollect(t, src)
	sp := specFor(specs, "M", "Outer")
	if sp == nil {
		t.Fatalf("no spec for promoted (M, Outer)")
	}
	ts := findPairTypeSpec(f, "__Outer_MPair")
	if ts == nil {
		t.Fatalf("__Outer_MPair not generated")
	}
	if ts.TypeParams != nil && len(ts.TypeParams.List) != 0 {
		t.Fatalf("__Outer_MPair should be non-generic")
	}
	// f1 must be int (T substituted), not the unbound T.
	st, ok := ts.Type.(*ast.StructType)
	if !ok || st.Fields == nil || len(st.Fields.List) != 2 {
		t.Fatalf("__Outer_MPair has unexpected shape")
	}
	if id, ok := st.Fields.List[0].Type.(*ast.Ident); !ok || id.Name != "int" {
		t.Fatalf("f1 type = %v, want int (T substituted)", st.Fields.List[0].Type)
	}
}

func TestDiskMREmbeddedDuplicateMethodDeduped(t *testing.T) {
	// A method declared directly with the same signature as a promoted
	// one is the same method (legal Go): exactly one __Outer_MPair, no
	// redeclaration.
	// (Green-green guard: without dedup the walk would emit the pair
	// type twice — a redeclaration type error.)
	src := `package p

type Inner interface {
	M() (int, error)
}

type Outer interface {
	Inner
	M() (int, error)
}
`
	checkDiskMRClean(t, src)
	specs, f := transformDiskMRCollect(t, src)
	count := 0
	for i := range specs {
		if specs[i].name == "M" && specs[i].recvType == "Outer" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("got %d specs for (M, Outer), want 1", count)
	}
	n := 0
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "__Outer_MPair" {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("got %d __Outer_MPair declarations, want 1", n)
	}
}

func TestDiskMREmbeddedCycleTerminates(t *testing.T) {
	// An embedding cycle is invalid Go, but the transform runs before
	// type-checking — it must terminate regardless. (Green-green guard.)
	src := `package p

type A interface {
	B
	M() (int, error)
}

type B interface {
	A
}
`
	specs, _ := transformDiskMRCollect(t, src)
	if sp := specFor(specs, "M", "B"); sp == nil {
		t.Fatalf("no spec for promoted (M, B)")
	}
}
