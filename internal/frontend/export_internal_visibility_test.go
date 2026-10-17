package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Regression tests for PKG-001: the frontend import gate applies Go's
// internal-visibility rule, with the package under test declared by the
// `graalgo test` driver via an explicit argv flag (testPackageDirFlag) —
// never via the environment.

// oldImportAdmitted replicates the pre-PKG-001 import gate
// (checkImportAllowed / needsOverlayImporter before the
// internal-visibility rule): the dedicated-constructor packages, any
// non-internal synthetic source, and disk-importable paths were
// admitted; everything else was a scope error. Internal paths are
// excluded from the equivalence corpus below: their handling is the
// intentional behavior change (visibility rule instead of blanket
// deny / bare disk probe).
func oldImportAdmitted(path string) bool {
	if path == "runtime" || path == "fmt" || path == "unsafe" {
		return true
	}
	if _, ok := syntheticSources[path]; ok {
		if strings.HasPrefix(path, "internal/") || strings.HasPrefix(path, "crypto/internal/") {
			// Synthetic-only: user imports not admitted (fell through
			// to the disk probe below).
		} else {
			return true
		}
	}
	return isDiskImportable(path)
}

func gorootSrc(t *testing.T) string {
	t.Helper()
	goroot := runtime.GOROOT()
	if goroot == "" {
		t.Skip("no GOROOT")
	}
	src := filepath.Join(goroot, "src")
	for _, d := range []string{
		filepath.Join(src, "crypto", "tls"),
		filepath.Join(src, "crypto", "internal", "boring"),
		filepath.Join(src, "strings"),
		filepath.Join(src, "net"),
		filepath.Join(src, "internal", "bytealg"),
	} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Skipf("missing std dir %s", d)
		}
	}
	return src
}

// TestImportAdmittedInternalVisibility pins Go's internal-visibility
// rule at the import gate: last-"internal"-element rooting (matching
// cmd/go's findInternal), synthetic-internal packages standing in for
// their real stdlib location, synthetic-only packages (no real
// counterpart) failing closed, and unresolvable/empty importers denied.
func TestImportAdmittedInternalVisibility(t *testing.T) {
	src := gorootSrc(t)
	cryptoTLS := filepath.Join(src, "crypto", "tls")
	stringsDir := filepath.Join(src, "strings")
	netDir := filepath.Join(src, "net")
	userDir := t.TempDir()

	cases := []struct {
		name         string
		path         string
		importingDir string
		want         bool
	}{
		{"boring admitted from crypto/tls", "crypto/internal/boring", cryptoTLS, true},
		{"boring denied from strings", "crypto/internal/boring", stringsDir, false},
		{"boring denied from user dir", "crypto/internal/boring", userDir, false},
		{"boring denied with empty importer", "crypto/internal/boring", "", false},
		{"bytealg admitted from net (top-level internal root is GOROOT/src)", "internal/bytealg", netDir, true},
		{"bytealg denied from user dir", "internal/bytealg", userDir, false},
		// internal/graalgo/fmtstate is a retired remnant path (the P3-M9
		// synthetic package was deleted with the synthetic-fmt retirement):
		// no synthetic registration and no disk counterpart, so the
		// visibility root is unresolvable and the gate fails closed —
		// denied even from stdlib.
		{"retired fmtstate path denied from strings", "internal/graalgo/fmtstate", stringsDir, false},
		{"retired fmtstate path denied from GOROOT/src", "internal/graalgo/fmtstate", src, false},
		{"retired fmtstate path denied from user dir", "internal/graalgo/fmtstate", userDir, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := importAdmitted(c.path, c.importingDir); got != c.want {
				t.Errorf("importAdmitted(%q, %q) = %v, want %v", c.path, c.importingDir, got, c.want)
			}
			// needsOverlayImporter is the same predicate's other view:
			// the importer must resolve exactly what the gate admits.
			if got := needsOverlayImporter(c.path, c.importingDir); got != c.want {
				t.Errorf("needsOverlayImporter(%q, %q) = %v, want %v", c.path, c.importingDir, got, c.want)
			}
		})
	}
}

// TestImportAdmittedNonInternalUnchanged pins that the PKG-001 rework
// did not move the gate for any non-internal import: the new predicate
// is byte-identical to the old one for every such path, regardless of
// the importing directory (which must be irrelevant off the
// internal-visibility path).
func TestImportAdmittedNonInternalUnchanged(t *testing.T) {
	src := gorootSrc(t)
	paths := []string{
		// Dedicated-constructor packages.
		"runtime", "fmt", "unsafe",
		// Synthetic sources (waves 1-3).
		"strings", "bytes", "errors", "sort", "math", "math/bits",
		"strconv", "encoding/json", "os", "io/fs", "reflect", "iter",
		"time", "context", "sync", "sync/atomic",
		// Disk-resolved stdlib (not synthetic).
		"net/http",
		// Unresolvable.
		"example.com/nonexistent/pkg-xyz-123",
	}
	dirs := []string{
		filepath.Join(src, "strings"),
		t.TempDir(),
		"",
	}
	for _, p := range paths {
		if isInternalPath(p) {
			t.Fatalf("equivalence corpus must be non-internal, got %q", p)
		}
		want := oldImportAdmitted(p)
		for _, d := range dirs {
			if got := importAdmitted(p, d); got != want {
				t.Errorf("importAdmitted(%q, %q) = %v, want pre-PKG-001 %v", p, d, got, want)
			}
			if got := needsOverlayImporter(p, d); got != want {
				t.Errorf("needsOverlayImporter(%q, %q) = %v, want pre-PKG-001 %v", p, d, got, want)
			}
		}
	}
}

// TestExportFilesRejectsInternalImport pins that an ordinary compile
// (no test-driver flag) rejects internal/* imports — and that the
// ambient environment cannot confer the privilege. PKG-001 trust
// boundary: GRAALGO_TEST_PACKAGE_DIR is no longer consulted, so even a
// user-exported value leaves `graalgo run` rejected.
func TestExportFilesRejectsInternalImport(t *testing.T) {
	t.Setenv("GRAALGO_TEST_PACKAGE_DIR", gorootSrc(t))

	dir := t.TempDir()
	f := filepath.Join(dir, "evil.go")
	src := "package main\n\nimport \"internal/bytealg\"\n\nfunc main() {}\n"
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFiles([]string{f}, "")
	if err == nil {
		t.Fatal("ExportFiles admitted internal/bytealg for an ordinary compile")
	}
	if !strings.Contains(err.Error(), "imports are not supported in frontend/v1 scope") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestImportingDir pins the importing-directory resolution: the
// test-driver flag takes precedence when present, otherwise the input
// files' directory is used.
func TestImportingDir(t *testing.T) {
	file := filepath.Join("some", "pkg", "x.go")
	if got, want := importingDir([]string{file}, ""), absDir(filepath.Join("some", "pkg")); got != want {
		t.Errorf("importingDir default = %q, want %q", got, want)
	}
	if got, want := importingDir([]string{file}, filepath.Join("a", "b")), absDir(filepath.Join("a", "b")); got != want {
		t.Errorf("importingDir flag = %q, want %q", got, want)
	}
	if got := importingDir(nil, ""); got != "" {
		t.Errorf("importingDir(nil, \"\") = %q, want \"\"", got)
	}
}

// TestDirWithin pins the at-or-under comparison, including symlink
// resolution on both sides (GOROOT is commonly a symlink, e.g.
// ~/tools/go -> go1.27.1, while package dirs are spelled through it).
func TestDirWithin(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if !dirWithin(sub, root) {
		t.Errorf("dirWithin(sub, root) = false, want true")
	}
	if !dirWithin(root, root) {
		t.Errorf("dirWithin(root, root) = false, want true")
	}
	if dirWithin(t.TempDir(), root) {
		t.Errorf("dirWithin(sibling, root) = true, want false")
	}
	// Sibling-prefix trap: root+"x" must not count as under root.
	if dirWithin(root+"x", root) {
		t.Errorf("dirWithin(root+\"x\", root) = true, want false")
	}

	parent := t.TempDir()
	link := filepath.Join(parent, "rootlink")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if !dirWithin(filepath.Join(link, "sub"), root) {
		t.Errorf("dirWithin(link/sub, root) = false, want true (symlink resolution)")
	}
	if !dirWithin(sub, link) {
		t.Errorf("dirWithin(sub, link) = false, want true (symlink resolution)")
	}
}
