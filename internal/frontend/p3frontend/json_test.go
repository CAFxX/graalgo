package p3frontend

// p3frontend tests for the "encoding/json" package. The synthetic json
// mirror (P3-M7 wave 1, synth_json.go) was RETIRED with the
// review-08-synth-json retirement (R08-S-01): encoding/json now
// resolves to the REAL GOROOT package, type-checked from source like
// any other stdlib package. These tests pin that contract: the
// importer serves real json (full surface, including Unmarshal,
// Decoder.Decode and Marshal on named structs, which the stub
// fail-fast panicked on), ResolveImport reports kindStdlib at the
// GOROOT directory, and the served package matches the source-imported
// real json method-for-method. (Pattern: sync_test.go, which pins the
// same contract for the retired fmt/sync mirrors.)

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

// importerJsonPackage returns the encoding/json package as served by
// the p3frontend Importer: the REAL GOROOT package, type-checked from
// source (the synthetic mirror is retired). Each call re-type-checks
// real json (~1s); callers that need it twice should share one.
func importerJsonPackage(t *testing.T) *types.Package {
	t.Helper()
	l := NewLoader()
	imp := NewImporter(l, t.TempDir(), token.NewFileSet())
	pkg, err := imp.Import("encoding/json")
	if err != nil {
		t.Fatalf("Import(encoding/json): %v", err)
	}
	return pkg
}

// TestJsonRealSurface pins the real-json contract: the importer serves
// the FULL gc 1.27.1 encoding/json surface — the stub declared only
// the any-model subset and fail-fast panicked on Unmarshal,
// Decoder.Decode, RawMessage.UnmarshalJSON and Marshal of any named
// struct type, so a program using any of those failed at runtime.
// Every name below must resolve.
func TestJsonRealSurface(t *testing.T) {
	pkg := importerJsonPackage(t)
	if pkg.Name() != "json" || pkg.Path() != "encoding/json" {
		t.Fatalf("package name/path = %s/%s, want json/encoding/json", pkg.Name(), pkg.Path())
	}
	// Package-level funcs the stub lacked (Unmarshal/Decode entry
	// points) plus the ones it had.
	for _, fn := range []string{"Marshal", "MarshalIndent", "Unmarshal",
		"NewDecoder", "NewEncoder", "Valid", "Compact", "Indent",
		"HTMLEscape", "AppendCompact", "AppendIndent"} {
		obj := pkg.Scope().Lookup(fn)
		if obj == nil {
			t.Fatalf("json.%s missing from the importer-served package", fn)
		}
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("json.%s is %T, want *types.Func", fn, obj)
		}
	}
	// Named types (the stub had these, but with fail-fast methods).
	for _, typ := range []string{"Decoder", "Encoder", "Marshaler",
		"Unmarshaler", "RawMessage", "Number", "Delim", "Token"} {
		obj := pkg.Scope().Lookup(typ)
		if obj == nil {
			t.Fatalf("json.%s missing from the importer-served package", typ)
		}
		if _, ok := obj.(*types.TypeName); !ok {
			t.Fatalf("json.%s is %T, want *types.TypeName", typ, obj)
		}
	}
	// Error types (the stub returned UnsupportedTypeError with a nil
	// Type; the real package populates it — see R08-S-09).
	for _, typ := range []string{"SyntaxError", "UnsupportedTypeError",
		"UnsupportedValueError", "InvalidUnmarshalError",
		"UnmarshalTypeError", "MarshalerError"} {
		obj := pkg.Scope().Lookup(typ)
		if obj == nil {
			t.Fatalf("json.%s missing from the importer-served package", typ)
		}
		if _, ok := obj.Type().(*types.Named); !ok {
			t.Fatalf("json.%s is %T, want *types.Named", typ, obj.Type())
		}
	}
	// Decoder: Decode/Token/More/Buffered/InputOffset (the stub's
	// Token/RawToken/InputPos panicked on the WS D trigger).
	dec := namedTypeOf(t, pkg, "Decoder")
	for _, m := range []string{"Decode", "Token", "More", "Buffered", "InputOffset"} {
		if !hasMethod(dec, m) {
			t.Fatalf("json.Decoder lacks method %s", m)
		}
	}
	// Encoder: Encode/SetIndent/SetEscapeHTML.
	enc := namedTypeOf(t, pkg, "Encoder")
	for _, m := range []string{"Encode", "SetIndent", "SetEscapeHTML"} {
		if !hasMethod(enc, m) {
			t.Fatalf("json.Encoder lacks method %s", m)
		}
	}
	// Number: String/Float64/Int64; RawMessage: MarshalJSON/UnmarshalJSON.
	num := namedTypeOf(t, pkg, "Number")
	for _, m := range []string{"String", "Float64", "Int64"} {
		if !hasMethod(num, m) {
			t.Fatalf("json.Number lacks method %s", m)
		}
	}
	rm := namedTypeOf(t, pkg, "RawMessage")
	for _, m := range []string{"MarshalJSON", "UnmarshalJSON"} {
		if !hasMethod(rm, m) {
			t.Fatalf("json.RawMessage lacks method %s", m)
		}
	}
	// Delim is a named string type; Token is an interface type.
	delim := namedTypeOf(t, pkg, "Delim")
	if b, ok := delim.Underlying().(*types.Basic); !ok || b.Kind() != types.String {
		t.Fatalf("json.Delim underlying is %v, want string", delim.Underlying())
	}
	tok := namedTypeOf(t, pkg, "Token")
	if _, ok := tok.Underlying().(*types.Interface); !ok {
		t.Fatalf("json.Token underlying is %T, want *types.Interface", tok.Underlying())
	}
	// Marshaler/Unmarshaler are interfaces with exactly one method each.
	for _, iface := range []string{"Marshaler", "Unmarshaler"} {
		named := namedTypeOf(t, pkg, iface)
		u, ok := named.Underlying().(*types.Interface)
		if !ok {
			t.Fatalf("json.%s underlying is %T, want *types.Interface", iface, named.Underlying())
		}
		if u.NumMethods() != 1 {
			t.Fatalf("json.%s has %d methods, want 1", iface, u.NumMethods())
		}
	}
}

// TestJsonMatchesRealGorootJson is the independent oracle for the
// importer-served json package: it type-checks the REAL
// $GOROOT/src/encoding/json package from source with the go/types
// source importer and asserts the served package's exported method
// sets and parameter/result types match gc exactly. Both sides are
// real json now (the synthetic mirror is retired), so the test pins
// that the importer serves the real package — not a stub, not a
// partial package — and fails if either side drifts.
//
// The oracle reads the test toolchain's GOROOT, which this project
// pins to gc 1.27.1 (the same tree the corpus differential runs
// against).
func TestJsonMatchesRealGorootJson(t *testing.T) {
	fset := token.NewFileSet()
	real, err := importer.ForCompiler(fset, "source", nil).Import("encoding/json")
	if err != nil {
		t.Fatalf("source-import of real encoding/json package: %v", err)
	}
	qf := func(p *types.Package) string { return p.Name() }
	mirror := importerJsonPackage(t)
	for _, typ := range []string{"Decoder", "Encoder", "Number", "RawMessage"} {
		realNamed := namedTypeOf(t, real, typ)
		mirrorNamed := namedTypeOf(t, mirror, typ)
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
				t.Fatalf("json.%s: real gc method %s missing from mirror", typ, name)
			}
		}
		for name := range mirrorByName {
			if _, ok := realByName[name]; !ok {
				t.Fatalf("json.%s: mirror method %s not in real gc package", typ, name)
			}
		}
		for name, rm := range realByName {
			mm := mirrorByName[name]
			rsig, ok := rm.Type().(*types.Signature)
			if !ok {
				t.Fatalf("real json.%s.%s is %T, want *types.Signature", typ, rm.Name(), rm.Type())
			}
			msig, ok := mm.Type().(*types.Signature)
			if !ok {
				t.Fatalf("mirror json.%s.%s is %T, want *types.Signature", typ, mm.Name(), mm.Type())
			}
			// Pointer receiver to the named type, on both sides.
			if _, ok := rsig.Recv().Type().(*types.Pointer); !ok {
				t.Fatalf("real json.%s.%s receiver is %v, want pointer", typ, rm.Name(), rsig.Recv().Type())
			}
			if rrecv, ok := msig.Recv().Type().(*types.Pointer); !ok || rrecv.Elem() != mirrorNamed {
				t.Fatalf("mirror json.%s.%s receiver = %v, want *%s",
					typ, mm.Name(), msig.Recv().Type(), typ)
			}
			rs, ms := types.TypeString(normSig(rsig), qf), types.TypeString(normSig(msig), qf)
			if rs != ms {
				t.Fatalf("json.%s.%s signature mismatch:\n  real:   %s\n  mirror: %s",
					typ, rm.Name(), rs, ms)
			}
		}
	}
	// Package-level funcs: same signatures on both sides.
	for _, fn := range []string{"Marshal", "MarshalIndent", "Unmarshal",
		"Valid", "Compact", "Indent", "HTMLEscape"} {
		robj, ok := real.Scope().Lookup(fn).(*types.Func)
		if !ok {
			t.Fatalf("real json.%s is %T, want *types.Func", fn, real.Scope().Lookup(fn))
		}
		mobj, ok := mirror.Scope().Lookup(fn).(*types.Func)
		if !ok {
			t.Fatalf("mirror json.%s is %T, want *types.Func", fn, mirror.Scope().Lookup(fn))
		}
		rs, ms := types.TypeString(robj.Type(), qf), types.TypeString(mobj.Type(), qf)
		if rs != ms {
			t.Fatalf("json.%s signature mismatch:\n  real:   %s\n  mirror: %s", fn, rs, ms)
		}
	}
}

// TestJsonResolveImportStdlib: "encoding/json" resolves as stdlib —
// the synthetic mirror is retired, so it MUST hit the stdlib branch
// (there IS a $GOROOT/src/encoding/json directory) and the importer
// must serve the real package from it.
func TestJsonResolveImportStdlib(t *testing.T) {
	l := NewLoader()
	dir := t.TempDir()
	ref, err := l.ResolveImport(dir, "encoding/json")
	if err != nil {
		t.Fatalf("ResolveImport(encoding/json): %v", err)
	}
	if ref.Kind != kindStdlib {
		t.Fatalf("encoding/json resolved as kind %v, want kindStdlib (synthetic mirror retired)", ref.Kind)
	}
	wantDir := filepath.Join(l.GOROOT(), "src", "encoding", "json")
	if ref.Dir != wantDir {
		t.Fatalf("encoding/json resolved at %s, want %s", ref.Dir, wantDir)
	}
	// And the importer serves the real package for it.
	imp := NewImporter(l, dir, token.NewFileSet())
	pkg, err := imp.Import("encoding/json")
	if err != nil {
		t.Fatalf("Import(encoding/json): %v", err)
	}
	if pkg.Name() != "json" {
		t.Fatalf("imported package name = %s, want json", pkg.Name())
	}
	if pkg.Scope().Lookup("Unmarshal") == nil || pkg.Scope().Lookup("Decoder") == nil {
		t.Fatal("importer-served encoding/json lacks Unmarshal/Decoder: not the real package")
	}
}

// TestJsonExportDir: a single-file program importing "encoding/json"
// compiles through ExportDir — loader, importer, flattener (import
// spec kept), and the frozen gofrontend binary.
//
// VERDICT (review-08-synth-json, R08-S-01, re-scoped contract, not a
// fixed bug): the old json_test.go asserted the synthetic mirror's
// surface (drift test against synth_json.go, plus the
// syntheticJsonPackage path). That contract is gone with the
// retirement: encoding/json is a real GOROOT package now, so the
// named-struct Marshal/Unmarshal round-trip is ordinary, compilable
// Go. This test therefore asserts the new contract: the program
// compiles through the full ExportDir path, the document names the
// json calls, and the merged source keeps the import spec. The
// runtime behavior is pinned separately by the differential corpus
// program 42_json and by test/regression/json-roundtrip (both run
// under graalgo).
func TestJsonExportDir(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import (
	"encoding/json"
	"fmt"
)

type Item struct {
	Name  string         ` + "`json:\"name\"`" + `
	Price float64        ` + "`json:\"price\"`" + `
	Tags  []string       ` + "`json:\"tags,omitempty\"`" + `
	Meta  map[string]int ` + "`json:\"meta\"`" + `
}

func main() {
	it := Item{Name: "widget", Price: 9.99, Meta: map[string]int{"b": 2, "a": 1}}
	data, err := json.Marshal(it)
	fmt.Println(string(data), err)
	var back Item
	fmt.Println(json.Unmarshal(data, &back), back.Name, back.Meta["a"])
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
		t.Fatalf("ExportDir with import \"encoding/json\": %v", err)
	}
	// Named types render with their package path in the document
	// ("encoding/json.Item" for the local type is "main.Item"; the
	// json calls render with their package path).
	for _, want := range []string{
		`"name": "encoding/json.Marshal"`, `"name": "encoding/json.Unmarshal"`,
		`"name": "Marshal"`, `"name": "Unmarshal"`,
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
	if !strings.Contains(merged, `"encoding/json"`) {
		t.Error("merged source dropped the import \"encoding/json\" spec")
	}
}

// TestJsonFormerScopeErrorsNowCompile: the M7 synthetic-json scope
// contract is gone with the retirement — Unmarshal, Decoder.Decode,
// RawMessage.UnmarshalJSON and Marshal of a named struct are ordinary,
// compilable Go now. This test asserts the new contract: each former
// scope-error case now compiles through the full ExportDir path. The
// test keeps a negative pin so it can still fail: a genuinely
// undefined json function must still fail ExportDir with a type error.
func TestJsonFormerScopeErrorsNowCompile(t *testing.T) {
	cases := []string{
		"json.Unmarshal(data, &back)",
		"json.MarshalIndent(it, \"\", \"  \")",
		"dec.Decode(&back)",
		"rm.UnmarshalJSON(data)",
	}
	for _, body := range cases {
		dir := t.TempDir()
		src := `package main

import (
	"encoding/json"
	"strings"
)

type Item struct {
	Name string
}

func main() {
	data := []byte("{\"name\":\"widget\"}")
	var back Item
	it := Item{Name: "widget"}
	dec := json.NewDecoder(strings.NewReader("{}"))
	var rm json.RawMessage
	_ = data
	_ = it
	_ = dec
	_ = rm
	BODY
}
`
		src = strings.Replace(src, "BODY", body, 1)
		formatted, err := format.Source([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := ExportDir(dir); err != nil {
			t.Errorf("%s: expected success under real encoding/json, got error: %v", body, err)
		}
	}

	// Negative pin: an undefined json function is still a type error,
	// not a silent success.
	dir := t.TempDir()
	src := "package main\n\nimport \"encoding/json\"\n\nfunc main() {\n\tjson.NoSuchFunction()\n}\n"
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportDir(dir); err == nil {
		t.Error("json.NoSuchFunction(): expected a type error, got success")
	} else {
		t.Logf("undefined json function correctly rejected: %v", err)
	}
}
