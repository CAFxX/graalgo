package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSplitsExternalTestPackage: vet.Run must analyze the internal
// package and an external foo_test package separately (R1-3), reporting
// diagnostics from both instead of merging them into one synthetic
// package. Each file carries its own printf mismatch; both must be
// reported with their own file's position.
func TestRunSplitsExternalTestPackage(t *testing.T) {
	dir := t.TempDir()
	internal := filepath.Join(dir, "foo.go")
	external := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(internal, []byte("package foo\n\nimport \"fmt\"\n\nfunc Foo() { fmt.Printf(\"%d\", \"internal\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("package foo_test\n\nimport \"fmt\"\n\nfunc Bar() { fmt.Printf(\"%d\", \"external\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{internal, external})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if strings.Contains(d.Message, "expects integer") || strings.Contains(d.Message, "integer") {
			seen[d.Pos.Filename] = true
		}
	}
	if !seen[internal] {
		t.Errorf("no printf diagnostic for internal package file; diagnostics: %v", ds)
	}
	if !seen[external] {
		t.Errorf("no printf diagnostic for external test package file; diagnostics: %v", ds)
	}
}

// TestRunGroupsByDirectory: two directories declaring the same package
// name are distinct packages (R1-3). Diagnostics from both must survive
// grouping; neither directory's files may be dropped or merged away.
func TestRunGroupsByDirectory(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "foo.go")
	b := filepath.Join(root, "b", "foo.go")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package foo\n\nimport \"fmt\"\n\nfunc Foo() { fmt.Printf(\"%d\", \"x\") }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := Run([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if strings.Contains(d.Message, "integer") {
			seen[d.Pos.Filename] = true
		}
	}
	if !seen[a] || !seen[b] {
		t.Errorf("expected printf diagnostics from both directories, got %v", ds)
	}
}
