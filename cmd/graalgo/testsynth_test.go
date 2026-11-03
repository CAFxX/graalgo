package main

import (
	"context"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFile creates dir/name with content, creating parent dirs.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// synthDriver runs synthesizeTestDriver on dir and returns the generated
// driver source.
func synthDriver(t *testing.T, dir string, ftList []fileTests, srcFiles []string) string {
	t.Helper()
	return synthDriverVerbose(t, dir, ftList, srcFiles, false)
}

// synthDriverVerbose is synthDriver with caller-chosen verbosity.
// Execution tests use verbose drivers so --- PASS/FAIL/SKIP lines are
// emitted for passing tests too (non-verbose drivers only print
// failures).
func synthDriverVerbose(t *testing.T, dir string, ftList []fileTests, srcFiles []string, verbose bool) string {
	t.Helper()
	// Reset the package-name cache so temp-dir paths from other tests
	// cannot leak in.
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, ftList, srcFiles, verbose)
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// discoverFileTests builds the fileTests entry runPackage would build
// for one test file (all discovered tests and examples, unfiltered).
func discoverFileTests(t *testing.T, file string) fileTests {
	t.Helper()
	df, err := discoverTestFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return fileTests{file: file, tests: df.tests, examples: df.examples}
}

// goTool locates the gc toolchain for driver-execution tests.
func goTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	t.Skip("go toolchain not available for driver-execution tests")
	return ""
}

// runDriver compiles and runs generated driver source with gc's go run
// and returns its combined output and exit code. The driver only uses
// the standard library, so it runs from its own scratch module.
func runDriver(t *testing.T, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module driver\n\ngo 1.21\n")
	writeFile(t, dir, "driver.go", src)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(t), "run", "driver.go")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("go run driver: %v\n%s", err, out)
	return "", -1
}

// mustContain fails unless out contains every listed fragment.
func mustContain(t *testing.T, out string, frags ...string) {
	t.Helper()
	for _, f := range frags {
		if !strings.Contains(out, f) {
			t.Errorf("driver output missing %q:\n%s", f, out)
		}
	}
}

// TestParentModuleImportKept is the regression test for the caddyconfig
// failure: a test importing its parent module package via a bare import
// whose path base differs from the declared package name
// (example.com/mod/v2 declares package mod). The old pathBase guess
// ("v2") pruned the import, yielding "undefined: mod" in the driver.
func TestParentModuleImportKept(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/mod/v2\n\ngo 1.21\n")
	writeFile(t, root, "mod.go", "package mod\n\nfunc Hello() string { return \"hi\" }\n")
	sub := filepath.Join(root, "sub")
	writeFile(t, sub, "sub.go", "package sub\n")
	writeFile(t, sub, "sub_test.go", `package sub

import (
	"example.com/mod/v2"
	"testing"
)

func TestParent(t *testing.T) {
	if mod.Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(sub, "sub_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestParent" {
		t.Fatalf("discoverTests = %v", tests)
	}
	src := synthDriver(t, sub,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(sub, "sub.go")})
	if !strings.Contains(src, `"example.com/mod/v2"`) {
		t.Errorf("driver dropped the parent-module import:\n%s", src)
	}
	if !strings.Contains(src, "mod.Hello()") {
		t.Errorf("driver lost the test body:\n%s", src)
	}
}

// TestStdlibV2ImportKept covers stdlib packages whose declared name
// differs from the path base (math/rand/v2 declares package rand).
func TestStdlibV2ImportKept(t *testing.T) {
	if got := resolveImportPkgName(t.TempDir(), "math/rand/v2"); got != "rand" {
		t.Errorf("resolveImportPkgName(math/rand/v2) = %q, want rand", got)
	}
	if got := resolveImportPkgName(t.TempDir(), "encoding/json/v2"); got != "json" {
		t.Errorf("resolveImportPkgName(encoding/json/v2) = %q, want json", got)
	}
	// Ordinary packages keep resolving to the path base.
	if got := resolveImportPkgName(t.TempDir(), "fmt"); got != "fmt" {
		t.Errorf("resolveImportPkgName(fmt) = %q, want fmt", got)
	}
}

// TestUnusedImportStillPruned ensures the pruning still drops imports the
// body never references (Go rejects unused imports).
func TestUnusedImportStillPruned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/prune\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package prune\n")
	writeFile(t, root, "a_test.go", `package prune

import (
	"strconv"
	"strings"
	"testing"
)

func TestUsed(t *testing.T) {
	_ = strings.TrimSpace(" x ")
}

func TestNotSelected(t *testing.T) {
	_ = strconv.Itoa(1)
}
`)
	testFile := filepath.Join(root, "a_test.go")
	if _, err := discoverTests(testFile); err != nil {
		t.Fatal(err)
	}
	// Select only TestUsed; TestNotSelected's import must be pruned even
	// though it was collected from the same file.
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: []string{"TestUsed"}}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, `"strings"`) {
		t.Errorf("driver dropped a used import:\n%s", src)
	}
	if strings.Contains(src, `"strconv"`) {
		t.Errorf("driver kept an unused import:\n%s", src)
	}
}

// TestBlankImportKept ensures side-effect imports survive pruning: they
// are never referenced by name, but dropping them changes semantics.
func TestBlankImportKept(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/blank\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package blank\n")
	writeFile(t, root, "a_test.go", `package blank

import (
	_ "strings"
	"testing"
)

func TestBlank(t *testing.T) {}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, `_ "strings"`) {
		t.Errorf("driver dropped a blank import:\n%s", src)
	}
}

// TestNoInlineForExternalOnly is the regression test for PKG-002's
// primary fix: when no test file needs white-box access (every test
// file is declared outside the package itself), the package's own
// sources are NOT inlined. The tests exercise the real imported
// package, so a dot-import of the package under test cannot redeclare
// anything, and the package's types keep their identity where other
// packages' APIs mention them.
func TestNoInlineForExternalOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/extonly\n\ngo 1.21\n")
	writeFile(t, root, "dot.go", "package extonly\n\nconst UpperCase = 1\n\nfunc Shout(s string) string { return s }\n")
	writeFile(t, root, "dot_test.go", `package extonly_test

import (
	. "example.com/extonly"
	"testing"
)

func TestDot(t *testing.T) {
	if Shout("x") == "" || UpperCase != 1 {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(root, "dot_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "dot.go")})
	// The package's sources are not inlined...
	if strings.Contains(src, "// --- from dot.go ---") {
		t.Errorf("driver inlined sources no test file needs:\n%s", src)
	}
	// ...so the dot-import of the real package must be kept: it is
	// load-bearing for the test's unqualified references.
	if !strings.Contains(src, `. "example.com/extonly"`) {
		t.Errorf("driver dropped the load-bearing dot-import:\n%s", src)
	}
	if !strings.Contains(src, `Shout("x")`) {
		t.Errorf("driver lost the test body:\n%s", src)
	}
}

// TestDotSelfImportDroppedWhenInlining covers the other half of
// PKG-002: when a test file IS in the package itself (white-box), the
// sources are inlined, and a dot self-import from an external test
// file must be dropped — otherwise it redeclares every inlined name.
func TestDotSelfImportDroppedWhenInlining(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/mixedwb\n\ngo 1.21\n")
	writeFile(t, root, "m.go", "package mixedwb\n\nconst Answer = 42\n")
	writeFile(t, root, "m_test.go", `package mixedwb

import "testing"

func TestWhiteBox(t *testing.T) {
	if Answer != 42 {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package mixedwb_test

import (
	. "example.com/mixedwb"
	"testing"
)

func TestExternal(t *testing.T) {
	if Answer != 42 {
		t.Fatal("bad")
	}
}
`)
	for _, tf := range []string{filepath.Join(root, "m_test.go"), filepath.Join(root, "e_test.go")} {
		tests, err := discoverTests(tf)
		if err != nil {
			t.Fatal(err)
		}
		if len(tests) != 1 {
			t.Fatalf("discoverTests(%s) = %v", tf, tests)
		}
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "m_test.go"), tests: []string{"TestWhiteBox"}},
			{file: filepath.Join(root, "e_test.go"), tests: []string{"TestExternal"}},
		},
		[]string{filepath.Join(root, "m.go")})
	// White-box access needs the sources inlined...
	if !strings.Contains(src, "const Answer = 42") {
		t.Errorf("driver did not inline sources needed for white-box access:\n%s", src)
	}
	// ...so the dot self-import must go.
	if strings.Contains(src, `. "example.com/mixedwb"`) {
		t.Errorf("driver kept the dot self-import alongside inlined sources:\n%s", src)
	}
}

// TestMixedSelfImportForms pins the import list's multi-form support:
// one test file dot-imports the package under test while another
// references it qualified. Both forms must survive — collapsing to one
// would leave the other's references undefined (gc accepts both forms
// in one import block).
func TestMixedSelfImportForms(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/multiform\n\ngo 1.21\n")
	writeFile(t, root, "m.go", "package multiform\n\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, root, "a_test.go", `package multiform_test

import (
	. "example.com/multiform"
	"testing"
)

func TestA(t *testing.T) {
	if Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "b_test.go", `package multiform_test

import (
	"example.com/multiform"
	"testing"
)

func TestB(t *testing.T) {
	if multiform.Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testsA, err := discoverTests(filepath.Join(root, "a_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsB, err := discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "a_test.go"), tests: testsA},
			{file: filepath.Join(root, "b_test.go"), tests: testsB},
		},
		[]string{filepath.Join(root, "m.go")})
	if !strings.Contains(src, `. "example.com/multiform"`) {
		t.Errorf("driver dropped the dot self-import form:\n%s", src)
	}
	if !strings.Contains(src, "\t\"example.com/multiform\"\n") {
		t.Errorf("driver dropped the plain self-import form:\n%s", src)
	}
}

// TestQualifiedSelfImportKept pins the other half of the PKG-002 design:
// a plain (qualified) self-import is kept, because external test files
// may reference the package's exported API through the qualifier.
func TestQualifiedSelfImportKept(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/qualself\n\ngo 1.21\n")
	writeFile(t, root, "q.go", "package qualself\n\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, root, "q_test.go", `package qualself_test

import (
	"example.com/qualself"
	"testing"
)

func TestQual(t *testing.T) {
	if qualself.Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(root, "q_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "q.go")})
	if !strings.Contains(src, `"example.com/qualself"`) {
		t.Errorf("driver dropped the qualified self-import:\n%s", src)
	}
}

// TestBuildExcludedFileIgnoredForPkgName is the regression test for
// PKG-003: packageNameInDir must honor build constraints. A generator
// file that sorts before the real sources (package main, //go:build
// ignore — like sort's gen_sort_variants.go) must not determine the
// import's package name; otherwise the pruner drops the import as
// unreferenced and the driver fails with "undefined: <pkg>".
func TestBuildExcludedFileIgnoredForPkgName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/genignored\n\ngo 1.21\n")
	sub := filepath.Join(root, "sortlike")
	writeFile(t, sub, "agen_main.go", "//go:build ignore\n\npackage main\n\nfunc main() {}\n")
	writeFile(t, sub, "sortlike.go", "package sortlike\n\nfunc SortInts(x []int) {}\n")
	writeFile(t, root, "u.go", "package genignored\n")
	writeFile(t, root, "u_test.go", `package genignored

import (
	"example.com/genignored/sortlike"
	"testing"
)

func TestUsesIt(t *testing.T) {
	sortlike.SortInts(nil)
}
`)
	if got := resolveImportPkgName(root, "example.com/genignored/sortlike"); got != "sortlike" {
		t.Fatalf("resolveImportPkgName = %q, want sortlike", got)
	}
	testFile := filepath.Join(root, "u_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "u.go")})
	if !strings.Contains(src, `"example.com/genignored/sortlike"`) {
		t.Errorf("driver dropped the import whose name was misresolved:\n%s", src)
	}
}

// TestDotImportOfAnyPackagePreserved pins the general dot-import
// mechanism (PKG-028): a test file dot-importing ANY package — not just
// the package under test — keeps the dot-import verbatim in the driver's
// import block, and the test body's unqualified references survive
// verbatim (not rewritten to a qualified form). Dot-imported names are
// unqualified in the test file; dropping or rewriting the import would
// leave them undefined. The frontend resolves dot-imports correctly
// (probed: dot-import of synthetic strings type-checks), so preservation
// is the right mechanism — no qualification rewrite is needed or wanted.
//
// (PKG-028's actual defect turned out to be the synthetic `path`
// package's scope-limited surface — Match/ErrBadPattern dropped — not
// the synthesis. This test guards the synthesis half so a future "fix"
// cannot regress dot-import preservation.)
func TestDotImportOfAnyPackagePreserved(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/dotany\n\ngo 1.21\n")
	writeFile(t, root, "dep/dep.go", "package dep\n\nfunc Helper() string { return \"h\" }\n")
	writeFile(t, root, "a.go", "package dotany\n")
	writeFile(t, root, "a_test.go", `package dotany_test

import (
	. "example.com/dotany/dep"
	"testing"
)

func TestDotDep(t *testing.T) {
	if Helper() != "h" {
		t.Fatal("bad")
	}
}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestDotDep" {
		t.Fatalf("discoverTests = %v", tests)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, `. "example.com/dotany/dep"`) {
		t.Errorf("driver dropped the dot-import of a non-self package:\n%s", src)
	}
	if !strings.Contains(src, `Helper() != "h"`) {
		t.Errorf("driver lost or rewrote the unqualified reference:\n%s", src)
	}
	if strings.Contains(src, "dep.Helper(") {
		t.Errorf("driver qualified a dot-imported reference (the dot-import provides it):\n%s", src)
	}
}

// TestIsSelfImportShortPath covers self-import detection when there is
// no module context at all: a package under a (fake) GOROOT whose test
// files import it by its short path. Detection must work purely by
// resolved source directory.
// TestImportCollisionUnusedPruned is the regression test for PKG-021:
// two imports binding the same local name, only one of them used. The
// package's own source imports "dep" (declared package name "dep") and
// uses it; a sibling external test file contributing no inlined
// declarations imports a *different* path under the explicit local name
// "dep". The old name-keyed pruner kept both and emitted `dep` twice
// ("dep redeclared in this block"); per-file resolution prunes the
// unused one.
func TestImportCollisionUnusedPruned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/impcol\n\ngo 1.21\n")
	writeFile(t, root, "dep/dep.go", "package dep\n\nfunc Who() string { return \"dep\" }\n")
	writeFile(t, root, "other/other.go", "package dep\n\nfunc Other() string { return \"other\" }\n")
	writeFile(t, root, "p.go", `package impcol

import "example.com/impcol/dep"

func Get() string { return dep.Who() }
`)
	writeFile(t, root, "p_test.go", `package impcol

import "testing"

func TestWhiteBox(t *testing.T) {
	if Get() != "dep" {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package impcol_test

import (
	dep "example.com/impcol/other"
	"testing"
)

func ExampleNothing() {
	_ = dep.Other()
	// Output:
}
`)
	tests, err := discoverTests(filepath.Join(root, "p_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: filepath.Join(root, "p_test.go"), tests: tests}},
		[]string{filepath.Join(root, "p.go")})
	if !strings.Contains(src, "\t\"example.com/impcol/dep\"\n") {
		t.Errorf("driver dropped the used import:\n%s", src)
	}
	if strings.Contains(src, "example.com/impcol/other") {
		t.Errorf("driver kept the unused colliding import:\n%s", src)
	}
}

// TestGenuineImportCollisionAliased covers the residual PKG-021 class:
// two imports binding the same local name that are BOTH genuinely used
// (by inlined declarations from different files). Emission must give
// the second a unique explicit alias and rewrite its files' qualifier
// references; the first keeps the natural name.
func TestGenuineImportCollisionAliased(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/aliascol\n\ngo 1.21\n")
	writeFile(t, root, "x/foo/foo.go", "package foo\n\nfunc X() int { return 1 }\n")
	writeFile(t, root, "y/foo/foo.go", "package foo\n\nfunc Y() int { return 2 }\n")
	writeFile(t, root, "p.go", `package aliascol

import "example.com/aliascol/x/foo"

func GetX() int { return foo.X() }
`)
	writeFile(t, root, "w_test.go", `package aliascol

import "testing"

func TestWhiteBox(t *testing.T) {
	if GetX() != 1 {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package aliascol_test

import (
	"example.com/aliascol/y/foo"
	"testing"
)

func TestExternal(t *testing.T) {
	if foo.Y() != 2 {
		t.Fatal("bad")
	}
}
`)
	testsW, err := discoverTests(filepath.Join(root, "w_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsE, err := discoverTests(filepath.Join(root, "e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "w_test.go"), tests: testsW},
			{file: filepath.Join(root, "e_test.go"), tests: testsE},
		},
		[]string{filepath.Join(root, "p.go")})
	// x/foo sorts first and keeps the natural name...
	if !strings.Contains(src, "\t\"example.com/aliascol/x/foo\"\n") {
		t.Errorf("driver lost the first import's natural name:\n%s", src)
	}
	// ...y/foo is explicitly aliased...
	if !strings.Contains(src, "\tfoo2 \"example.com/aliascol/y/foo\"\n") {
		t.Errorf("driver did not alias the colliding import:\n%s", src)
	}
	// ...and its file's qualifier references are rewritten.
	if !strings.Contains(src, "foo2.Y()") {
		t.Errorf("driver did not rewrite the qualifier to the alias:\n%s", src)
	}
	if !strings.Contains(src, "foo.X()") {
		t.Errorf("driver rewrote a qualifier it should have left alone:\n%s", src)
	}
}

// TestExtDeclCollisionRenamed is the regression test for PKG-025 and
// PKG-030: the package under test and its external test package each
// declare the same top-level name (legal under gc — separate packages),
// which the driver's single-file flattening would redeclare. The
// external test package yields: its declaration is renamed to a unique
// suffix and its references are rewritten; the package under test keeps
// the pristine name.
func TestExtDeclCollisionRenamed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/declcol\n\ngo 1.21\n")
	writeFile(t, root, "p.go", `package declcol

const bufSize = 16

func Fill(n int) int {
	if n < bufSize {
		return bufSize
	}
	return n
}
`)
	writeFile(t, root, "w_test.go", `package declcol

import "testing"

func TestWhiteBox(t *testing.T) {
	if Fill(4) != bufSize {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package declcol_test

import "testing"

const bufSize = 32

func helper() int { return bufSize }

func TestExternal(t *testing.T) {
	if helper() != bufSize {
		t.Fatal("bad")
	}
}
`)
	testsW, err := discoverTests(filepath.Join(root, "w_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsE, err := discoverTests(filepath.Join(root, "e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "w_test.go"), tests: testsW},
			{file: filepath.Join(root, "e_test.go"), tests: testsE},
		},
		[]string{filepath.Join(root, "p.go")})
	// The package under test keeps the pristine name...
	if !strings.Contains(src, "const bufSize = 16") {
		t.Errorf("driver lost the package's own declaration:\n%s", src)
	}
	// ...the external test package's is renamed...
	if !strings.Contains(src, "const bufSize2 = 32") {
		t.Errorf("driver did not rename the external colliding declaration:\n%s", src)
	}
	// ...and its references follow the rename while the package's own
	// references keep the pristine name.
	if !strings.Contains(src, "return bufSize2") || !strings.Contains(src, "helper() != bufSize2") {
		t.Errorf("driver did not rewrite the external references:\n%s", src)
	}
	if !strings.Contains(src, "if n < bufSize {") {
		t.Errorf("driver rewrote a reference it should have left alone:\n%s", src)
	}
}

// TestExtDeclShadowedLocalUntouched pins the scope-awareness of the
// external-declaration rewrite: a local variable shadowing the renamed
// name keeps working — neither its declaration nor its uses are
// renamed.
func TestExtDeclShadowedLocalUntouched(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/declshadow\n\ngo 1.21\n")
	writeFile(t, root, "p.go", `package declshadow

var level = 1

func Level() int { return level }
`)
	writeFile(t, root, "w_test.go", `package declshadow

import "testing"

func TestWhiteBox(t *testing.T) {
	if Level() != 1 {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package declshadow_test

import "testing"

var level = 2

func TestExternal(t *testing.T) {
	level := 3
	if level != 3 {
		t.Fatal("bad")
	}
	if Level() != 1 {
		t.Fatal("bad")
	}
}
`)
	testsW, err := discoverTests(filepath.Join(root, "w_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsE, err := discoverTests(filepath.Join(root, "e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "w_test.go"), tests: testsW},
			{file: filepath.Join(root, "e_test.go"), tests: testsE},
		},
		[]string{filepath.Join(root, "p.go")})
	if !strings.Contains(src, "var level2 = 2") {
		t.Errorf("driver did not rename the external declaration:\n%s", src)
	}
	// The shadowing local keeps its name and its uses.
	if !strings.Contains(src, "level := 3") {
		t.Errorf("driver renamed a shadowing local's declaration:\n%s", src)
	}
	if !strings.Contains(src, "if level != 3") {
		t.Errorf("driver renamed a shadowing local's use:\n%s", src)
	}
}

// TestExtTestFuncCollisionRenamed pins the call-site half of the
// external-declaration rename: when the white-box and external test
// packages declare the same TestXxx (legal under gc — separate
// packages), the external one is renamed AND the generated main calls
// the renamed function for the external file's entry. Calling the
// pristine name would silently run the wrong package's test.
func TestExtTestFuncCollisionRenamed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/testcol\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package testcol\n")
	writeFile(t, root, "w_test.go", `package testcol

import "testing"

var wbRan = false

func TestSame(t *testing.T) { wbRan = true }
`)
	writeFile(t, root, "e_test.go", `package testcol_test

import "testing"

var extRan = false

func TestSame(t *testing.T) { extRan = true }
`)
	testsW, err := discoverTests(filepath.Join(root, "w_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsE, err := discoverTests(filepath.Join(root, "e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "w_test.go"), tests: testsW},
			{file: filepath.Join(root, "e_test.go"), tests: testsE},
		},
		[]string{filepath.Join(root, "p.go")})
	if !strings.Contains(src, "func TestSame(t *graalgoT,)") {
		t.Errorf("driver lost the white-box test's pristine name:\n%s", src)
	}
	if !strings.Contains(src, "func TestSame2(") {
		t.Errorf("driver did not rename the external test function:\n%s", src)
	}
	// The external file's main entry calls the renamed function; the
	// white-box entry calls the pristine one.
	if strings.Count(src, "TestSame2(tt)") != 1 {
		t.Errorf("driver does not call the renamed test exactly once:\n%s", src)
	}
	if strings.Count(src, "TestSame(tt)") != 1 {
		t.Errorf("driver does not call the white-box test exactly once:\n%s", src)
	}
}

// TestExtDeclMethodReceiverRewritten pins the receiver-type half of the
// external-declaration rewrite: when the external test package declares
// a type colliding with the package under test's, methods on the
// external type keep pointing at the renamed type.
func TestExtDeclMethodReceiverRewritten(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/declrecv\n\ngo 1.21\n")
	writeFile(t, root, "p.go", `package declrecv

type Box struct{ v int }

func (b Box) Get() int { return b.v }
`)
	writeFile(t, root, "w_test.go", `package declrecv

import "testing"

func TestWhiteBox(t *testing.T) {
	if (Box{v: 1}).Get() != 1 {
		t.Fatal("bad")
	}
}
`)
	writeFile(t, root, "e_test.go", `package declrecv_test

import "testing"

type Box struct{ w int }

func (b Box) Sum() int { return b.w }

func TestExternal(t *testing.T) {
	if (Box{w: 2}).Sum() != 2 {
		t.Fatal("bad")
	}
}
`)
	testsW, err := discoverTests(filepath.Join(root, "w_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	testsE, err := discoverTests(filepath.Join(root, "e_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{
			{file: filepath.Join(root, "w_test.go"), tests: testsW},
			{file: filepath.Join(root, "e_test.go"), tests: testsE},
		},
		[]string{filepath.Join(root, "p.go")})
	if !strings.Contains(src, "type Box struct{ v int }") {
		t.Errorf("driver lost the package's own type:\n%s", src)
	}
	if !strings.Contains(src, "type Box2 struct{ w int }") {
		t.Errorf("driver did not rename the external type:\n%s", src)
	}
	if !strings.Contains(src, "func (b Box2) Sum() int") {
		t.Errorf("driver did not rewrite the method receiver type:\n%s", src)
	}
	if !strings.Contains(src, "func (b Box) Get() int") {
		t.Errorf("driver rewrote the package's own method:\n%s", src)
	}
}

func TestIsSelfImportShortPath(t *testing.T) {
	fake := t.TempDir()
	writeFile(t, fake, "src/mypkg/mypkg.go", "package mypkg\n")
	t.Setenv("GOROOT", fake)
	dir := filepath.Join(fake, "src", "mypkg")
	if !isSelfImport(dir, "mypkg") {
		t.Errorf("isSelfImport(mypkg dir, \"mypkg\") = false, want true")
	}
	if isSelfImport(dir, "otherpkg") {
		t.Errorf("isSelfImport(mypkg dir, \"otherpkg\") = true, want false")
	}
}

// TestSiblingBuildTaggedFileExcluded is the regression test for
// PKG-037: sibling test files (helpers not selected for -run) were
// collected with a raw *_test.go glob, ignoring build constraints.
// math's export_s390x_test.go (constrained to s390x by the _s390x
// filename suffix) was inlined on amd64, where its `var HasVX = hasVX`
// helper references hasVX from the equally s390x-only arith_s390x.go
// and fails to compile with "undefined: hasVX". The fix filters
// siblings through buildableGoFiles (the same build-constraint-aware
// mechanism runnableTests in test.go uses) — general for all packages
// and all GOOS/GOARCH tags, not math-specific.
func TestSiblingBuildTaggedFileExcluded(t *testing.T) {
	if build.Default.GOARCH == "s390x" {
		t.Skip("the s390x-constrained sibling is buildable on s390x; nothing to exclude")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/sibbuild\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package sib\n\nfunc F() int { return 1 }\n")
	// Selected test file: buildable on every platform.
	writeFile(t, root, "p_test.go", `package sib

import "testing"

func TestSelected(t *testing.T) {
	if F() != 1 {
		t.Fatal("bad")
	}
}
`)
	// Sibling helper file constrained to s390x by filename suffix —
	// mirrors math/export_s390x_test.go. Its helper references a name
	// that only exists in s390x-gated sources (mirrors hasVX from
	// arith_s390x.go); inlining it on another platform would leave the
	// reference undefined — the PKG-037 failure.
	writeFile(t, root, "export_s390x_test.go", `package sib

var HasVX = hasVX
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestSelected" {
		t.Fatalf("discoverTests = %v", tests)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")})
	if strings.Contains(src, "HasVX") {
		t.Errorf("driver inlined the s390x-constrained sibling's helper on %s/%s:\n%s",
			build.Default.GOOS, build.Default.GOARCH, src)
	}
}

// TestPKG075SelfNamedType is the regression test for PKG-075: when the
// package under test declares a top-level name equal to its own package
// name (like fmt's `type fmt struct`) and its sources are inlined
// (white-box test via an internal test file), a kept plain self-import
// must be aliased so it does not redeclare the inlined name ("fmt
// already declared through import of package fmt"). The emission
// uniqueness scope is the whole driver file, not just the import block.
func TestPKG075SelfNamedType(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/p\n\ngo 1.21\n")
	// Package declares a type named same as the package (like fmt's `type fmt struct`).
	writeFile(t, root, "p.go", "package p\n\ntype p struct{ x int }\n\nfunc (v *p) Get() int { return v.x }\n\nfunc Hello() string { return \"hi\" }\n")
	// Internal test file -> inlineSrc = true (like fmt's export_test.go).
	writeFile(t, root, "export_test.go", "package p\n\nvar InternalHook = Hello\n")
	// External test file with plain self-import (like fmt's errors_test.go).
	writeFile(t, root, "b_test.go", `package p_test

import (
	"example.com/p"
	"testing"
)

func TestB(t *testing.T) {
	if p.Hello() != "hi" {
		t.Fatal("bad")
	}
}
`)
	testsB, err := discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: filepath.Join(root, "b_test.go"), tests: testsB}},
		[]string{filepath.Join(root, "p.go")})

	// The generated driver must parse.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "driver.go", src, 0); err != nil {
		t.Fatalf("generated driver does not parse: %v\n%s", err, src)
	}
	// The self-import must be aliased (not plain "example.com/p"),
	// because the inlined `type p struct` occupies the name `p`.
	if strings.Contains(src, "\t\"example.com/p\"\n") {
		t.Errorf("self-import was not aliased despite inlined `type p struct`:\n%s", src)
	}
	if !strings.Contains(src, "p2 \"example.com/p\"") {
		t.Errorf("expected aliased self-import `p2 \"example.com/p\"`:\n%s", src)
	}
	// The qualifier reference must have been rewritten to the alias.
	if !strings.Contains(src, "p2.Hello()") {
		t.Errorf("expected qualifier rewritten to p2.Hello():\n%s", src)
	}
}

// TestDiscoverExcludesTestMain is the regression test for P1-T1: the
// old 1-param selection picked up TestMain(m *testing.M) as a test,
// and the generated driver then called it with a *graalgoT argument
// while testing.M was undefined, so the package could not compile.
// TestMain is now excluded from tests and reported separately.
func TestDiscoverExcludesTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/testmain\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package testmain\n")
	writeFile(t, root, "a_test.go", `package testmain

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestReal(t *testing.T) {}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestReal" {
		t.Fatalf("discoverTests = %v, want [TestReal]", tests)
	}
	df, err := discoverTestFile(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if !df.hasTestMain {
		t.Errorf("discoverTestFile.hasTestMain = false, want true")
	}
	// The synthesised driver must call TestMain through graalgoM, never
	// as a test with a *graalgoT argument.
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, "TestMain(m *graalgoM") {
		t.Errorf("driver did not rewrite TestMain to *graalgoM:\n%s", src)
	}
	if strings.Contains(src, "TestMain(tt)") {
		t.Errorf("driver calls TestMain as a test:\n%s", src)
	}
	if !strings.Contains(src, "TestMain(m)") {
		t.Errorf("driver main does not call TestMain:\n%s", src)
	}
}

// TestDiscoverExamples pins example discovery: Output: and Unordered
// output: comments, examples without output, and non-examples
// (parameterised Example functions are not examples under go test).
func TestDiscoverExamples(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a_test.go", `package ex

func ExampleOut() {
	println("x")
	// Output: hello
	// world
}

func ExampleUnordered() {
	println("x")
	// Unordered output: b
	// a
}

func ExamplePlain() {
	println("x")
}

func ExampleParam(x int) {}
`)
	df, err := discoverTestFile(filepath.Join(root, "a_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(df.examples) != 3 {
		t.Fatalf("examples = %v, want 3", df.examples)
	}
	if df.examples[0].name != "ExampleOut" || !df.examples[0].hasOutput || df.examples[0].output != "hello\nworld" {
		t.Errorf("ExampleOut = %+v", df.examples[0])
	}
	if df.examples[1].name != "ExampleUnordered" || !df.examples[1].hasOutput || !df.examples[1].unordered || df.examples[1].output != "b\na" {
		t.Errorf("ExampleUnordered = %+v", df.examples[1])
	}
	if df.examples[2].name != "ExamplePlain" || df.examples[2].hasOutput {
		t.Errorf("ExamplePlain = %+v", df.examples[2])
	}
}

// TestDiscoverDuplicateOutputBlock pins the gc rule (verified against
// gc 1.27: "there can only be one output comment block per example"):
// a second, separate Output: comment group in one example body is
// rejected at discovery time, instead of being silently ignored while
// the first block's output is used. A single comment group whose later
// lines happen to read "Output: ..." is NOT a second block: gc treats
// those lines as literal expected output, and so does discovery.
func TestDiscoverDuplicateOutputBlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "dup_test.go", `package ex

func Hello() {}

func ExampleHello() {
	Hello()
	// Output: hi
	Hello()
	// Output: bye
}
`)
	if _, err := discoverTestFile(filepath.Join(root, "dup_test.go")); err == nil {
		t.Errorf("discoverTestFile with two Output: blocks = no error, want rejection")
	} else if !strings.Contains(err.Error(), "one output comment block") {
		t.Errorf("discoverTestFile error = %v, want it to name the one-output-block rule", err)
	}

	writeFile(t, root, "lit_test.go", `package ex

func Hello() {}

func ExampleWorld() {
	Hello()
	// Output: hi
	// Output: bye
}
`)
	df, err := discoverTestFile(filepath.Join(root, "lit_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(df.examples) != 1 || df.examples[0].output != "hi\nOutput: bye" {
		t.Errorf("single-group literal Output: line = %+v, want output %q", df.examples, "hi\nOutput: bye")
	}
}

// TestSynthParseErrorsFail is the regression test for P1-T6: parse
// failures in sibling test files and package sources used to be
// silently skipped (continue), dropping init functions and
// package-level variables without a word. They now fail synthesis
// naming the file.
func TestSynthParseErrorsFail(t *testing.T) {
	// Sibling test file with a syntax error.
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/parseerr\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package parseerr\n")
	writeFile(t, root, "p_test.go", `package parseerr

import "testing"

func TestOK(t *testing.T) {}
`)
	writeFile(t, root, "bad_test.go", "package parseerr\n\nfunc broken( {\n")
	importPkgNameCache = map[string]string{}
	_, err := synthesizeTestDriver(root,
		[]fileTests{{file: filepath.Join(root, "p_test.go"), tests: []string{"TestOK"}}},
		[]string{filepath.Join(root, "p.go")}, false)
	if err == nil {
		t.Fatalf("sibling parse error was silently dropped")
	}
	if !strings.Contains(err.Error(), "bad_test.go") {
		t.Errorf("sibling parse error does not name the file: %v", err)
	}
	// Package source file with a syntax error.
	root2 := t.TempDir()
	writeFile(t, root2, "go.mod", "module example.com/parseerr2\n\ngo 1.21\n")
	writeFile(t, root2, "broken.go", "package parseerr2\n\nfunc broken( {\n")
	writeFile(t, root2, "p_test.go", `package parseerr2

import "testing"

func TestOK(t *testing.T) {}
`)
	importPkgNameCache = map[string]string{}
	_, err = synthesizeTestDriver(root2,
		[]fileTests{{file: filepath.Join(root2, "p_test.go"), tests: []string{"TestOK"}}},
		[]string{filepath.Join(root2, "broken.go")}, false)
	if err == nil {
		t.Fatalf("source parse error was silently dropped")
	}
	if !strings.Contains(err.Error(), "broken.go") {
		t.Errorf("source parse error does not name the file: %v", err)
	}
}

// TestDriverExecPassFailSkip executes a synthesised driver under gc:
// a passing, a failing, and a skipping test must produce the matching
// --- PASS/FAIL/SKIP lines and a non-zero exit code.
func TestDriverExecPassFailSkip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execpass\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execpass\n")
	writeFile(t, root, "a_test.go", `package execpass

import "testing"

func TestPass(t *testing.T) {}

func TestFail(t *testing.T) { t.Errorf("boom %d", 42) }

func TestSkip(t *testing.T) { t.Skip("nope") }
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero:\n%s", out)
	}
	mustContain(t, out, "--- PASS: TestPass", "--- FAIL: TestFail", "--- SKIP: TestSkip", "FAIL")

	// All-pass driver: exit 0 with a PASS summary.
	root2 := t.TempDir()
	writeFile(t, root2, "go.mod", "module example.com/execpass2\n\ngo 1.21\n")
	writeFile(t, root2, "a.go", "package execpass2\n")
	writeFile(t, root2, "a_test.go", `package execpass2

import "testing"

func TestPass(t *testing.T) {}
`)
	src2 := synthDriverVerbose(t, root2,
		[]fileTests{discoverFileTests(t, filepath.Join(root2, "a_test.go"))},
		[]string{filepath.Join(root2, "a.go")}, true)
	out2, code2 := runDriver(t, src2)
	if code2 != 0 {
		t.Errorf("all-pass driver exit code = %d, want 0:\n%s", code2, out2)
	}
	mustContain(t, out2, "--- PASS: TestPass", "PASS")
}

// TestDriverExecSubtestSkipFatalFailNow executes a driver whose parent
// test has subtests that Skip, Fatal, and FailNow. Under go test
// semantics each stops only its subtest: code after t.Run(...) runs
// (proven by the "continued" log), the parent fails (failed subtests)
// but is NOT reported skipped (a skipped subtest alone never skips
// the parent — the P0-3 corruption this lane's Run recover also fixes
// for its own execution tests; L-C owns the P0-3 verdict).
func TestDriverExecSubtestSkipFatalFailNow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execsub\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execsub\n")
	writeFile(t, root, "a_test.go", `package execsub

import "testing"

func TestParent(t *testing.T) {
	t.Run("skippy", func(t *testing.T) { t.Skip("nope") })
	t.Run("fatal", func(t *testing.T) { t.Fatal("boom") })
	t.Run("failnow", func(t *testing.T) { t.FailNow() })
	t.Log("continued")
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero:\n%s", out)
	}
	mustContain(t, out,
		"--- SKIP: TestParent/skippy",
		"--- FAIL: TestParent/fatal",
		"--- FAIL: TestParent/failnow",
		"--- FAIL: TestParent",
		"continued")
	if strings.Contains(out, "--- SKIP: TestParent ") {
		t.Errorf("parent wrongly reported SKIP (skipped subtest leaked):\n%s", out)
	}
}

// TestDriverExecTestMain executes a driver for a package with TestMain:
// TestMain must run (the test asserts its setup flag), drive the tests
// through m.Run(), and must not itself appear as a test result.
func TestDriverExecTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/exectm\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package exectm\n")
	writeFile(t, root, "a_test.go", `package exectm

import (
	"os"
	"testing"
)

var setupDone = false

func TestMain(m *testing.M) {
	setupDone = true
	os.Exit(m.Run())
}

func TestChecksSetup(t *testing.T) {
	if !setupDone {
		t.Fatal("TestMain did not run")
	}
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code != 0 {
		t.Errorf("driver exit code = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "--- PASS: TestChecksSetup")
	if strings.Contains(out, "--- PASS: TestMain") || strings.Contains(out, "=== RUN   TestMain") {
		t.Errorf("TestMain reported as a test:\n%s", out)
	}
}

// TestDriverExecExamples executes a driver with examples: matching
// output, mismatched output, unordered output, and no output comment.
// Only the mismatched example may fail.
func TestDriverExecExamples(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execex\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execex\n")
	writeFile(t, root, "a_test.go", `package execex

import (
	"fmt"
	"testing"
)

func TestDummy(t *testing.T) {}

func ExampleGood() {
	fmt.Println("hello")
	// Output: hello
}

func ExampleBad() {
	fmt.Println("goodbye")
	// Output: hello
}

func ExampleNoOutput() {
	fmt.Println("anything")
}

func ExampleUnordered() {
	fmt.Println("b")
	fmt.Println("a")
	// Unordered output: a
	// b
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero (ExampleBad mismatches):\n%s", out)
	}
	mustContain(t, out,
		"--- PASS: ExampleGood",
		"--- FAIL: ExampleBad",
		"--- PASS: ExampleNoOutput",
		"--- PASS: ExampleUnordered")
}

// TestDriverExecSubtestAncestorFatalSkip is the ancestor Fatal/Skip
// execution coverage for the vetoed L-F Run recover (F1): L-F matched
// Fatal/Skip sentinels by prefix, so a parent's Fatal/Skip called from
// inside a subtest was misattributed to the subtest and the parent
// kept executing after t.Run. This is the L-C
// TestSubtestSkipFatalExecution case set, adapted to this lane's
// driver helpers: a skipped subtest returns true and leaves the parent
// PASS, a fatal subtest returns false and leaves the parent FAIL, a
// nested skipped subtest skips neither its outer subtest nor the
// top-level test, code after each t.Run runs, a genuine subtest panic
// fails only that subtest, and an ancestor Fatal/Skip re-panics out of
// Run so the parent stops with its own verdict and the code after Run
// does NOT run.
func TestDriverExecSubtestAncestorFatalSkip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execancestor\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package execancestor\n")
	testFile := filepath.Join(root, "p_test.go")
	writeFile(t, root, "p_test.go", `package execancestor

import "testing"

func TestSubSkip(t *testing.T) {
	if ok := t.Run("skipme", func(t *testing.T) { t.Skip("not today") }); !ok {
		t.Error("Run returned false for a skipped subtest, want true")
	}
	if t.Skipped() {
		t.Error("parent marked skipped by a skipped subtest")
	}
	t.Log("after skipped subtest")
}

func TestSubFatal(t *testing.T) {
	if ok := t.Run("fatalme", func(t *testing.T) { t.Fatal("fatal subtest") }); ok {
		t.Error("Run returned true for a fatal subtest, want false")
	}
	if !t.Failed() {
		t.Error("parent not marked failed by a fatal subtest")
	}
	t.Log("after fatal subtest")
}

func TestSubNestedSkip(t *testing.T) {
	if ok := t.Run("outer", func(t *testing.T) {
		if ok := t.Run("inner", func(t *testing.T) { t.Skip("inner skip") }); !ok {
			t.Error("inner Run returned false for a skipped subtest")
		}
		if t.Skipped() {
			t.Error("outer marked skipped by a skipped inner subtest")
		}
	}); !ok {
		t.Error("outer Run returned false, want true")
	}
	if t.Skipped() {
		t.Error("top marked skipped by a skipped nested subtest")
	}
}

func TestSubPass(t *testing.T) {
	if ok := t.Run("passme", func(t *testing.T) { t.Log("subtest log") }); !ok {
		t.Error("Run returned false for a passing subtest")
	}
}

func TestSubPanic(t *testing.T) {
	if ok := t.Run("subpanic", func(t *testing.T) { panic("kaboom") }); ok {
		t.Error("Run returned true for a panicking subtest, want false")
	}
	if !t.Failed() {
		t.Error("parent not marked failed by a panicking subtest")
	}
	t.Log("after panic subtest")
}

func TestSubAncestorFatal(t *testing.T) {
	t.Run("child", func(st *testing.T) {
		_ = st
		t.Fatal("parent fatal from subtest")
	})
	t.Log("after ancestor fatal")
}

func TestSubAncestorSkip(t *testing.T) {
	t.Run("child", func(st *testing.T) {
		_ = st
		t.Skip("parent skip from subtest")
	})
	t.Log("after ancestor skip")
}
`)
	ft := discoverFileTests(t, testFile)
	if len(ft.tests) != 7 {
		t.Fatalf("discovered tests = %v, want 7", ft.tests)
	}
	src := synthDriverVerbose(t, root, []fileTests{ft}, []string{filepath.Join(root, "p.go")}, true)
	out, code := runDriver(t, src)
	// The driver must exit 1 because TestSubFatal fails — and only
	// because of that, which the line assertions below pin down.
	if code != 1 {
		t.Errorf("driver exit code = %d, want 1 (TestSubFatal must fail); output:\n%s", code, out)
	}
	mustContain(t, out,
		"=== RUN   TestSubSkip",
		"--- PASS: TestSubSkip (",
		"--- SKIP: TestSubSkip/skipme (",
		"after skipped subtest",
		"--- FAIL: TestSubFatal (",
		"--- FAIL: TestSubFatal/fatalme (",
		"after fatal subtest",
		"--- PASS: TestSubNestedSkip (",
		"--- PASS: TestSubNestedSkip/outer (",
		"--- SKIP: TestSubNestedSkip/outer/inner (",
		"--- PASS: TestSubPass (",
		"--- PASS: TestSubPass/passme (",
		"--- FAIL: TestSubPanic (",
		"--- FAIL: TestSubPanic/subpanic (",
		"panic: kaboom",
		"after panic subtest",
		"--- FAIL: TestSubAncestorFatal (",
		"--- SKIP: TestSubAncestorSkip (")
	for _, notWant := range []string{
		"--- SKIP: TestSubSkip (",
		"--- SKIP: TestSubFatal (",
		"--- SKIP: TestSubNestedSkip (",
		"--- SKIP: TestSubNestedSkip/outer (",
		"--- FAIL: TestSubAncestorFatal/child (",
		"--- SKIP: TestSubAncestorFatal/child (",
		"--- PASS: TestSubAncestorFatal/child (",
		"--- FAIL: TestSubAncestorSkip/child (",
		"--- SKIP: TestSubAncestorSkip/child (",
		"--- PASS: TestSubAncestorSkip/child (",
		"after ancestor fatal",
		"after ancestor skip",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("driver output contains %q (ancestor verdict misattributed or parent continued):\n%s", notWant, out)
		}
	}
	for _, pair := range [][2]string{
		{"--- PASS: TestSubSkip (", "--- SKIP: TestSubSkip/skipme ("},
		{"--- FAIL: TestSubFatal (", "--- FAIL: TestSubFatal/fatalme ("},
	} {
		pi, si := strings.Index(out, pair[0]), strings.Index(out, pair[1])
		if pi >= 0 && si >= 0 && pi > si {
			t.Errorf("parent line %q printed after subtest line %q:\n%s", pair[0], pair[1], out)
		}
	}
	if !strings.Contains(out, "\nFAIL\n") && !strings.HasSuffix(out, "FAIL\n") {
		t.Errorf("driver output lacks the FAIL summary line:\n%s", out)
	}
}

// TestDriverExecExamplePanicRestoresStdout is the F2 execution test: a
// panicking example must not leave stdout redirected into its capture
// pipe. The TestMain prints a marker after m.Run() returns; under the
// old wrapper (no deferred restore) that marker vanished into the
// abandoned pipe, and a later large write could block on the pipe
// filling. The passing example after the panicking one must still run.
func TestDriverExecExamplePanicRestoresStdout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execpanic\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execpanic\n")
	writeFile(t, root, "a_test.go", `package execpanic

import (
	"fmt"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Println("testmain before")
	m.Run()
	fmt.Println("testmain after")
}

func TestDummy(t *testing.T) {}

func ExamplePanics() {
	panic("example boom")
}

func ExampleAfter() {
	fmt.Println("ok")
	// Output: ok
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero (ExamplePanics must fail):\n%s", out)
	}
	mustContain(t, out,
		"testmain before",
		"testmain after",
		"--- PASS: TestDummy",
		"--- FAIL: ExamplePanics",
		"--- PASS: ExampleAfter")
}

// TestDriverExecExampleLargeOutput is the F3 execution test: an example
// printing ~220KB (well over the 64KB pipe buffer) must not deadlock.
// The old wrapper read the pipe only after the example returned, so
// the writer blocked on a full pipe forever; the fixed wrapper drains
// the pipe concurrently while the example runs. The example has no
// Output: comment, so it only has to run without panicking.
func TestDriverExecExampleLargeOutput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execbig\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execbig\n")
	writeFile(t, root, "a_test.go", `package execbig

import (
	"fmt"
	"testing"
)

func TestDummy(t *testing.T) {}

func ExampleBig() {
	for i := 0; i < 20000; i++ {
		fmt.Println("0123456789")
	}
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code != 0 {
		t.Errorf("driver exit code = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "--- PASS: ExampleBig")
}

// TestDriverExecSetenvError pins Setenv error handling: gc fails the
// test ("cannot set environment variable") when os.Setenv errors, e.g.
// on a key containing '='. The old shim ignored the error and carried
// on with the variable unset.
func TestDriverExecSetenvError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execsetenv\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execsetenv\n")
	writeFile(t, root, "a_test.go", `package execsetenv

import "testing"

func TestBadSetenv(t *testing.T) {
	t.Setenv("BAD=KEY", "v")
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code == 0 {
		t.Errorf("driver exit code = 0, want non-zero (bad Setenv key must fail):\n%s", out)
	}
	mustContain(t, out, "--- FAIL: TestBadSetenv", "cannot set environment variable")
}

// TestTwoTestMainRejected pins the corrected two-TestMain behaviour:
// gc rejects a package with both a white-box and an external TestMain
// ("multiple definitions of TestMain", setup failed). The old driver
// silently renamed the external TestMain away and ran only the
// white-box one, so synthesis now rejects the combination with the
// same message instead of emitting a driver.
func TestTwoTestMainRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/twotm\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package twotm\n")
	writeFile(t, root, "w_test.go", `package twotm

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestW(t *testing.T) {}
`)
	writeFile(t, root, "e_test.go", `package twotm_test

import "testing"

func TestMain(m *testing.M) {
	m.Run()
}

func TestE(t *testing.T) {}
`)
	importPkgNameCache = map[string]string{}
	_, err := synthesizeTestDriver(root,
		[]fileTests{
			discoverFileTests(t, filepath.Join(root, "w_test.go")),
			discoverFileTests(t, filepath.Join(root, "e_test.go")),
		},
		[]string{filepath.Join(root, "a.go")}, true)
	if err == nil {
		t.Fatalf("synthesizeTestDriver with white+external TestMain = no error, want rejection")
	}
	if !strings.Contains(err.Error(), "multiple definitions of TestMain") {
		t.Errorf("synthesizeTestDriver error = %v, want it to name the multiple-TestMain rule", err)
	}
}

// TestDriverExecTestMainHelper pins *testing.M rewriting for helpers:
// a helper called from TestMain may take *testing.M (pointer or bare
// value). The old driver rewrote the type only in TestMain itself, so
// a helper's *testing.M parameter survived into a driver whose testing
// import is dropped, and the driver failed to compile.
func TestDriverExecTestMainHelper(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/exechm\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package exechm\n")
	writeFile(t, root, "a_test.go", `package exechm

import "testing"

var setupDone = false

func runSuite(m *testing.M) int {
	return m.Run()
}

func runSuiteValue(m testing.M) int {
	return (&m).Run()
}

func TestMain(m *testing.M) {
	setupDone = true
	runSuite(m)
	runSuiteValue(*m)
}

func TestChecksSetup(t *testing.T) {
	if !setupDone {
		t.Fatal("TestMain did not run")
	}
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code != 0 {
		t.Errorf("driver exit code = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "--- PASS: TestChecksSetup")
}

// TestDriverExecShimMethods executes a driver using the P1-T2 shim
// methods: Parallel, Deadline, Context, Cleanup, TempDir, Setenv, and
// Chdir — including cleanup ordering (the second test observes the
// first test's cleanup effects: TempDir removed, Chdir restored).
func TestDriverExecShimMethods(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/execshim\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package execshim\n")
	writeFile(t, root, "a_test.go", `package execshim

import (
	"os"
	"path/filepath"
	"testing"
)

var cleaned = false
var tempDirPath string
var startDir string

func TestShim(t *testing.T) {
	t.Parallel()
	if _, ok := t.Deadline(); ok {
		t.Fatal("unexpected deadline")
	}
	if t.Context() == nil {
		t.Fatal("nil context")
	}
	t.Cleanup(func() { cleaned = true })
	d := t.TempDir()
	tempDirPath = d
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal("write failed")
	}
	startDir, _ = os.Getwd()
	t.Run("env", func(t *testing.T) {
		t.Setenv("GRAALGO_EXEC_SHIM_KEY", "v1")
		if os.Getenv("GRAALGO_EXEC_SHIM_KEY") != "v1" {
			t.Fatal("setenv not visible")
		}
	})
	// The subtest's Setenv cleanup must have restored the variable.
	if os.Getenv("GRAALGO_EXEC_SHIM_KEY") != "" {
		t.Fatal("setenv not restored after subtest")
	}
	t.Chdir(d)
	wd, _ := os.Getwd()
	if wd != d {
		t.Fatalf("chdir did not take effect")
	}
}

func TestAfter(t *testing.T) {
	if !cleaned {
		t.Fatal("cleanup did not run")
	}
	if _, err := os.Stat(tempDirPath); err == nil {
		t.Fatal("tempdir not removed by cleanup")
	}
	wd, _ := os.Getwd()
	if wd != startDir {
		t.Fatal("chdir not restored by cleanup")
	}
}
`)
	src := synthDriverVerbose(t, root,
		[]fileTests{discoverFileTests(t, filepath.Join(root, "a_test.go"))},
		[]string{filepath.Join(root, "a.go")}, true)
	out, code := runDriver(t, src)
	if code != 0 {
		t.Errorf("driver exit code = %d, want 0:\n%s", code, out)
	}
	mustContain(t, out, "--- PASS: TestShim", "--- PASS: TestAfter")
}
