package p3frontend

// P3-M2b (WS D, ADR-0002) origin, RETIRED (review-2026-11-04 NEW-P1-5,
// mirroring the frontend/v1 retirement in commit 503608ea): the
// p3frontend importer no longer serves synthetic "sync"/"sync/atomic"
// packages. Both resolve from GOROOT source via the general disk path,
// so a program sees the real gc surface — sync.Map/Pool/Cond,
// sync.WaitGroup.Go, generic sync/atomic.Pointer/Value — instead of the
// narrow M2/M4 mirror that rejected them. These tests pin the retired
// contract: real packages from the importer, real packages through the
// full ExportDir path, and agreement with gc's own source importer.

import (
	"go/format"
	"go/importer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestSyncRetiredToDisk pins the retirement at the importer level:
//
//   - "sync" and "sync/atomic" resolve as kindStdlib with a GOROOT
//     directory — never as synthetic markers;
//   - Import serves the REAL packages: the members the narrow M2/M4
//     mirror rejected (Map, Pool, Cond, Locker, WaitGroup.Go, generic
//     atomic.Pointer/Value) are present;
//   - the served packages agree with gc's own source importer on the
//     full exported scope (names, signatures, method sets). This is the
//     differential that replaces the old mirror-vs-real oracle: both
//     sides are now the real package, so any drift fails here.
func TestSyncRetiredToDisk(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader()
	for _, path := range []string{"sync", "sync/atomic"} {
		ref, err := l.ResolveImport(dir, path)
		if err != nil {
			t.Fatalf("ResolveImport(%s): %v", path, err)
		}
		if ref.Kind != kindStdlib {
			t.Fatalf("%s resolved as kind %v, want kindStdlib (synthetic retired)", path, ref.Kind)
		}
		if ref.Dir == "" {
			t.Fatalf("%s resolution carries no Dir, want its GOROOT directory", path)
		}
	}

	imp := NewImporter(l, dir, token.NewFileSet())
	syncPkg, err := imp.Import("sync")
	if err != nil {
		t.Fatalf("Import(sync): %v", err)
	}
	atomicPkg, err := imp.Import("sync/atomic")
	if err != nil {
		t.Fatalf("Import(sync/atomic): %v", err)
	}

	// Members the retired mirror rejected (or never had).
	for _, name := range []string{"Map", "Pool", "Cond", "Locker", "Mutex", "RWMutex", "WaitGroup", "Once"} {
		if syncPkg.Scope().Lookup(name) == nil {
			t.Errorf("sync.%s missing from retired-to-disk package", name)
		}
	}
	if wg := namedTypeOf(t, syncPkg, "WaitGroup"); wg != nil {
		found := false
		for i := 0; i < wg.NumMethods(); i++ {
			if wg.Method(i).Name() == "Go" {
				found = true
			}
		}
		if !found {
			t.Error("sync.WaitGroup.Go missing from retired-to-disk package")
		}
	}
	for _, name := range []string{"Pointer", "Value", "Int64", "Uint64", "Bool"} {
		if atomicPkg.Scope().Lookup(name) == nil {
			t.Errorf("sync/atomic.%s missing from retired-to-disk package", name)
		}
	}
	// Generic additions of the real package: Pointer[T]/Value's
	// predecessors in the mirror were absent entirely.
	for _, name := range []string{"Pointer", "Value"} {
		named := namedTypeOf(t, atomicPkg, name)
		if name == "Pointer" && named.TypeParams().Len() != 1 {
			t.Errorf("sync/atomic.Pointer has %d type params, want 1", named.TypeParams().Len())
		}
	}

	// Differential vs gc's source importer, per package.
	qf := func(p *types.Package) string { return p.Name() }
	compareWithOracle := func(got *types.Package, path string) {
		t.Helper()
		fset := token.NewFileSet()
		real, err := importer.ForCompiler(fset, "source", nil).Import(path)
		if err != nil {
			t.Fatalf("source-import of real %s package: %v", path, err)
		}
		for _, name := range real.Scope().Names() {
			obj := got.Scope().Lookup(name)
			if obj == nil {
				t.Errorf("%s.%s missing from p3frontend-imported package (oracle has it)", path, name)
				continue
			}
			want := types.TypeString(real.Scope().Lookup(name).Type(), qf)
			if gotStr := types.TypeString(obj.Type(), qf); gotStr != want {
				t.Errorf("%s.%s type mismatch:\n  p3frontend: %s\n  oracle:     %s", path, name, gotStr, want)
			}
		}
		for _, name := range got.Scope().Names() {
			if real.Scope().Lookup(name) == nil {
				t.Errorf("%s.%s present in p3frontend-imported package but absent from oracle", path, name)
			}
		}
		// Method sets of the named types, order-independent.
		for _, name := range real.Scope().Names() {
			realNamed, ok := real.Scope().Lookup(name).Type().(*types.Named)
			if !ok {
				continue
			}
			gotNamed, ok := got.Scope().Lookup(name).Type().(*types.Named)
			if !ok {
				t.Errorf("%s.%s is %T, want *types.Named", path, name, got.Scope().Lookup(name).Type())
				continue
			}
			realMethods := map[string]string{}
			for i := 0; i < realNamed.NumMethods(); i++ {
				m := realNamed.Method(i)
				realMethods[m.Name()] = types.TypeString(m.Type(), qf)
			}
			gotMethods := map[string]string{}
			for i := 0; i < gotNamed.NumMethods(); i++ {
				m := gotNamed.Method(i)
				gotMethods[m.Name()] = types.TypeString(m.Type(), qf)
			}
			for mName, want := range realMethods {
				if gotStr, ok := gotMethods[mName]; !ok {
					t.Errorf("%s.%s: oracle method %s missing from p3frontend package", path, name, mName)
				} else if gotStr != want {
					t.Errorf("%s.%s.%s signature mismatch:\n  p3frontend: %s\n  oracle:     %s", path, name, mName, gotStr, want)
				}
			}
			for mName := range gotMethods {
				if _, ok := realMethods[mName]; !ok {
					t.Errorf("%s.%s: method %s present in p3frontend package but absent from oracle", path, name, mName)
				}
			}
		}
	}
	compareWithOracle(syncPkg, "sync")
	compareWithOracle(atomicPkg, "sync/atomic")
}

// TestSyncExportDir: a single-file program importing "sync" compiles
// through ExportDir — loader, importer (real GOROOT sync), flattener
// (import spec kept), and the frozen gofrontend binary.
//
// The contract here is the post-503608ea one: sync resolves from disk
// (real GOROOT package) and its methods compile as ordinary method
// calls, with the runtime semaphore backend via linkname. The syncnew
// node no longer appears for Mutex/WaitGroup/Once (verified: 0
// occurrences in a gofrontend doc for this program; the syncop nodes
// that remain are the sync/atomic wire nodes, not Mutex ops). The
// substring pins are deliberately weak on their own (the doc embeds the
// real sync package bodies); the strong pin is ExportDir success itself
// — the p3frontend gate, the flattener, and the real-sync exporter
// agreeing — plus the runtime differential in TestTwoPackageDifferential-
// style tests via graalgo-sync-run, which exercises sync at runtime.
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

// TestSyncMapWaitGroupGoDifferential is the NEW-P1-5 differential: a
// program using the surface the retired mirror rejected — sync.Map and
// sync.WaitGroup.Go (plus generic sync/atomic.Pointer) — must compile
// through BOTH paths that used to disagree:
//
//   - the p3frontend gate (MergeDir: loader + importer type-check), which
//     previously failed with "undefined: sync.Map", and
//   - the full ExportDir path (merged source through the frozen
//     gofrontend, i.e. frontend/v1's real-sync handling), which already
//     accepted it.
//
// Both paths succeeding is the fix: one program, one type-check outcome.
// The negative pin from the old scope tests is kept in
// TestSyncFormerScopeErrorsNowCompile (an undefined sync method still
// fails).
func TestSyncMapWaitGroupGoDifferential(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var m sync.Map
	m.Store("k", 42)
	if v, ok := m.Load("k"); ok {
		println(v.(int))
	}
	m.Range(func(k, v any) bool {
		println("range")
		return true
	})
	var wg sync.WaitGroup
	wg.Go(func() { println("go") })
	wg.Wait()
	var p atomic.Pointer[string]
	s := "ptr"
	p.Store(&s)
	println(*p.Load())
	var v atomic.Value
	v.Store("val")
	println(v.Load().(string))
}
`
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
		t.Fatal(err)
	}
	// Path 1: the p3frontend gate alone.
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir (p3frontend gate) with sync.Map/WaitGroup.Go/atomic.Pointer: %v", err)
	}
	for _, want := range []string{`"sync"`, `"sync/atomic"`} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged source dropped the import %s spec", want)
		}
	}
	// Path 2: the full ExportDir path (frozen gofrontend = v1 handling).
	if _, err := ExportDir(dir); err != nil {
		t.Fatalf("ExportDir (v1 path) with sync.Map/WaitGroup.Go/atomic.Pointer: %v", err)
	}
}

// TestSyncInLocalDependency: the retired-to-disk sync must also work
// when imported by a flattened local dependency, not just by main —
// the dependency's type-check goes through the same Importer, and the
// flattener must keep the "sync" spec (stdlib is never flattened) while
// renaming the dependency's own identifiers.
func TestSyncInLocalDependency(t *testing.T) {
	dir := t.TempDir()
	depSrc := `package dep

import "sync"

func Get(m *sync.Map) (any, bool) {
	return m.Load("k")
}
`
	mainSrc := `package main

import "example.com/twosync/dep"

import "sync"

func main() {
	var m sync.Map
	m.Store("k", 7)
	if v, ok := dep.Get(&m); ok {
		println(v.(int))
	}
}
`
	for _, f := range []struct {
		path, src string
	}{
		{"go.mod", "module example.com/twosync\n\ngo 1.27\n"},
		{"main.go", mainSrc},
		{"dep/dep.go", depSrc},
	} {
		p := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		formatted, err := format.Source([]byte(f.src))
		if err != nil && !strings.HasSuffix(f.path, "go.mod") {
			t.Fatal(err)
		}
		if strings.HasSuffix(f.path, "go.mod") {
			formatted = []byte(f.src)
		}
		if err := os.WriteFile(p, formatted, 0644); err != nil {
			t.Fatal(err)
		}
	}
	merged, err := MergeDir(dir)
	if err != nil {
		t.Fatalf("MergeDir with sync-importing local dependency: %v", err)
	}
	if !strings.Contains(merged, `"sync"`) {
		t.Error("merged source dropped the import \"sync\" spec")
	}
	if !strings.Contains(merged, "p3i_") {
		t.Error("merged source shows no renamed dependency identifiers (dep was not flattened)")
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
