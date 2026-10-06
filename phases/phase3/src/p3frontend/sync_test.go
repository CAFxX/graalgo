package p3frontend

// P3-M2b (WS D, ADR-0002): p3frontend tests for the synthetic "sync"
// package. The importer reimplements the frozen exporter's synthetic sync
// signatures (it cannot reuse them — unexported and frozen); these tests
// pin the mirror so drift fails fast.

import (
	"go/format"
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
// through ExportDir — loader (synthetic), importer (mirrored
// signatures), flattener (import spec kept), and the frozen gofrontend
// binary (syncnew/syncop nodes in the document).
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
	for _, want := range []string{
		`"kind": "syncnew"`, `"kind": "syncop"`,
		`"op": "Lock"`, `"op": "Unlock"`,
		`"op": "Add"`, `"op": "Done"`, `"op": "Wait"`, `"op": "Do"`,
		`"synctyp": "Mutex"`, `"synctyp": "WaitGroup"`, `"synctyp": "Once"`,
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

// TestSyncScopeErrorParity: out-of-scope sync surface fails through the
// full ExportDir path with the same named M2 scope errors the frozen
// exporter produces (the mirrored signatures must agree).
func TestSyncScopeErrorParity(t *testing.T) {
	cases := map[string]string{
		"m.TryLock()":                   "not in M2 scope",
		"rw.RLocker()":                  "not in M2 scope",
		"type T struct { mu sync.Once }": "not in M2 scope",
	}
	for body, want := range cases {
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
		_, err = ExportDir(dir)
		if err == nil {
			t.Errorf("%s: expected scope error, got success", body)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q does not contain %q", body, err, want)
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
