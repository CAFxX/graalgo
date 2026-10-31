package main

// Tests for the disk multi-result transform's required-intrinsic
// recording (mr_disk.go, review-bytes-mr lane).
//
// A bodyless disk function (e.g. a //go:noescape assembly stub) has no
// Go body to rewrite, so its __-helper was a panic body — and call sites
// desugared to it, so a program calling it built cleanly and then
// panicked at guest runtime. The fix records the backend intrinsic the
// function requires (spec.requiresIntrinsic) and refuses to plan calls
// to it at export time unless a provider is visible, so the call fails
// at export instead.
//
// Covered here:
//  1. transformDiskMultiResult sets requiresIntrinsic for a bodyless
//     function/method with no //go:linkname binding, and leaves it empty
//     for real-bodied and linkname-bound declarations;
//  2. diskMRIntrinsicProvided distinguishes a visible provider from an
//     absent one;
//  3. classifyDiskMultiResultCall returns no plan for an unprovided
//     intrinsic (export-time failure) and a plan when the intrinsic is
//     provided or not required (control: same seeded disk package, so
//     the gate — not package loading — is what differs).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func parseDiskMRFile(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return f
}

func TestDiskMRBodylessRecordsRequiredIntrinsic(t *testing.T) {
	f := parseDiskMRFile(t, `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`)
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if specs[0].requiresIntrinsic != "example.com/p.chanrecv" {
		t.Errorf("requiresIntrinsic = %q, want %q", specs[0].requiresIntrinsic, "example.com/p.chanrecv")
	}

	// Real-bodied functions require nothing.
	f2 := parseDiskMRFile(t, `package p

func Parse(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(b[0]), nil
}
`)
	specs2 := transformDiskMultiResult("example.com/p", []*ast.File{f2})
	if len(specs2) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs2))
	}
	if specs2[0].requiresIntrinsic != "" {
		t.Errorf("requiresIntrinsic = %q, want empty for real-bodied func", specs2[0].requiresIntrinsic)
	}

	// Bodyless methods record the receiver-qualified symbol.
	f3 := parseDiskMRFile(t, `package p

import "unsafe"

type T struct{}

//go:noescape
func (t *T) Foo(x unsafe.Pointer) (int, error)
`)
	specs3 := transformDiskMultiResult("example.com/p", []*ast.File{f3})
	if len(specs3) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs3))
	}
	if specs3[0].requiresIntrinsic != "example.com/p.T.Foo" {
		t.Errorf("requiresIntrinsic = %q, want %q", specs3[0].requiresIntrinsic, "example.com/p.T.Foo")
	}

	// Linkname-bound bodyless declarations require nothing: their
	// helper is linkname-bound, not a panic body.
	f4 := parseDiskMRFile(t, `package p

import "unsafe"

//go:linknamestd myfunc
func myfunc(x unsafe.Pointer) (int, error)
`)
	specs4 := transformDiskMultiResult("example.com/p", []*ast.File{f4})
	if len(specs4) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs4))
	}
	if specs4[0].requiresIntrinsic != "" {
		t.Errorf("requiresIntrinsic = %q, want empty for linkname-bound func", specs4[0].requiresIntrinsic)
	}
}

func TestDiskMRIntrinsicGate(t *testing.T) {
	const pkgPath = "example.com/intrinsic-test-p"
	// Build a minimal disk package in the cache: a pair type for the
	// spec, so classifyDiskMultiResultCall's pair lookup succeeds and
	// only the intrinsic gate can refuse the plan.
	pkg := types.NewPackage(pkgPath, "p")
	f1 := types.NewField(token.NoPos, pkg, "f1", types.Typ[types.Bool], false)
	f2 := types.NewField(token.NoPos, pkg, "f2", types.Typ[types.Bool], false)
	st := types.NewStruct([]*types.Var{f1, f2}, nil)
	tn := types.NewTypeName(token.NoPos, pkg, "__chanrecvPair", nil)
	types.NewNamed(tn, st, nil)
	pkg.Scope().Insert(tn)

	diskMu.Lock()
	oldCache := diskCache
	diskCache = map[string]*syntheticPackage{pkgPath: {pkg: pkg}}
	diskMu.Unlock()
	defer func() {
		diskMu.Lock()
		diskCache = oldCache
		diskMu.Unlock()
	}()

	diskMRMu.Lock()
	oldSpecs := diskMRSpecs
	diskMRSpecs = []diskMRSpec{
		{
			pkgPath:           pkgPath,
			name:              "chanrecv",
			helper:            "__chanrecv",
			pairType:          "__chanrecvPair",
			pairFields:        []string{"f1", "f2"},
			requiresIntrinsic: pkgPath + ".chanrecv",
		},
		{
			pkgPath:    pkgPath,
			name:       "Parse",
			helper:     "__Parse",
			pairType:   "__chanrecvPair",
			pairFields: []string{"f1", "f2"},
		},
	}
	diskMRMu.Unlock()
	defer func() {
		diskMRMu.Lock()
		diskMRSpecs = oldSpecs
		diskMRMu.Unlock()
	}()

	mkFn := func(name string) *types.Func {
		params := types.NewTuple(
			types.NewVar(token.NoPos, pkg, "ch", types.Typ[types.UnsafePointer]),
			types.NewVar(token.NoPos, pkg, "nb", types.Typ[types.Bool]),
		)
		results := types.NewTuple(
			types.NewVar(token.NoPos, pkg, "selected", types.Typ[types.Bool]),
			types.NewVar(token.NoPos, pkg, "received", types.Typ[types.Bool]),
		)
		sig := types.NewSignatureType(nil, nil, nil, params, results, false)
		return types.NewFunc(token.NoPos, pkg, name, sig)
	}

	// Unprovided intrinsic: no plan (export-time failure).
	if plan := (&exporter{}).classifyDiskMultiResultCall(pkgPath, mkFn("chanrecv"), nil); plan != nil {
		t.Fatalf("classifyDiskMultiResultCall returned plan %+v for unprovided intrinsic, want nil", plan)
	}
	// Provided intrinsic (visible in synthFuncIDs): plan.
	provided := &exporter{synthFuncIDs: map[string]*funcDeclInfo{pkgPath + ".chanrecv": {}}}
	plan := provided.classifyDiskMultiResultCall(pkgPath, mkFn("chanrecv"), nil)
	if plan == nil {
		t.Fatal("classifyDiskMultiResultCall returned nil for provided intrinsic, want plan")
	}
	if plan.helperName != "__chanrecv" {
		t.Errorf("plan helperName = %q, want %q", plan.helperName, "__chanrecv")
	}
	// Control: no requirement at all → plan even with no provider.
	if plan := (&exporter{}).classifyDiskMultiResultCall(pkgPath, mkFn("Parse"), nil); plan == nil {
		t.Fatal("classifyDiskMultiResultCall returned nil for requirement-free spec, want plan")
	}

	// diskMRIntrinsicProvided unit behavior.
	e := &exporter{}
	if e.diskMRIntrinsicProvided(&diskMRSpec{requiresIntrinsic: "x.y"}) {
		t.Error("diskMRIntrinsicProvided = true with no synthFuncIDs, want false")
	}
	if !e.diskMRIntrinsicProvided(&diskMRSpec{}) {
		t.Error("diskMRIntrinsicProvided = false for empty requirement, want true")
	}
}
