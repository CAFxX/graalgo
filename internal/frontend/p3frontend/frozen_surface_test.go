package p3frontend

// P1-08 (review 2026-11-04) — the p3frontend frozen synthetic surface vs
// the surface the main frontend actually serves.
//
// The main frontend retired sync/sync/atomic in 503608ea: they now resolve
// from disk (real GOROOT packages) via the general disk importer
// (internal/frontend/export.go:182-190), while p3frontend still serves
// its own frozen copies (importer.go: the per-package switch, plus
// syntheticRuntimePackage / syntheticSyncPackage /
// syntheticAtomicPackage). The main frontend's synthetic runtime has
// likewise grown far beyond p3frontend's four functions. The two frozen
// APIs therefore cannot both be "exact gc 1.27.1" — and they are not:
//
//   - p3 sync serves only Mutex/RWMutex/WaitGroup/Once; gc sync also has
//     Cond, Locker, Map, Pool, NewCond, OnceFunc, OnceValue, OnceValues.
//     A local package using sync.Pool (or sync.Locker as a type) fails
//     p3frontend's type-check even though the main frontend accepts it.
//   - p3 sync/atomic serves only six wrappers and the word/bool-free
//     package-level Load/Store/Add/Swap/CompareAndSwap; gc also has the
//     generic Pointer[T]/Value types, package-level And*/Or*, and
//     LoadPointer/StorePointer/SwapPointer/CompareAndSwapPointer.
//   - p3 runtime serves only Goexit/SetFinalizer/KeepAlive/GC; the main
//     frontend's synthetic runtime adds GOARCH/GOOS, NumCPU, GOMAXPROCS,
//     NumGoroutine, Gosched, the Caller/CallersFrames family, and the
//     PKG-038 documented API.
//   - p3 fmt serves only the seven D4 functions; gc fmt is far larger.
//   - In the other direction, p3 sync/atomic USED TO invent package-level
//     bool atomics (LoadBool/StoreBool/SwapBool/CompareAndSwapBool) that
//     gc does not have at all — the main frontend rejects them as
//     undefined (TestAtomicBoolPackageFuncRejected in the main module).
//     Those four were deleted from importer.go in this lane; the
//     no-invented-names invariant below is their regression test.
//
// Deleting the p3frontend frozen copies outright is NOT safe: p3frontend
// type-checks local packages against them before flattening, and the
// flattened program is then handed to the frozen exporter, whose own
// surface (synthetic runtime, disk sync/atomic) is what the program
// finally compiles against. So the contract pinned here is deliberately
// the weaker, honest one, and it is enforced mechanically:
//
//   1. Every p3 member is a real gc member with an identical signature
//      (no invented names, no mistyped members).
//   2. The p3 scope is EXACTLY the pinned subset — any silent addition or
//      removal fails.
//   3. The known omissions are pinned as an explicit list, each verified
//      to exist in gc and be absent here — so a future widening of the
//      p3 surface (or a gc change) forces a conscious update of this
//      list instead of silent drift.
//
// The oracle is the gc 1.27.1 source tree via the go/types source
// importer, the same oracle TestSyncMirrorMatchesRealGorootSync uses.
// Because the main frontend resolves sync/sync/atomic from that same
// disk tree, comparing p3 against the oracle IS comparing p3 against the
// main frontend's sync/atomic surface. The main frontend's synthetic
// runtime is in package main and unimportable, so for runtime the
// comparison is p3-vs-gc for the four overlapping members, plus the
// exact-scope pin; the superset relationship to the main synthetic
// runtime is documented in importer.go's header comment.

import (
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"testing"
)

// frozenRealPackage type-checks a real GOROOT package from source with
// the go/types source importer (gc 1.27.1, the pinned toolchain).
func frozenRealPackage(t *testing.T, path string) *types.Package {
	t.Helper()
	pkg, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(path)
	if err != nil {
		t.Fatalf("source-import of real %s package: %v", path, err)
	}
	return pkg
}

// frozenScopeNames returns the sorted exported-or-not scope names of pkg.
// (The p3 mirrors contain only exported names, so callers compare
// against the exported subset of the real package separately.)
func frozenScopeNames(pkg *types.Package) []string {
	names := pkg.Scope().Names()
	sort.Strings(names)
	return names
}

// frozenExportedNames returns the sorted exported scope names of pkg.
func frozenExportedNames(pkg *types.Package) []string {
	var names []string
	for _, n := range pkg.Scope().Names() {
		if ast.IsExported(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// frozenAssertScope is the exact-scope pin: mirror must expose exactly
// want, no more, no less.
func frozenAssertScope(t *testing.T, pkgName string, mirror *types.Package, want []string) {
	t.Helper()
	want = append([]string{}, want...)
	sort.Strings(want)
	if got := frozenScopeNames(mirror); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s mirror scope = %v, want exactly %v", pkgName, got, want)
	}
}

// frozenAssertNoInventedNames: every mirror scope name must be an
// exported name of the real gc package.
func frozenAssertNoInventedNames(t *testing.T, pkgName string, mirror, real *types.Package) {
	t.Helper()
	realNames := map[string]bool{}
	for _, n := range frozenExportedNames(real) {
		realNames[n] = true
	}
	for _, n := range mirror.Scope().Names() {
		if !realNames[n] {
			t.Errorf("%s mirror invents %q: not exported by gc 1.27.1 %s", pkgName, n, pkgName)
		}
	}
}

// frozenAssertOmissions: each name in want must exist (exported) in the
// real package and be absent from the mirror. A failure means either
// the mirror silently widened (update the scope pin too) or gc changed.
func frozenAssertOmissions(t *testing.T, pkgName string, mirror, real *types.Package, want []string) {
	t.Helper()
	for _, n := range want {
		if real.Scope().Lookup(n) == nil {
			t.Errorf("%s omission %q is not in gc 1.27.1 %s any more: re-derive the omission list", pkgName, n, pkgName)
		}
		if mirror.Scope().Lookup(n) != nil {
			t.Errorf("%s omission %q is now served by the mirror: update the scope pin and this list", pkgName, n)
		}
	}
}

// frozenNormalizeType rewrites a type for comparison: a synthesized
// empty interface (what the mirrors build with
// types.NewInterfaceType(nil, nil)) and gc's predeclared `any` are the
// same type, but types.TypeString spells the former `interface{}` and
// the latter `any`. Mapping empty interfaces to the universe `any`
// makes the spelling agree; non-empty interfaces (e.g. error) are left
// alone.
func frozenNormalizeType(t types.Type) types.Type {
	switch u := t.(type) {
	case *types.Interface:
		if u.NumMethods() == 0 {
			return types.Universe.Lookup("any").Type()
		}
		return t
	case *types.Slice:
		return types.NewSlice(frozenNormalizeType(u.Elem()))
	case *types.Pointer:
		return types.NewPointer(frozenNormalizeType(u.Elem()))
	case *types.Array:
		return types.NewArray(frozenNormalizeType(u.Elem()), u.Len())
	case *types.Map:
		return types.NewMap(frozenNormalizeType(u.Key()), frozenNormalizeType(u.Elem()))
	case *types.Chan:
		return types.NewChan(u.Dir(), frozenNormalizeType(u.Elem()))
	case *types.Signature:
		// Function-typed parameter (e.g. sync.Once.Do's func()): the
		// same canonicalization applies inside it.
		return frozenCanonSig(u)
	default:
		return t
	}
}

// frozenCanonSig canonicalizes a signature for equality: the existing
// normSig normalizer (which strips named-ness from interface types, as
// for RWMutex.RLocker), frozenNormalizeType (empty interface == any),
// plus stripping parameter/result names. Names are not part of signature
// identity in Go (types.Identical ignores them): gc names atomic Load
// results `val` and CompareAndSwap results `swapped`, while the mirror
// leaves them unnamed — same types, same signature. What IS compared
// strictly: parameter/result types in order, receiver type, and
// variadic-ness.
func frozenCanonSig(sig *types.Signature) *types.Signature {
	norm := normSig(sig)
	strip := func(t *types.Tuple) *types.Tuple {
		vars := make([]*types.Var, t.Len())
		for i := range vars {
			v := t.At(i)
			vars[i] = types.NewParam(v.Pos(), v.Pkg(), "", frozenNormalizeType(v.Type()))
		}
		return types.NewTuple(vars...)
	}
	var recv *types.Var
	if r := norm.Recv(); r != nil {
		recv = types.NewParam(r.Pos(), r.Pkg(), "", frozenNormalizeType(r.Type()))
	}
	return types.NewSignatureType(recv, nil, nil,
		strip(norm.Params()), strip(norm.Results()), norm.Variadic())
}

// frozenAssertFuncSigsEqual compares package-level function signatures
// between mirror and real, using frozenCanonSig (see above).
func frozenAssertFuncSigsEqual(t *testing.T, pkgName string, mirror, real *types.Package, names []string) {
	t.Helper()
	qf := func(p *types.Package) string { return p.Name() }
	for _, n := range names {
		mobj := mirror.Scope().Lookup(n)
		robj := real.Scope().Lookup(n)
		if mobj == nil || robj == nil {
			t.Fatalf("%s func %s: mirror=%v real=%v", pkgName, n, mobj, robj)
		}
		mfn, ok := mobj.(*types.Func)
		if !ok {
			t.Fatalf("%s mirror %s is %T, want *types.Func", pkgName, n, mobj)
		}
		rfn, ok := robj.(*types.Func)
		if !ok {
			t.Fatalf("%s real %s is %T, want *types.Func", pkgName, n, robj)
		}
		msig, ok := mfn.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s mirror %s is %T, want *types.Signature", pkgName, n, mfn.Type())
		}
		rsig, ok := rfn.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s real %s is %T, want *types.Signature", pkgName, n, rfn.Type())
		}
		ms, rs := types.TypeString(frozenCanonSig(msig), qf), types.TypeString(frozenCanonSig(rsig), qf)
		if ms != rs {
			t.Errorf("%s.%s signature mismatch:\n  mirror: %s\n  real:   %s", pkgName, n, ms, rs)
		}
	}
}

// frozenAssertMethodSigsEqual compares the exported method set and
// signatures of one named type between mirror and real.
func frozenAssertMethodSigsEqual(t *testing.T, pkgName string, mirrorNamed, realNamed *types.Named) {
	t.Helper()
	qf := func(p *types.Package) string { return p.Name() }
	realByName := map[string]*types.Func{}
	for i := 0; i < realNamed.NumMethods(); i++ {
		if m := realNamed.Method(i); m.Exported() {
			realByName[m.Name()] = m
		}
	}
	mirrorByName := map[string]*types.Func{}
	for i := 0; i < mirrorNamed.NumMethods(); i++ {
		mirrorByName[mirrorNamed.Method(i).Name()] = mirrorNamed.Method(i)
	}
	for name := range realByName {
		if _, ok := mirrorByName[name]; !ok {
			t.Errorf("%s.%s: real gc method %s missing from mirror", pkgName, realNamed.Obj().Name(), name)
		}
	}
	for name := range mirrorByName {
		if _, ok := realByName[name]; !ok {
			t.Errorf("%s.%s: mirror method %s not in real gc package", pkgName, mirrorNamed.Obj().Name(), name)
		}
	}
	for name, rm := range realByName {
		mm, ok := mirrorByName[name]
		if !ok {
			continue
		}
		rsig, ok := rm.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s.%s.%s real is %T", pkgName, realNamed.Obj().Name(), name, rm.Type())
		}
		msig, ok := mm.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s.%s.%s mirror is %T", pkgName, mirrorNamed.Obj().Name(), name, mm.Type())
		}
		rs, ms := types.TypeString(frozenCanonSig(rsig), qf), types.TypeString(frozenCanonSig(msig), qf)
		if rs != ms {
			t.Errorf("%s.%s.%s signature mismatch:\n  mirror: %s\n  real:   %s", pkgName, mirrorNamed.Obj().Name(), name, ms, rs)
		}
	}
}

func TestFrozenRuntimeSubsetMatchesGC(t *testing.T) {
	mirror := syntheticRuntimePackage()
	real := frozenRealPackage(t, "runtime")
	want := []string{"GC", "Goexit", "KeepAlive", "SetFinalizer"}
	frozenAssertScope(t, "runtime", mirror, want)
	frozenAssertNoInventedNames(t, "runtime", mirror, real)
	frozenAssertFuncSigsEqual(t, "runtime", mirror, real, want)
	// The mirror is a 4-function subset of both gc runtime and the main
	// frontend's synthetic runtime; the omissions are too many to list
	// usefully here (GOARCH/GOOS, NumCPU, GOMAXPROCS, Caller family,
	// ...). The exact-scope pin above is what makes the subset loud:
	// any widening must update `want` deliberately.
}

func TestFrozenFmtSubsetMatchesGC(t *testing.T) {
	mirror := syntheticFmtPackage()
	real := frozenRealPackage(t, "fmt")
	want := []string{"Errorf", "Print", "Printf", "Println", "Sprint", "Sprintf", "Sprintln"}
	frozenAssertScope(t, "fmt", mirror, want)
	frozenAssertNoInventedNames(t, "fmt", mirror, real)
	frozenAssertFuncSigsEqual(t, "fmt", mirror, real, want)
}

func TestFrozenSyncSubsetMatchesGC(t *testing.T) {
	mirror := syntheticSyncPackage()
	real := frozenRealPackage(t, "sync")
	// Exact scope: only the four P3-M2b primitives. Note sync.Locker is
	// deliberately absent as a scope name (RWMutex.RLocker's result is an
	// anonymous interface in the mirror) — it is a pinned omission below.
	frozenAssertScope(t, "sync", mirror, []string{"Mutex", "Once", "RWMutex", "WaitGroup"})
	frozenAssertNoInventedNames(t, "sync", mirror, real)
	// Method-level signature equality for the four primitives is pinned
	// by TestSyncMirrorMatchesRealGorootSync; re-assert it here through
	// the shared helper so this file stands alone as the P1-08 pin.
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once"} {
		frozenAssertMethodSigsEqual(t, "sync",
			namedTypeOf(t, mirror, prim), namedTypeOf(t, real, prim))
	}
	// Pinned omissions: real gc sync members the frozen mirror does not
	// serve, so a program using them fails p3frontend's type-check while
	// the main frontend (disk import) accepts it. Kept explicit so the
	// gap is a documented, reviewed fact, not silent drift.
	frozenAssertOmissions(t, "sync", mirror, real, []string{
		"Cond", "Locker", "Map", "Pool",
		"NewCond", "OnceFunc", "OnceValue", "OnceValues",
	})
}

func TestFrozenAtomicSubsetMatchesGC(t *testing.T) {
	mirror := syntheticAtomicPackage()
	real := frozenRealPackage(t, "sync/atomic")
	wrappers := []string{"Bool", "Int32", "Int64", "Uint32", "Uint64", "Uintptr"}
	// Exact scope: six wrappers plus the word package-level functions.
	// (Bool has no package-level functions in gc — see below.)
	wantFuncs := []string{}
	for _, wt := range []string{"Int32", "Int64", "Uint32", "Uint64", "Uintptr"} {
		for _, op := range []string{"Load", "Store", "Add", "Swap", "CompareAndSwap"} {
			wantFuncs = append(wantFuncs, op+wt)
		}
	}
	want := append(append([]string{}, wrappers...), wantFuncs...)
	frozenAssertScope(t, "sync/atomic", mirror, want)
	frozenAssertNoInventedNames(t, "sync/atomic", mirror, real)
	// Regression test for the deleted LoadBool family: gc has no
	// package-level bool atomics, and the mirror must not invent them.
	// (frozenAssertScope already fails if they return; this names the
	// reason, matching main-frontend TestAtomicBoolPackageFuncRejected.)
	for _, invented := range []string{"LoadBool", "StoreBool", "SwapBool", "CompareAndSwapBool"} {
		if real.Scope().Lookup(invented) != nil {
			t.Fatalf("gc 1.27.1 sync/atomic now HAS %s: re-derive this test", invented)
		}
		if mirror.Scope().Lookup(invented) != nil {
			t.Errorf("sync/atomic mirror serves invented %s: gc rejects it as undefined", invented)
		}
	}
	frozenAssertFuncSigsEqual(t, "sync/atomic", mirror, real, wantFuncs)
	for _, w := range wrappers {
		frozenAssertMethodSigsEqual(t, "sync/atomic",
			namedTypeOf(t, mirror, w), namedTypeOf(t, real, w))
	}
	// Pinned omissions: generic wrappers, package-level And/Or, and
	// pointer atomics — all real gc API the frozen mirror does not serve.
	omissions := []string{"Pointer", "Value"}
	for _, wt := range []string{"Int32", "Int64", "Uint32", "Uint64", "Uintptr"} {
		omissions = append(omissions, "And"+wt, "Or"+wt)
	}
	omissions = append(omissions,
		"LoadPointer", "StorePointer", "SwapPointer", "CompareAndSwapPointer")
	frozenAssertOmissions(t, "sync/atomic", mirror, real, omissions)
}
