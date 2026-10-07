package main

import (
	"encoding/json"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"
)

// Reviewer-2 P2-4: pkgPathOf must fail loud on an unexpected nil Pkg.
// "" is legitimate ONLY for the universe-scope predeclared named types
// (error, comparable — go/types builds them with nil Pkg). This test
// exports a program exercising user-declared, synthetic-package, and
// universe named types, and asserts every "" pkgpath belongs to a
// universe predeclared type — i.e. no user-visible named type takes
// the silent "" path.
func TestPkgPathEmptyOnlyForUniversePredeclared(t *testing.T) {
	src := `package main

import (
	"fmt"
	"strings"
)

type userT struct{ X int }

func returnsErr() error { return nil }

func main() {
	var u userT
	var r strings.Reader
	err := returnsErr()
	fmt.Println(u.X, err == nil)
	_ = r
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "pkgpath.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	types, ok := doc["types"].([]any)
	if !ok {
		t.Fatal("export has no types array")
	}
	sawError := false
	for i, v := range types {
		m, ok := v.(map[string]any)
		if !ok || m["kind"] != "named" {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		name, _ := d["name"].(string)
		pp, _ := d["pkgpath"].(string)
		if pp != "" {
			continue
		}
		if name == "error" || name == "comparable" {
			sawError = sawError || name == "error"
			continue
		}
		t.Errorf("types[%d] named %q: empty pkgpath for a non-universe type", i, name)
	}
	if !sawError {
		t.Error("expected the universe \"error\" type in the export (covers the \"\" path)")
	}
}

// pkgPathOf must fail loud for a non-universe named type with nil Pkg,
// and accept the universe predeclared ones.
func TestPkgPathOfFailsLoudOnUnexpectedNilPkg(t *testing.T) {
	bogusObj := types.NewTypeName(token.NoPos, nil, "Bogus", nil)
	bogus := types.NewNamed(bogusObj, types.NewStruct(nil, nil), nil)
	if _, err := pkgPathOf(bogus); err == nil {
		t.Error("pkgPathOf(Bogus, nil Pkg): expected error, got nil")
	}

	errType, ok := types.Universe.Lookup("error").Type().(*types.Named)
	if !ok {
		t.Fatal("universe error is not *types.Named")
	}
	pp, err := pkgPathOf(errType)
	if err != nil {
		t.Errorf("pkgPathOf(error): unexpected error: %v", err)
	}
	if pp != "" {
		t.Errorf("pkgPathOf(error) = %q, want \"\"", pp)
	}
}
