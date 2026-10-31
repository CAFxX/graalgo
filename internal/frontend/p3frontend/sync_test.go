package p3frontend

// P3-M2b (WS D, ADR-0002): p3frontend tests for the synthetic "sync"
// package. The importer reimplements the frozen exporter's synthetic sync
// signatures (it cannot reuse them — unexported and frozen); these tests
// pin the mirror so drift fails fast.

import (
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSyncSyntheticSignatures pins the mirrored synthetic sync package:
// exactly the four primitives, each method present with a pointer
// receiver and exact gc 1.27.1 parameter/result types.
func TestSyncSyntheticSignatures(t *testing.T) {
	pkg := syntheticSyncPackage()
	if pkg.Name() != "sync" || pkg.Path() != "sync" {
		t.Fatalf("package name/path = %s/%s, want sync/sync", pkg.Name(), pkg.Path())
	}
	sigString := func(sig *types.Signature) string {
		return types.TypeString(sig, func(p *types.Package) string { return p.Name() })
	}
	for prim, specs := range syncMethodSpecs {
		obj := pkg.Scope().Lookup(prim)
		if obj == nil {
			t.Fatalf("sync.%s missing from synthetic package", prim)
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			t.Fatalf("sync.%s is %T, want *types.TypeName", prim, obj)
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			t.Fatalf("sync.%s is %T, want *types.Named", prim, tn.Type())
		}
		if _, ok := named.Underlying().(*types.Struct); !ok {
			t.Fatalf("sync.%s underlying is %T, want *types.Struct", prim, named.Underlying())
		}
		if got := named.NumMethods(); got != len(specs) {
			t.Fatalf("sync.%s has %d methods, want %d", prim, got, len(specs))
		}
		for i, ms := range specs {
			m := named.Method(i)
			if m.Name() != ms.name {
				t.Fatalf("sync.%s method %d = %s, want %s", prim, i, m.Name(), ms.name)
			}
			sig, ok := m.Type().(*types.Signature)
			if !ok {
				t.Fatalf("sync.%s.%s is %T, want *types.Signature", prim, ms.name, m.Type())
			}
			// Pointer receiver, like the real package.
			recv, ok := sig.Recv().Type().(*types.Pointer)
			if !ok {
				t.Fatalf("sync.%s.%s receiver is %T, want *types.Pointer", prim, ms.name, sig.Recv().Type())
			}
			if recv.Elem() != named {
				t.Fatalf("sync.%s.%s receiver points at %v, want %v", prim, ms.name, recv.Elem(), named)
			}
			// Exact parameter/result shape.
			if sig.Params().Len() != len(ms.params) {
				t.Fatalf("sync.%s.%s has %d params, want %d (%s)",
					prim, ms.name, sig.Params().Len(), len(ms.params), sigString(sig))
			}
			for j, ps := range ms.params {
				want := ps[strings.Index(ps, ":")+1:]
				got := types.TypeString(sig.Params().At(j).Type(), nil)
				var wantStr string
				switch want {
				case "int":
					wantStr = "int"
				case "func()":
					wantStr = "func()"
				default:
					t.Fatalf("test bug: unhandled param type %q", want)
				}
				if got != wantStr {
					t.Fatalf("sync.%s.%s param %d = %s, want %s", prim, ms.name, j, got, wantStr)
				}
			}
			if sig.Results().Len() != len(ms.results) {
				t.Fatalf("sync.%s.%s has %d results, want %d (%s)",
					prim, ms.name, sig.Results().Len(), len(ms.results), sigString(sig))
			}
			for j, rs := range ms.results {
				want := rs[strings.Index(rs, ":")+1:]
				got := types.TypeString(sig.Results().At(j).Type(), nil)
				switch want {
				case "bool":
					if got != "bool" {
						t.Fatalf("sync.%s.%s result %d = %s, want bool", prim, ms.name, j, got)
					}
				case "Locker":
					iface, ok := sig.Results().At(j).Type().(*types.Interface)
					if !ok || iface.NumMethods() != 2 {
						t.Fatalf("sync.%s.%s result %d = %v, want Locker interface", prim, ms.name, j, got)
					}
				default:
					t.Fatalf("test bug: unhandled result type %q", want)
				}
			}
		}
	}
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

// TestSyncMirrorMatchesRealGorootSync is the independent oracle for the
// synthetic sync mirror (reviewer P1-1): it type-checks the REAL
// $GOROOT/src/sync package from source with the go/types source importer
// and asserts the mirror's exported method set, pointer receivers, and
// parameter/result types match gc exactly.
//
// TestSyncSyntheticSignatures asserts the mirror against syncMethodSpecs —
// the same table that BUILDS the mirror — so a wrong table passes. This
// test breaks that circularity: it fails if the mirror drifts from gc's
// real signatures in any direction (missing/extra method, changed
// receiver, mistyped parameter or result).
//
// The oracle reads the test toolchain's GOROOT, which this project pins
// to gc 1.27.1 (the same tree the corpus differential runs against).
func TestSyncMirrorMatchesRealGorootSync(t *testing.T) {
	fset := token.NewFileSet()
	real, err := importer.ForCompiler(fset, "source", nil).Import("sync")
	if err != nil {
		t.Fatalf("source-import of real sync package: %v", err)
	}
	qf := func(p *types.Package) string { return p.Name() }
	mirror := syntheticSyncPackage()
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once"} {
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
			m := mirrorNamed.Method(i)
			mirrorByName[m.Name()] = m
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

// TestSyncResolveImportSynthetic: "sync" resolves as synthetic — it must
// NOT hit the stdlib branch (there IS a $GOROOT/src/sync directory; the
// M2 binding is synthetic, not compiled from GOROOT).
func TestSyncResolveImportSynthetic(t *testing.T) {
	l := NewLoader()
	dir := t.TempDir()
	ref, err := l.ResolveImport(dir, "sync")
	if err != nil {
		t.Fatalf("ResolveImport(sync): %v", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("sync resolved as kind %v, want kindSynthetic (must not hit the stdlib branch)", ref.Kind)
	}
	// The stdlib branch would have resolved a directory; synthetic has none.
	if ref.Dir != "" {
		t.Fatalf("sync resolution carries Dir %q, want empty (synthetic)", ref.Dir)
	}
	// And the importer serves the mirrored package for it.
	imp := NewImporter(l, dir, nil)
	pkg, err := imp.Import("sync")
	if err != nil {
		t.Fatalf("Import(sync): %v", err)
	}
	if pkg.Name() != "sync" {
		t.Fatalf("imported package name = %s, want sync", pkg.Name())
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

// TestSyncMethodSetComplete: the mirror exposes exactly the union of the
// M2-supported methods and the defined-but-out-of-scope ones — no more,
// no less (a missing method would be a confusing type error; an extra one
// would silently widen the surface).
func TestSyncMethodSetComplete(t *testing.T) {
	pkg := syntheticSyncPackage()
	var got []string
	for _, prim := range []string{"Mutex", "RWMutex", "WaitGroup", "Once"} {
		named := pkg.Scope().Lookup(prim).(*types.TypeName).Type().(*types.Named)
		for i := 0; i < named.NumMethods(); i++ {
			got = append(got, prim+"."+named.Method(i).Name())
		}
	}
	want := []string{
		"Mutex.Lock", "Mutex.Unlock", "Mutex.TryLock",
		"RWMutex.Lock", "RWMutex.Unlock", "RWMutex.RLock", "RWMutex.RUnlock",
		"RWMutex.TryLock", "RWMutex.TryRLock", "RWMutex.RLocker",
		"WaitGroup.Add", "WaitGroup.Done", "WaitGroup.Wait", "WaitGroup.Go",
		"Once.Do",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("method set = %v, want %v", got, want)
	}
}
