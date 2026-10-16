package p3frontend

// P3FE-1 regression: every synthetic stdlib package constructor must call
// pkg.MarkComplete() before handing the package out. go/types leaves a
// package unusable (any user program importing it dies with
// "undefined: <pkg>") when it is not marked complete. This test pins
// completeness directly on each constructor AND end-to-end through the
// real MergeDir pipeline: a program importing the package and using one
// member must merge without error.

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type syntheticMarkCompleteCase struct {
	path   string // import path
	member string // qualified one-member use, e.g. "json.Marshal"
	build  func() *types.Package
}

var syntheticMarkCompleteCases = []syntheticMarkCompleteCase{
	{"encoding/csv", "csv.NewReader", syntheticCsvPackage},
	{"encoding/gob", "gob.NewBufferDecoder", syntheticGobPackage},
	{"errors", "errors.New", syntheticErrorsPackage},
	{"encoding/json", "json.Marshal", syntheticJsonPackage},
	{"sort", "sort.Ints", syntheticSortPackage},
	{"strconv", "strconv.Itoa", syntheticStrconvPackage},
	{"strings", "strings.ToUpper", syntheticStringsPackage},
	{"encoding/xml", "xml.Marshal", syntheticXmlPackage},
}

// TestSyntheticPackagesMarkComplete pins the direct invariant: each
// synthetic package reports Complete(). A constructor that forgets
// MarkComplete() fails here before any pipeline is involved.
func TestSyntheticPackagesMarkComplete(t *testing.T) {
	for _, c := range syntheticMarkCompleteCases {
		pkg := c.build()
		if pkg.Path() != c.path {
			t.Errorf("constructor for %q built package with path %q", c.path, pkg.Path())
		}
		if !pkg.Complete() {
			t.Errorf("synthetic package %q is not marked complete (missing MarkComplete)", c.path)
		}
		if obj := pkg.Scope().Lookup(strings.TrimPrefix(c.member, pkg.Name()+".")); obj == nil {
			t.Errorf("synthetic package %q missing member %q", c.path, c.member)
		}
	}
}

// TestSyntheticPackagesUsableViaMergeDir verifies the user-visible
// symptom from P3FE-1: a program importing each synthetic package and
// using one member must pass the real MergeDir pipeline (type-check the
// import, flatten). Before the fix, every one of these failed with
// "undefined: <pkg>".
func TestSyntheticPackagesUsableViaMergeDir(t *testing.T) {
	for _, c := range syntheticMarkCompleteCases {
		t.Run(c.path, func(t *testing.T) {
			dir := t.TempDir()
			src := "package main\n\nimport " + `"` + c.path + `"` + "\n\nvar _ = " + c.member + "\n\nfunc main() {}\n"
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
				t.Fatal(err)
			}
			merged, err := MergeDir(dir)
			if err != nil {
				t.Fatalf("MergeDir with import %q (use %s): %v", c.path, c.member, err)
			}
			if merged == "" {
				t.Fatalf("MergeDir with import %q produced empty output", c.path)
			}
		})
	}
}
