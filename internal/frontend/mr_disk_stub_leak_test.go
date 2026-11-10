package main

// P1-1 (REVIEW-2026-11-10-07): regression test — disk multi-result stubs
// must never ship in emitted output.
//
// transformMRFunc (mr_disk.go) rewrites each disk multi-result function
// into a pair-returning __-helper plus a stub `func F(...) (T1, T2) {
// panic("unreachable") }` spliced into the rewritten file AST. The stubs
// are kept in the AST for method resolution, so the exporter must never
// emit them: non-generic stubs are skipped by exportFuncDecl (lookupDiskMR)
// and generic stub templates are redirected to the __-helper at every
// instantiation site (newFuncInstantiation / diskMRMethodInstantiation /
// buildMethodTableInst), with exportInstantiation failing loud if a stub
// template ever reaches it. removeDiskMRStubs used to claim to remove
// them after type-checking but had zero callers; it is gone.
//
// These tests export a small hermetic disk package (written to a temp
// GOPATH so it resolves through the normal disk importer) exercised via
// direct call, method value, interface dispatch, and reflection, and
// assert no panic("unreachable") function survives in the wire.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mrStubLeakBoxSrc is the hermetic disk package: a generic Box with a
// multi-result method, a pointer-receiver multi-result method, a
// non-generic multi-result function, and a generic multi-result function.
const mrStubLeakBoxSrc = `package mrbox

type boxError struct{ msg string }

func (e *boxError) Error() string { return e.msg }

type Box[T any] struct{ V T }

func (b Box[T]) Get() (T, error) {
	return b.V, nil
}

func (b *Box[T]) Put(v T) error {
	b.V = v
	return nil
}

func Parse(s string) (int, error) {
	if len(s) == 0 {
		return 0, &boxError{msg: "empty"}
	}
	return len(s), nil
}

func Make[T any](v T) (T, error) {
	return v, nil
}
`

// mrStubLeakProgSrc exercises the disk package through every call shape
// the P1-1 review named: direct call, method value, interface dispatch,
// and reflection.
const mrStubLeakProgSrc = `package main

import (
	"example.com/mrbox"
	"reflect"
)

type Getter interface {
	Get() (int, error)
}

func main() {
	b := mrbox.Box[int]{V: 7}
	v, err := b.Get()
	var g Getter = b
	v2, err2 := g.Get()
	n, err3 := mrbox.Parse("abc")
	m, err4 := mrbox.Make[int](7)
	f := b.Get
	h := mrbox.Parse
	_ = f
	_ = h
	rt := reflect.TypeOf(b)
	_, _ = rt.MethodByName("Get")
	println(v, v2, n, m, err, err2, err3, err4)
}
`

// writeMRStubLeakPkg writes mrStubLeakBoxSrc to a temp GOPATH at
// $GOPATH/src/<importPath>/box.go and points GOPATH there. The import
// path is a parameter so the two tests use distinct paths: the disk
// directory cache is per-process and keyed by import path.
func writeMRStubLeakPkg(t *testing.T, importPath string) {
	t.Helper()
	gopath := filepath.Join(t.TempDir(), "gopath")
	dir := filepath.Join(gopath, "src", filepath.FromSlash(importPath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "box.go"), []byte(mrStubLeakBoxSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
}

// namesReferencedIn walks a wire subtree and returns every function name
// it references, either as an ident name or through an obj id resolved
// via the doc's objects table.
func namesReferencedIn(n any, objects []any) map[string]bool {
	out := map[string]bool{}
	var rec func(n any)
	rec = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if name, ok := v["name"].(string); ok && name != "" {
				out[name] = true
			}
			if oidF, ok := v["obj"].(float64); ok {
				oid := int(oidF)
				if oid >= 0 && oid < len(objects) {
					if om, ok := objects[oid].(map[string]any); ok {
						if name, ok := om["name"].(string); ok && name != "" {
							out[name] = true
						}
					}
				}
			}
			for _, c := range v {
				rec(c)
			}
		case []any:
			for _, c := range v {
				rec(c)
			}
		}
	}
	rec(n)
	return out
}

func TestDiskMRStubDoesNotShip(t *testing.T) {
	writeMRStubLeakPkg(t, "example.com/mrbox")
	out, err := ExportFile(writeIfaceProg(t, mrStubLeakProgSrc))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	objects, _ := doc["objects"].([]any)

	// (2) Structural: no funcdecl body contains panic("unreachable").
	// Names the offending funcdecl. This is the red-green discriminator:
	// pre-fix it fails on example.com/mrbox.Box[int].Get.
	for _, f := range findAllKind(doc, "funcdecl") {
		name, _ := fieldsOf(f)["name"].(string)
		raw, merr := json.Marshal(f)
		if merr != nil {
			t.Fatalf("marshal funcdecl %q: %v", name, merr)
		}
		if strings.Contains(string(raw), "unreachable") {
			t.Fatalf("funcdecl %q contains panic(\"unreachable\") in its body", name)
		}
	}

	// (1) Belt-and-braces: the raw wire must not contain the stub's panic
	// string anywhere (funcdecls, globals, funcvals).
	if strings.Contains(out, "unreachable") {
		t.Fatal("wire contains \"unreachable\" — a multi-result stub shipped")
	}

	// (3) No stub-named funcdecl survives in the mrbox package: a bare
	// Get/Put/Make/Parse (as opposed to the __Box_Get/__Box_Put/__Make/
	// __Parse helpers). Strip type-arg segments first: a method
	// instantiation renders as example.com/mrbox.Box[int].Get, with the
	// args in the middle, so truncating at the first "[" would hide it.
	stubNameRe := regexp.MustCompile(`\[[^\]]*\]`)
	for _, f := range findAllKind(doc, "funcdecl") {
		name, _ := fieldsOf(f)["name"].(string)
		if !strings.HasPrefix(name, "example.com/mrbox") {
			continue
		}
		base := stubNameRe.ReplaceAllString(name, "")
		switch {
		case strings.HasSuffix(base, ".Get") && !strings.Contains(base, "__Box_Get"):
			t.Fatalf("stub funcdecl %q shipped (want only the __Box_Get helper)", name)
		case strings.HasSuffix(base, ".Put") && !strings.Contains(base, "__Box_Put"):
			t.Fatalf("stub funcdecl %q shipped (want only the __Box_Put helper)", name)
		case strings.HasSuffix(base, ".Make") && !strings.Contains(base, "__Make"):
			t.Fatalf("stub funcdecl %q shipped (want only the __Make helper)", name)
		case strings.HasSuffix(base, ".Parse") && !strings.Contains(base, "__Parse"):
			t.Fatalf("stub funcdecl %q shipped (want only the __Parse helper)", name)
		}
	}

	// (4) The __-helpers ARE present (non-tautology: the assertions above
	// would also pass if multi-result desugar silently dropped the calls).
	names := funcNames(doc)
	for _, want := range []string{"__Box_Get", "__Parse", "__Make"} {
		found := false
		for n := range names {
			if strings.Contains(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no funcdecl for helper %q in wire (names: %v)", want, names)
		}
	}

	// (5) main calls all three helpers (direct call, interface dispatch,
	// and direct generic-func call all desugar to the helpers).
	var mainDecl map[string]any
	for _, f := range findAllKind(doc, "funcdecl") {
		if name, _ := fieldsOf(f)["name"].(string); name == "main" || name == "main.main" {
			mainDecl = f
			break
		}
	}
	if mainDecl == nil {
		t.Fatal("no main.main funcdecl in wire")
	}
	called := namesReferencedIn(mainDecl, objects)
	for _, want := range []string{"__Box_Get", "__Parse", "__Make"} {
		found := false
		for n := range called {
			if strings.Contains(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("main never calls helper %q (referenced: %v)", want, called)
		}
	}

	// (6) The Box[int] method table maps Get to the helper instantiation,
	// not the stub: interface dispatch must target the helper.
	entries := typeEntries(t, doc)
	_, boxDetail := findNamedEntry(t, entries, "example.com/mrbox.Box[int]")
	methods, _ := boxDetail["methods"].([]any)
	var getObj float64 = -1
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		if mm["name"] == "Get" {
			getObj, _ = mm["obj"].(float64)
		}
	}
	if getObj < 0 {
		t.Fatalf("Box[int] method table has no Get entry (methods: %v)", methods)
	}
	if int(getObj) >= len(objects) {
		t.Fatalf("Box[int] Get obj %v out of range (objects=%d)", getObj, len(objects))
	}
	obj, _ := objects[int(getObj)].(map[string]any)
	if obj["kind"] != "inst" {
		t.Fatalf("Box[int] Get obj kind = %v, want inst", obj["kind"])
	}
	if oname, _ := obj["name"].(string); !strings.Contains(oname, "__Box_Get") {
		t.Fatalf("Box[int] Get obj = %q, want the __Box_Get helper instantiation", oname)
	}
}

func TestDiskMRStubLeakReflection(t *testing.T) {
	writeMRStubLeakPkg(t, "example.com/mrbox2")
	src := `package main

import (
	"example.com/mrbox2"
	"reflect"
)

func main() {
	b := mrbox.Box[int]{V: 7}
	rt := reflect.TypeOf(b)
	m, ok := rt.MethodByName("Get")
	println(m.Name, ok)
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if strings.Contains(out, "unreachable") {
		t.Fatal("wire contains \"unreachable\" — a multi-result stub shipped")
	}
}
