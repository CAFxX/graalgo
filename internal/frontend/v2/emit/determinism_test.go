// Determinism regression test (V2-12).
//
// pkg.Files is a Go map: ranging over it directly made function order and
// type/object ids vary run to run, breaking reproducible builds and any
// future snapshot testing. Emission must be deterministic: files sorted by
// name, declarations sorted by source position, so repeated emissions of
// the same package are byte-identical regardless of Go's randomized map
// iteration order.
//
// The test is demonstrably capable of failing: with the fix reverted,
// 25 emissions of a 3-file package produce different byte output with
// overwhelming probability (6 possible file orders); with the fix, all 25
// are identical and functions appear in (filename, source-position) order.
package emit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// determinismSources is a multi-file package using only constructs the
// emitter handles, so every emission succeeds. The files are named so that
// alphabetical order is meaningful for the ordering assertion.
var determinismSources = map[string]string{
	"alpha.go": `package demo

func Alpha(x int) int {
	y := x * 2
	if y > 10 {
		return y
	}
	return y + 1
}
`,
	"beta.go": `package demo

func Beta(s string) string {
	t := s
	for i := 0; i < 3; i++ {
		t = t + "!"
	}
	return t
}
`,
	"gamma.go": `package demo

func Gamma(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total = total + Alpha(i)
	}
	return total
}
`,
}

func TestEmitDeterministic(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, len(determinismSources))
	for name, src := range determinismSources {
		fn := filepath.Join(dir, name)
		if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, fn)
	}
	// The input file list is the caller's contract: sort it so the test's
	// own input is deterministic and the 25-run loop below exercises only
	// the emitter's handling of pkg.Files map order.
	sort.Strings(paths)

	pres, err := parser.ParseFiles(paths)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	// Each Emit call re-ranges over pkg.Files; Go randomizes map iteration
	// per range, so this is the map-order perturbation the fix must
	// survive.
	const runs = 25
	var prev []byte
	for i := 0; i < runs; i++ {
		doc, err := Emit(cres, pres.Pkg, pres.Files)
		if err != nil {
			t.Fatalf("emit run %d: %v", i, err)
		}
		// Canonical encoding: encoding/json sorts map keys, so any byte
		// difference reflects slice order or values — exactly the parts
		// that must be deterministic.
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal run %d: %v", i, err)
		}
		if i == 0 {
			prev = b
			got := emittedFuncNames(doc)
			want := []string{"Alpha", "Beta", "Gamma"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("func order = %v, want %v (files sorted by name)", got, want)
			}
			continue
		}
		if !bytes.Equal(prev, b) {
			t.Fatalf("emit run %d differs from run 0: output not deterministic", i)
		}
	}
}

// emittedFuncNames returns the function names in Document.Funcs order.
func emittedFuncNames(doc *Document) []string {
	var names []string
	for _, f := range doc.Funcs {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fields, _ := m["fields"].(map[string]any)
		name, _ := fields["name"].(string)
		names = append(names, name)
	}
	return names
}
