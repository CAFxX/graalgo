package p3frontend

// p3frontend tests for the "sync" package. The frozen exporter retired its
// synthetic sync lowering in 503608ea (real-sync2: sync and sync/atomic
// now compile from real GOROOT disk sources); the p3frontend importer
// keeps a mirrored signature table for type-checking only (it cannot reuse
// the frozen exporter's — unexported and frozen). The mirror tests pin the
// table so drift fails fast; the ExportDir tests below pin the real-sync
// end-to-end path (loader + importer + flattener + frozen exporter).

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
// through ExportDir — loader (synthetic-kind marker), importer (mirrored
// signatures for type-checking), flattener (import spec kept), and the
// frozen gofrontend binary, which since 503608ea (real-sync2) compiles the
// REAL sync package from disk instead of the retired synthetic lowering.
// Calls are real method dispatches (methodref), not syncnew/syncop nodes.
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
	// The real sync package compiled through: its types are in the doc and
	// the calls are real method dispatches.
	for _, want := range []string{
		`sync.Mutex`, `sync.WaitGroup`, `sync.Once`,
		`"kind": "methodref"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("ExportDir doc missing %s (real sync package did not compile through)", want)
		}
	}
	// The synthetic lowering is retired: no syncnew node may appear.
	if strings.Contains(doc, `"kind": "syncnew"`) {
		t.Error("ExportDir doc contains a syncnew node (retired by real-sync2, 503608ea)")
	}
	// The merged source kept the import spec (flatten.go kept-import path).
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir: %v", err)
	}
	if !strings.Contains(merged, `"sync"`) {
		t.Error("merged source dropped the import \"sync\" spec")
	}
	// Parity: the p3frontend path agrees byte-for-byte with the frozen
	// exporter run directly on the merged source.
	mergedPath := filepath.Join(t.TempDir(), "merged.go")
	if err := os.WriteFile(mergedPath, []byte(merged), 0644); err != nil {
		t.Fatal(err)
	}
	want, err := runGofrontend(mergedPath)
	if err != nil {
		t.Fatalf("runGofrontend: %v", err)
	}
	if !reflect.DeepEqual(normalizeDoc(t, want), normalizeDoc(t, doc)) {
		t.Error("ExportDir doc != direct gofrontend export of merged source")
	}
}

// TestSyncExtendedSurfaceExportParity: the sync members that were
// out-of-M2-scope under the retired synthetic lowering (TryLock,
// TryRLock, RLocker, WaitGroup.Go, embedded sync.Once) compile through the
// full ExportDir path now that the frozen exporter builds the real sync
// package from disk — and the p3frontend path agrees byte-for-byte with
// the frozen exporter run directly (the parity property the old
// scope-error test pinned, under the new mechanism).
func TestSyncExtendedSurfaceExportParity(t *testing.T) {
	cases := map[string]struct {
		src  string
		mark string // doc marker proving the member compiled through
	}{
		"trylock": {`package main

import "sync"

func main() {
	var m sync.Mutex
	println(m.TryLock())
	var rw sync.RWMutex
	println(rw.TryRLock())
	_ = rw.RLocker()
}
`, `"name": "TryLock"`},
		"waitgroup_go": {`package main

import "sync"

func main() {
	var wg sync.WaitGroup
	wg.Go(func() {})
	wg.Wait()
}
`, `"name": "Go"`},
		"embedded_once": {`package main

import "sync"

type T struct{ mu sync.Once }

func main() {
	var t T
	t.mu.Do(func() { println("once") })
}
`, `sync.Once`},
	}
	for name, tc := range cases {
		dir := t.TempDir()
		formatted, err := format.Source([]byte(tc.src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
			t.Fatal(err)
		}
		doc, err := ExportDir(dir)
		if err != nil {
			t.Errorf("%s: ExportDir failed, want success (real sync compiles): %v", name, err)
			continue
		}
		if !strings.Contains(doc, tc.mark) {
			t.Errorf("%s: ExportDir doc missing %s", name, tc.mark)
		}
		merged, err := MergeDir(dir)
		if err != nil {
			t.Fatalf("%s: MergeDir: %v", name, err)
		}
		mergedPath := filepath.Join(t.TempDir(), "merged.go")
		if err := os.WriteFile(mergedPath, []byte(merged), 0644); err != nil {
			t.Fatal(err)
		}
		want, err := runGofrontend(mergedPath)
		if err != nil {
			t.Fatalf("%s: runGofrontend: %v", name, err)
		}
		if !reflect.DeepEqual(normalizeDoc(t, want), normalizeDoc(t, doc)) {
			t.Errorf("%s: ExportDir doc != direct gofrontend export of merged source", name)
		}
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
