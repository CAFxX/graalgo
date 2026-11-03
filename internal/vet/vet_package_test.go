package vet

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunSeparatesTestPackage is the regression test for review
// REVIEW-2026-11-03-12 P1-1(a): a directory holding a package and its
// external test package must vet as two passes, not as one merged
// synthetic package. Both passes must type-check cleanly (no error)
// and findings from both files must be reported — the test-package
// finding (copylocks) depends on valid type info for the test package
// alone, which a merged check cannot provide.
func TestRunSeparatesTestPackage(t *testing.T) {
	dir := t.TempDir()
	fooSrc := `package foo

import "fmt"

func Greet() {
	fmt.Printf("%d", "hello")
}
`
	testSrc := `package foo_test

import (
	"fmt"
	"sync"
)

func copyLock(m sync.Mutex) {
	_ = m
}

func Check() {
	fmt.Printf("%d", "hello")
}
`
	fooPath := filepath.Join(dir, "foo.go")
	testPath := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(fooPath, []byte(fooSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{fooPath, testPath})
	if err != nil {
		t.Fatalf("Run(foo + foo_test) returned error, want clean separate passes: %v", err)
	}
	var filesSeen []string
	var msgs []string
	for _, d := range ds {
		filesSeen = append(filesSeen, d.Pos.Filename)
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, wantFile := range []string{fooPath, testPath} {
		found := false
		for _, f := range filesSeen {
			if f == wantFile {
				found = true
			}
		}
		if !found {
			t.Errorf("no diagnostic from %s, got diagnostics in %v:\n%s", wantFile, filesSeen, joined)
		}
	}
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("expected printf diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "copies lock") {
		t.Errorf("expected copylocks diagnostic from the test package, got:\n%s", joined)
	}
}

// TestRunCrossFileTypeError is the regression test for review
// REVIEW-2026-11-03-12 P1-1(b): a package whose type error spans two
// files must not vet clean. Run must return the type-check failure so
// cmdVet can never print "vet: no issues found" for it.
func TestRunCrossFileTypeError(t *testing.T) {
	dir := t.TempDir()
	aSrc := `package p

func Get() int {
	return Value
}
`
	bSrc := `package p

var Value = "not an int"
`
	aPath := filepath.Join(dir, "a.go")
	bPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(aPath, []byte(aSrc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte(bSrc), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{aPath, bPath})
	if err == nil {
		t.Fatal("Run() returned nil error for a package with a cross-file type error, want a type-check failure")
	}
	if !strings.Contains(err.Error(), "type check") {
		t.Errorf("error should identify a type-check failure, got: %v", err)
	}
}

// TestRunMixedPackagesInOneDir rejects a directory mixing two package
// names: such files are not one package, and checking them together
// would silently vet a package that does not exist.
func TestRunMixedPackagesInOneDir(t *testing.T) {
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo.go")
	barPath := filepath.Join(dir, "bar.go")
	if err := os.WriteFile(fooPath, []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(barPath, []byte("package bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Run([]string{fooPath, barPath})
	if err == nil {
		t.Fatal("Run() returned nil error for one directory with two packages, want an error")
	}
	if !strings.Contains(err.Error(), "found packages") {
		t.Errorf("error should report mixed packages, got: %v", err)
	}
}

// TestRunTwoDirsSamePackageName checks that files from two different
// directories are never merged, even when they share a package name:
// each directory is its own package pass.
func TestRunTwoDirsSamePackageName(t *testing.T) {
	src := `package foo

import "fmt"

func Greet() {
	fmt.Printf("%d", "hello")
}
`
	var paths []string
	var dirs []string
	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		dirs = append(dirs, dir)
		p := filepath.Join(dir, "foo.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	ds, err := Run(paths)
	if err != nil {
		t.Fatalf("Run(two dirs, same package name) returned error, want clean separate passes: %v", err)
	}
	seen := make(map[string]bool)
	for _, d := range ds {
		seen[d.Pos.Filename] = true
	}
	for _, p := range paths {
		if !seen[p] {
			t.Errorf("no diagnostic from %s", p)
		}
	}
}

// TestRunAsmDiagnosticNotDuplicatedAcrossPasses is the regression test
// for the REVIEW-2026-11-03-12 veto finding 1: framepointer (and any
// directory-scoped analyzer) scans the directory's .s files once per
// pass, so a directory with foo.go + foo_test.go + one violating .s
// file used to yield the identical framepointer diagnostic twice.
// Run must report it exactly once.
func TestRunAsmDiagnosticNotDuplicatedAcrossPasses(t *testing.T) {
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		t.Skip("framepointer amd64 assembly fixture only fires on amd64 linux/darwin")
	}
	dir := t.TempDir()
	fooPath := filepath.Join(dir, "foo.go")
	testPath := filepath.Join(dir, "foo_test.go")
	asmPath := filepath.Join(dir, "foo_amd64.s")
	if err := os.WriteFile(fooPath, []byte("package foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte("package foo_test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	asmSrc := "TEXT \u00b7clobber(SB),$0-0\n" +
		"\tMOVQ AX, BP\n" +
		"\tRET\n"
	if err := os.WriteFile(asmPath, []byte(asmSrc), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{fooPath, testPath})
	if err != nil {
		t.Fatalf("Run(foo + foo_test) returned error, want clean passes: %v", err)
	}
	n := 0
	for _, d := range ds {
		if d.Message == "frame pointer is clobbered before saving" {
			n++
			if d.Pos.Filename != asmPath {
				t.Errorf("framepointer diagnostic from unexpected file %s", d.Pos.Filename)
			}
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 framepointer diagnostic, got %d: %v", n, ds)
	}
}

// TestRunTwoDirsSameClauseNoModule is the regression test for the
// second veto finding on REVIEW-2026-11-03-12 P1-1: outside a module,
// the import path falls back to the bare package clause name, so two
// directories with clause `foo` used to share one key in the global
// checked map and the second package silently shadowed the first for
// every test pass. Dir A has only Greet, dir B adds OnlyInB; A's test
// file using foo.OnlyInB must fail honestly (against A's package),
// while B's test file using foo.OnlyInB must resolve against B's
// package and run its analyzers (proved by its printf diagnostic).
func TestRunTwoDirsSameClauseNoModule(t *testing.T) {
	write := func(t *testing.T, dir, name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	dirA := t.TempDir()
	dirB := t.TempDir()
	aFoo := write(t, dirA, "foo.go", "package foo\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n")
	aTest := write(t, dirA, "foo_test.go", "package foo_test\n\nimport \"foo\"\n\nfunc Check() {\n\t_ = foo.OnlyInB\n}\n")
	bFoo := write(t, dirB, "foo.go", "package foo\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n\nfunc OnlyInB() {}\n")
	bTest := write(t, dirB, "foo_test.go", "package foo_test\n\nimport (\n\t\"fmt\"\n\t\"foo\"\n)\n\nfunc Check() {\n\tfoo.OnlyInB()\n\tfmt.Printf(\"%d\", \"hello\")\n}\n")
	ds, err := Run([]string{aFoo, aTest, bFoo, bTest})
	if err == nil {
		t.Fatal("Run() returned nil error, want a type-check failure for dir A's test file using foo.OnlyInB against A's package")
	}
	if !strings.Contains(err.Error(), dirA) {
		t.Errorf("error should name dir A (%s), got: %v", dirA, err)
	}
	if strings.Contains(err.Error(), dirB) {
		t.Errorf("error should not name dir B (%s) — B's test file must resolve against B's package, got: %v", dirB, err)
	}
	found := false
	for _, d := range ds {
		if d.Pos.Filename == bTest && strings.Contains(d.Message, "expects integer") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected printf diagnostic from B's test file (proof its test pass resolved against B's package), got: %v", ds)
	}
}

// TestRunStdlibNameClauseDoesNotShadowStdlib is the regression test
// for the stdlib-shadowing half of the second veto finding on
// REVIEW-2026-11-03-12 P1-1: a no-module package whose clause is a
// stdlib name (here `fmt`) used to shadow the real stdlib import for
// every test pass via the global checked map. With per-directory
// scoping, another directory's test pass importing "fmt" must still
// resolve the real stdlib (proved by a clean run plus its printf
// diagnostic, which needs stdlib fmt's Printf signature).
func TestRunStdlibNameClauseDoesNotShadowStdlib(t *testing.T) {
	write := func(t *testing.T, dir, name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	fakeDir := t.TempDir()
	barDir := t.TempDir()
	fakePath := write(t, fakeDir, "fmt.go", "package fmt\n\nfunc Fake() {}\n")
	barPath := write(t, barDir, "bar.go", "package bar\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n")
	barTestPath := write(t, barDir, "bar_test.go", "package bar_test\n\nimport \"fmt\"\n\nfunc Check() {\n\tfmt.Printf(\"%d\", \"hello\")\n}\n")
	ds, err := Run([]string{fakePath, barPath, barTestPath})
	if err != nil {
		t.Fatalf("Run() returned error, want the bar test pass to resolve stdlib fmt, not the fake fmt package: %v", err)
	}
	found := false
	for _, d := range ds {
		if d.Pos.Filename == barTestPath && strings.Contains(d.Message, "expects integer") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected printf diagnostic from the bar test file (proof it resolved stdlib fmt), got: %v", ds)
	}
}

// TestRunExternalTestImportsPackageUnderTest is the regression test for
// the REVIEW-2026-11-03-12 veto finding 2: the standard external-test
// layout — foo_test.go importing the package under test — must
// type-check. Previously the test pass used importer.Default(), which
// resolves only installed export data, so the import failed with
// "could not import" and vet reported 0 diagnostics, exit 1.
func TestRunExternalTestImportsPackageUnderTest(t *testing.T) {
	fooSrc := `package foo

func Greet() string {
	return "hello"
}
`
	testSrc := func(importPath string) string {
		return "package foo_test\n\nimport (\n\t\"fmt\"\n\t\"" + importPath + "\"\n)\n\nfunc Check() {\n\t_ = foo.Greet()\n\tfmt.Printf(\"%d\", \"hello\")\n}\n"
	}
	cases := []struct {
		name       string
		goMod      string // "" = no go.mod: import by package clause name
		subdir     string // "" = package at the module root
		importPath string
	}{
		{name: "module", goMod: "module example.com/foo\n\ngo 1.21\n", importPath: "example.com/foo"},
		{name: "module-subdir", goMod: "module example.com/mod\n\ngo 1.21\n", subdir: "pkg", importPath: "example.com/mod/pkg"},
		{name: "no-module", goMod: "", importPath: "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.goMod != "" {
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.goMod), 0644); err != nil {
					t.Fatal(err)
				}
			}
			pkgDir := dir
			if tc.subdir != "" {
				pkgDir = filepath.Join(dir, tc.subdir)
				if err := os.MkdirAll(pkgDir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			fooPath := filepath.Join(pkgDir, "foo.go")
			testPath := filepath.Join(pkgDir, "foo_test.go")
			if err := os.WriteFile(fooPath, []byte(fooSrc), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(testPath, []byte(testSrc(tc.importPath)), 0644); err != nil {
				t.Fatal(err)
			}
			ds, err := Run([]string{fooPath, testPath})
			if err != nil {
				t.Fatalf("Run(foo + importing foo_test) returned error, want the package under test importable: %v", err)
			}
			// The printf diagnostic in the test file proves the test
			// pass actually ran its analyzers on resolved type info,
			// not merely that the import error vanished.
			found := false
			for _, d := range ds {
				if d.Pos.Filename == testPath && strings.Contains(d.Message, "expects integer") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected printf diagnostic from %s, got: %v", testPath, ds)
			}
		})
	}
}
