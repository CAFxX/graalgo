package p3frontend

// p3frontend tests for the "sync" package. The synthetic sync mirror
// (P3-M2b, ADR-0002) was RETIRED with the fmt/sync retirement
// (REVIEW-2026-11-05-11): sync now resolves to the REAL GOROOT package,
// type-checked from source like any other stdlib package. These tests
// pin that contract: the importer serves real sync (full surface,
// including Pool/Map/OnceFunc, which the stub lacked), ResolveImport
// reports kindStdlib at the GOROOT directory, and the served package
// matches the source-imported real sync method-for-method.

import (
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// importerSyncPackage returns the sync package as served by the
// p3frontend Importer: the REAL GOROOT package, type-checked from
// source (the synthetic mirror is retired). Each call re-type-checks
// real sync (~1s); callers that need it twice should share one.
func importerSyncPackage(t *testing.T) *types.Package {
	t.Helper()
	l := NewLoader()
	imp := NewImporter(l, t.TempDir(), token.NewFileSet())
	pkg, err := imp.Import("sync")
	if err != nil {
		t.Fatalf("Import(sync): %v", err)
	}
	return pkg
}

// TestSyncRealSurface pins the real-sync contract: the importer serves
// the FULL gc 1.27.1 sync surface — the stub declared only
// Mutex/RWMutex/WaitGroup/Once, so a program using Pool, Map, Cond or
// OnceFunc failed type-check. Every name below must resolve, with
// pointer receivers on the primitive methods.
func TestSyncRealSurface(t *testing.T) {
	pkg := importerSyncPackage(t)
	if pkg.Name() != "sync" || pkg.Path() != "sync" {
		t.Fatalf("package name/path = %s/%s, want sync/sync", pkg.Name(), pkg.Path())
	}
	// The primitives the stub had — plus the ones it lacked.
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once",
		"Pool", "Map", "Cond"} {
		obj := pkg.Scope().Lookup(prim)
		if obj == nil {
			t.Fatalf("sync.%s missing from the importer-served package", prim)
		}
		if _, ok := obj.Type().(*types.Named); !ok {
			t.Fatalf("sync.%s is %T, want *types.Named", prim, obj.Type())
		}
	}
	// Pool.New field (func() any) and Get/Put methods.
	pool := namedTypeOf(t, pkg, "Pool")
	if st, ok := pool.Underlying().(*types.Struct); !ok {
		t.Fatalf("sync.Pool underlying is %T, want *types.Struct", pool.Underlying())
	} else {
		f, ok := findField(st, "New")
		if !ok {
			t.Fatal("sync.Pool lacks the New field")
		}
		if _, ok := f.Type().Underlying().(*types.Signature); !ok {
			t.Fatalf("sync.Pool.New is %T, want func type", f.Type())
		}
	}
	for _, m := range []string{"Get", "Put"} {
		if !hasMethod(pool, m) {
			t.Fatalf("sync.Pool lacks method %s", m)
		}
	}
	// Map: the full 10-method set (the stub had no Map at all).
	mapNamed := namedTypeOf(t, pkg, "Map")
	for _, m := range []string{"Clear", "CompareAndDelete", "CompareAndSwap",
		"Delete", "Load", "LoadAndDelete", "LoadOrStore", "Range", "Store", "Swap"} {
		if !hasMethod(mapNamed, m) {
			t.Fatalf("sync.Map lacks method %s", m)
		}
	}
	// Cond: L Locker field + Broadcast/Signal/Wait.
	cond := namedTypeOf(t, pkg, "Cond")
	if st, ok := cond.Underlying().(*types.Struct); !ok {
		t.Fatalf("sync.Cond underlying is %T, want *types.Struct", cond.Underlying())
	} else if _, ok := findField(st, "L"); !ok {
		t.Fatal("sync.Cond lacks the L field")
	}
	for _, m := range []string{"Broadcast", "Signal", "Wait"} {
		if !hasMethod(cond, m) {
			t.Fatalf("sync.Cond lacks method %s", m)
		}
	}
	// Locker interface + package-level funcs the stub lacked.
	if obj := pkg.Scope().Lookup("Locker"); obj == nil {
		t.Fatal("sync.Locker missing")
	} else if _, ok := obj.Type().Underlying().(*types.Interface); !ok {
		t.Fatalf("sync.Locker underlying is %T, want *types.Interface", obj.Type().Underlying())
	}
	for _, fn := range []string{"NewCond", "OnceFunc", "OnceValue", "OnceValues"} {
		obj := pkg.Scope().Lookup(fn)
		if obj == nil {
			t.Fatalf("sync.%s missing", fn)
		}
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("sync.%s is %T, want *types.Func", fn, obj)
		}
	}
}

// findField looks up an (exported) struct field by name.
func findField(st *types.Struct, name string) (*types.Var, bool) {
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Name() == name {
			return f, true
		}
	}
	return nil, false
}

// hasMethod reports whether the named type has a method of that name.
func hasMethod(named *types.Named, name string) bool {
	for i := 0; i < named.NumMethods(); i++ {
		if named.Method(i).Name() == name {
			return true
		}
	}
	return false
}

// normIface strips named-ness from interface types for comparison: Go
// interface identity is structural (a named and an unnamed interface with
// the same method set are identical types). The frozen exporter
// deliberately synthesizes RWMutex.RLocker's result as an anonymous
// interface{Lock(); Unlock()} ("just needs to exist for the signature to
// type-check", internal/frontend/export.go — RLocker is out of M2
// scope and never lowers), while gc names it sync.Locker. Normalizing
// both sides keeps the oracle strict on what matters (method sets,
// parameter/result types, receivers) without flagging that documented
// simplification.
func normIface(t types.Type) types.Type {
	if n, ok := t.(*types.Named); ok {
		if _, ok := n.Underlying().(*types.Interface); ok {
			return n.Underlying()
		}
	}
	return t
}

func normSig(sig *types.Signature) *types.Signature {
	params := make([]*types.Var, sig.Params().Len())
	for i := range params {
		p := sig.Params().At(i)
		params[i] = types.NewParam(p.Pos(), p.Pkg(), p.Name(), normIface(p.Type()))
	}
	results := make([]*types.Var, sig.Results().Len())
	for i := range results {
		r := sig.Results().At(i)
		results[i] = types.NewParam(r.Pos(), r.Pkg(), r.Name(), normIface(r.Type()))
	}
	return types.NewSignatureType(sig.Recv(), nil, nil,
		types.NewTuple(params...), types.NewTuple(results...), sig.Variadic())
}

// namedTypeOf looks up a named type in a package scope (test helper).
func namedTypeOf(t *testing.T, pkg *types.Package, name string) *types.Named {
	t.Helper()
	obj := pkg.Scope().Lookup(name)
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("sync.%s is %T, want *types.TypeName", name, obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("sync.%s is %T, want *types.Named", name, tn.Type())
	}
	return named
}

// TestSyncMatchesRealGorootSync is the independent oracle for the
// importer-served sync package: it type-checks the REAL
// $GOROOT/src/sync package from source with the go/types source importer
// and asserts the served package's exported method set, pointer
// receivers, and parameter/result types match gc exactly. Both sides
// are real sync now (the synthetic mirror is retired), so the test
// pins that the importer serves the real package — not a stub, not a
// partial package — and fails if either side drifts.
//
// The oracle reads the test toolchain's GOROOT, which this project pins
// to gc 1.27.1 (the same tree the corpus differential runs against).
func TestSyncMatchesRealGorootSync(t *testing.T) {
	fset := token.NewFileSet()
	real, err := importer.ForCompiler(fset, "source", nil).Import("sync")
	if err != nil {
		t.Fatalf("source-import of real sync package: %v", err)
	}
	qf := func(p *types.Package) string { return p.Name() }
	mirror := importerSyncPackage(t)
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once", "Pool", "Map", "Cond"} {
		realNamed := namedTypeOf(t, real, prim)
		mirrorNamed := namedTypeOf(t, mirror, prim)
		var realByName = map[string]*types.Func{}
		for i := 0; i < realNamed.NumMethods(); i++ {
			if m := realNamed.Method(i); m.Exported() {
				realByName[m.Name()] = m
			}
		}
		var mirrorByName = map[string]*types.Func{}
		for i := 0; i < mirrorNamed.NumMethods(); i++ {
			if m := mirrorNamed.Method(i); m.Exported() {
				mirrorByName[m.Name()] = m
			}
		}
		// Same exported method set (order-independent: go/types sorts
		// type-checked methods, AddMethod keeps insertion order).
		for name := range realByName {
			if _, ok := mirrorByName[name]; !ok {
				t.Fatalf("sync.%s: real gc method %s missing from mirror", prim, name)
			}
		}
		for name := range mirrorByName {
			if _, ok := realByName[name]; !ok {
				t.Fatalf("sync.%s: mirror method %s not in real gc package", prim, name)
			}
		}
		for name, rm := range realByName {
			mm := mirrorByName[name]
			rsig, ok := rm.Type().(*types.Signature)
			if !ok {
				t.Fatalf("real sync.%s.%s is %T, want *types.Signature", prim, rm.Name(), rm.Type())
			}
			msig, ok := mm.Type().(*types.Signature)
			if !ok {
				t.Fatalf("mirror sync.%s.%s is %T, want *types.Signature", prim, mm.Name(), mm.Type())
			}
			// Pointer receiver to the primitive, on both sides.
			if _, ok := rsig.Recv().Type().(*types.Pointer); !ok {
				t.Fatalf("real sync.%s.%s receiver is %v, want pointer", prim, rm.Name(), rsig.Recv().Type())
			}
			if rrecv, ok := msig.Recv().Type().(*types.Pointer); !ok || rrecv.Elem() != mirrorNamed {
				t.Fatalf("mirror sync.%s.%s receiver = %v, want *%s",
					prim, mm.Name(), msig.Recv().Type(), prim)
			}
			rs, ms := types.TypeString(normSig(rsig), qf), types.TypeString(normSig(msig), qf)
			if rs != ms {
				t.Fatalf("sync.%s.%s signature mismatch:\n  real:   %s\n  mirror: %s",
					prim, rm.Name(), rs, ms)
			}
		}
	}
}

// TestSyncResolveImportStdlib: "sync" resolves as stdlib — the synthetic
// mirror is retired, so it MUST hit the stdlib branch (there IS a
// $GOROOT/src/sync directory) and the importer must serve the real
// package from it.
func TestSyncResolveImportStdlib(t *testing.T) {
	l := NewLoader()
	dir := t.TempDir()
	ref, err := l.ResolveImport(dir, "sync")
	if err != nil {
		t.Fatalf("ResolveImport(sync): %v", err)
	}
	if ref.Kind != kindStdlib {
		t.Fatalf("sync resolved as kind %v, want kindStdlib (synthetic mirror retired)", ref.Kind)
	}
	wantDir := filepath.Join(l.GOROOT(), "src", "sync")
	if ref.Dir != wantDir {
		t.Fatalf("sync resolved at %s, want %s", ref.Dir, wantDir)
	}
	// And the importer serves the real package for it.
	imp := NewImporter(l, dir, token.NewFileSet())
	pkg, err := imp.Import("sync")
	if err != nil {
		t.Fatalf("Import(sync): %v", err)
	}
	if pkg.Name() != "sync" {
		t.Fatalf("imported package name = %s, want sync", pkg.Name())
	}
	if pkg.Scope().Lookup("Pool") == nil || pkg.Scope().Lookup("Map") == nil {
		t.Fatal("importer-served sync lacks Pool/Map: not the real package")
	}
}

// TestSyncExportDir: a single-file program importing "sync" compiles
// through ExportDir — loader, importer (mirrored signatures),
// flattener (import spec kept), and the frozen gofrontend binary.
//
// VERDICT (review-2026-10-31 P1-1, re-scoped contract, not a fixed
// bug): the old test asserted the M2 synthetic-sync document shape
// (syncnew/syncop nodes, `"synctyp": "Mutex"`). That contract is gone:
// commit 503608ea RETIRED the synthetic sync/sync-atomic packages in the
// product frontend — sync now resolves from disk (real GOROOT package)
// and its methods compile as ordinary method calls, with the runtime
// semaphore backend via linkname. The syncnew node no longer appears
// for Mutex/WaitGroup/Once (verified: 0 occurrences in a gofrontend doc
// for this program; the syncop nodes that remain are the sync/atomic
// wire nodes, not Mutex ops). So this test now pins the real-sync
// contract: ExportDir succeeds, the document is for package main, it
// names the sync primitives and their methods, and the merged source
// keeps the import spec. The substring pins are deliberately weak on
// their own (the doc embeds the real sync package bodies); the strong
// pin is ExportDir success itself — the p3frontend mirror, the
// flattener, and the real-sync exporter agreeing — plus the runtime
// differential in TestTwoPackageDifferential-style tests via
// graalgo-sync-run, which exercises sync at runtime.
func TestSyncExportDir(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "sync"

func main() {
	var m sync.Mutex
	m.Lock()
	m.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Wait()
	var o sync.Once
	o.Do(func() { println("once") })
}
`
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
		t.Fatal(err)
	}
	doc, err := ExportDir(dir)
	if err != nil {
		t.Fatalf("ExportDir with import \"sync\": %v", err)
	}
	if strings.Contains(doc, `"kind": "syncnew"`) {
		t.Error("ExportDir doc still contains syncnew nodes: synthetic sync was retired (503608ea), Mutex/WaitGroup/Once must compile as ordinary calls")
	}
	// Named types render with their package path in the document
	// ("sync.Mutex", not bare "Mutex").
	for _, want := range []string{
		`"name": "sync.Mutex"`, `"name": "sync.WaitGroup"`, `"name": "sync.Once"`,
		`"name": "Lock"`, `"name": "Unlock"`,
		`"name": "Add"`, `"name": "Done"`, `"name": "Wait"`, `"name": "Do"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("ExportDir doc missing %s", want)
		}
	}
	// The merged source kept the import spec (flatten.go kept-import path).
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	if !strings.Contains(merged, `"sync"`) {
		t.Error("merged source dropped the import \"sync\" spec")
	}
}

// TestSyncFormerScopeErrorsNowCompile: formerly TestSyncScopeErrorParity.
//
// VERDICT (review-2026-10-31 P1-1, re-scoped contract, not a fixed
// bug): the old test asserted that TryLock, RLocker, and a struct field
// of type sync.Once fail ExportDir with "not in M2 scope" — the M2
// synthetic-sync scope contract. That contract is gone with the 503608ea
// retirement: sync is a real GOROOT package now, so all three are
// ordinary, compilable Go (TryLock since Go 1.18, RLocker, Once as a
// field). Asserting the old scope errors would pin a dead contract.
// This test therefore asserts the new contract: each former scope-error
// case now compiles through the full ExportDir path. The test keeps a
// negative pin so it can still fail: a genuinely undefined sync method
// must still fail ExportDir with a type error.
func TestSyncFormerScopeErrorsNowCompile(t *testing.T) {
	cases := []string{
		"m.TryLock()",
		"rw.RLocker()",
		"type T struct { mu sync.Once }",
	}
	for _, body := range cases {
		dir := t.TempDir()
		var src string
		if strings.HasPrefix(body, "type ") {
			src = "package main\n\nimport \"sync\"\n\n" + body + "\n\nfunc main() {}\n"
		} else {
			src = "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tvar rw sync.RWMutex\n\t_ = m\n\t_ = rw\n\t" + body + "\n}\n"
		}
		formatted, err := format.Source([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ExportDir(dir); err != nil {
			t.Errorf("%s: expected success under real sync, got error: %v", body, err)
		}
	}

	// Negative pin: an undefined sync method is still a type error,
	// not a silent success.
	dir := t.TempDir()
	src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tm.NoSuchMethod()\n}\n"
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportDir(dir); err == nil {
		t.Error("m.NoSuchMethod(): expected a type error, got success")
	} else {
		t.Logf("undefined sync method correctly rejected: %v", err)
	}
}

// TestSyncMethodSetComplete: the importer-served sync package exposes
// exactly the real gc 1.27.1 method set for every primitive — no more,
// no less (a missing method would be a confusing type error; an extra
// one would silently widen the surface). Pool/Map/Cond are the
// regression pin: the retired stub had none of them.
func TestSyncMethodSetComplete(t *testing.T) {
	pkg := importerSyncPackage(t)
	var got []string
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once", "Pool", "Map", "Cond"} {
		named := pkg.Scope().Lookup(prim).(*types.TypeName).Type().(*types.Named)
		for i := 0; i < named.NumMethods(); i++ {
			if m := named.Method(i); m.Exported() {
				got = append(got, prim+"."+m.Name())
			}
		}
	}
	want := []string{
		"Mutex.Lock", "Mutex.Unlock", "Mutex.TryLock",
		"RWMutex.Lock", "RWMutex.Unlock", "RWMutex.RLock", "RWMutex.RUnlock",
		"RWMutex.TryLock", "RWMutex.TryRLock", "RWMutex.RLocker",
		"WaitGroup.Add", "WaitGroup.Done", "WaitGroup.Wait", "WaitGroup.Go",
		"Once.Do",
		"Pool.Get", "Pool.Put",
		"Map.Clear", "Map.CompareAndDelete", "Map.CompareAndSwap",
		"Map.Delete", "Map.Load", "Map.LoadAndDelete", "Map.LoadOrStore",
		"Map.Range", "Map.Store", "Map.Swap",
		"Cond.Broadcast", "Cond.Signal", "Cond.Wait",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method set = %v, want %v", got, want)
	}
}
