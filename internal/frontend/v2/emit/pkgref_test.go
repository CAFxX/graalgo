// Pkgref identity regression test (L14).
//
// v2's emitSelector used to wire the LOCAL import alias into the pkgref
// node's "pkg" field ("pkg": id.Name) and discard the go/types PkgName.
// `import f "fmt"` and `import "fmt"` then produced different wire values
// ("f" vs "fmt") for the same package — the backend (or any future
// cross-package resolution) could not identify the package canonically.
//
// The fix: emit pkgname.Imported().Path(), the canonical import path.
//
// The test is demonstrably capable of failing: with the fix reverted, the
// aliased import case emits pkg="f" instead of "fmt" and the test fails.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// pkgrefPkgs collects the "pkg" field of every pkgref node in doc.
func pkgrefPkgs(doc *Document) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		if m["kind"] == "pkgref" {
			if fields, ok := m["fields"].(map[string]any); ok {
				if pkg, ok := fields["pkg"].(string); ok {
					out = append(out, pkg)
				}
			}
		}
		for _, val := range m {
			switch t := val.(type) {
			case map[string]any:
				walk(t)
			case []any:
				for _, e := range t {
					walk(e)
				}
			}
		}
	}
	for _, f := range doc.Funcs {
		walk(f)
	}
	return out
}

// emitPkgrefTestProgram parses, checks, and emits one single-file program.
func emitPkgrefTestProgram(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{path})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pres.Pkg, []string{path})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

func TestPkgrefEmitsCanonicalPath(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "aliased import",
			src:  "package main\n\nimport f \"fmt\"\n\nfunc main() {\n\tf.Println(\"hi\")\n}\n",
		},
		{
			name: "plain import",
			src:  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n",
		},
		{
			name: "explicit same-name alias",
			src:  "package main\n\nimport fmtAlias \"fmt\"\n\nfunc main() {\n\tfmtAlias.Println(\"hi\")\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := emitPkgrefTestProgram(t, tc.src)
			pkgs := pkgrefPkgs(doc)
			if len(pkgs) != 1 {
				t.Fatalf("expected exactly 1 pkgref node, got %d (%v)", len(pkgs), pkgs)
			}
			if pkgs[0] != "fmt" {
				t.Fatalf("pkgref pkg = %q, want canonical import path %q", pkgs[0], "fmt")
			}
		})
	}
}
