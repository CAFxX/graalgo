package main

import (
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	// Reset the package-name cache so temp-dir paths from other tests
	// cannot leak in.
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, ftList, srcFiles, false)
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

// TestDiscoverTestsRequiresTestingT is the regression test for P1-21:
// discovery used to accept any TestXxx with one parameter, including
// TestMain (which takes *testing.M) and helpers taking other types.
func TestDiscoverTestsRequiresTestingT(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/discover\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package discover\n")
	writeFile(t, root, "a_test.go", `package discover

import "testing"

func TestMain(m *testing.M) {}

func TestReal(t *testing.T) {}

func TestUnnamed(*testing.T) {}

func TestWrongType(x int) {}

func TestTwoParams(t, u *testing.T) {}

func TestBench(b *testing.B) {}

func TestNoParam() {}
`)
	tests, err := discoverTests(filepath.Join(root, "a_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TestReal", "TestUnnamed"}
	if len(tests) != len(want) {
		t.Fatalf("discoverTests = %v, want %v", tests, want)
	}
	for i, w := range want {
		if tests[i] != w {
			t.Fatalf("discoverTests = %v, want %v", tests, want)
		}
	}
	if !hasTestMain(filepath.Join(root, "a_test.go")) {
		t.Errorf("hasTestMain = false for a file declaring TestMain")
	}
	// TestMain must not be synthesized either: it is skipped as an
	// entry point, so the driver never inlines or calls it.
	src := synthDriver(t, root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	if strings.Contains(src, "TestMain") {
		t.Errorf("driver synthesized TestMain:\n%s", src)
	}

	// Aliased and dot-imported testing packages still resolve.
	writeFile(t, root, "b_test.go", `package discover

import tt "testing"

func TestAliased(x *tt.T) {}
`)
	aliased, err := discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(aliased) != 1 || aliased[0] != "TestAliased" {
		t.Fatalf("discoverTests (aliased import) = %v, want [TestAliased]", aliased)
	}
	if hasTestMain(filepath.Join(root, "b_test.go")) {
		t.Errorf("hasTestMain = true for a file without TestMain")
	}
	writeFile(t, root, "c_test.go", `package discover

import . "testing"

func TestDot(x *T) {}
`)
	dotted, err := discoverTests(filepath.Join(root, "c_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dotted) != 1 || dotted[0] != "TestDot" {
		t.Fatalf("discoverTests (dot import) = %v, want [TestDot]", dotted)
	}
}

// TestShimImplementsMissingTMethods pins the P1-22 shim surface: every
// method the review found missing must exist on graalgoT, and Helper's
// no-op must carry its rationale instead of standing unexplained.
func TestShimImplementsMissingTMethods(t *testing.T) {
	shim := testShimSource(false)
	for _, m := range []string{
		"func (t *graalgoT) FailNow()",
		"func (t *graalgoT) Cleanup(",
		"func (t *graalgoT) Parallel()",
		"func (t *graalgoT) TempDir() string",
		"func (t *graalgoT) Setenv(",
		"func (t *graalgoT) Chdir(",
		"func (t *graalgoT) Deadline()",
		"func (t *graalgoT) Context()",
		"func (t *graalgoT) runCleanups()",
	} {
		if !strings.Contains(shim, m) {
			t.Errorf("shim missing %s", m)
		}
	}
	if !strings.Contains(shim, "Helper is intentionally a no-op") {
		t.Errorf("Helper lacks its no-op rationale comment")
	}
}

// TestUnsupportedTMethodRejected pins the P1-22 fallback: a test calling
// a testing.T method the shim does not implement fails at synthesis
// time with a clear error naming the method, instead of failing later
// in the backend with an obscure compile error.
func TestUnsupportedTMethodRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/unsup\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package unsup\n")
	writeFile(t, root, "a_test.go", `package unsup

import "testing"

func TestBad(t *testing.T) {
	t.NoSuchMethod()
}
`)
	importPkgNameCache = map[string]string{}
	_, err := synthesizeTestDriver(root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: []string{"TestBad"}}},
		[]string{filepath.Join(root, "a.go")}, false)
	if err == nil {
		t.Fatal("synthesizeTestDriver accepted a call to an unimplemented testing.T method")
	}
	if !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "NoSuchMethod") {
		t.Fatalf("synthesis error = %v, want it to name the unsupported method", err)
	}
}

// TestSupportedNewTMethodsSynthesize ensures the new shim methods pass
// synthesis (they need no position-capturing rewrite) and that the
// driver imports context for the shim's Context method.
func TestSupportedNewTMethodsSynthesize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/newmethods\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package newmethods\n")
	writeFile(t, root, "a_test.go", `package newmethods

import "testing"

func TestUses(t *testing.T) {
	_ = t.TempDir()
	t.Setenv("GRAALGO_SHIM_TEST_KEY", "v1")
	t.Chdir(".")
	_, _ = t.Deadline()
	_ = t.Context()
	t.Cleanup(func() {})
	t.Parallel()
	t.FailNow()
}
`)
	src := synthDriver(t, root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: []string{"TestUses"}}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, `"context"`) {
		t.Errorf("driver dropped the context import the shim needs:\n%s", src)
	}
}

// TestShimFeaturesRunUnderGC executes a synthesized driver with the gc
// toolchain: the new shim methods must behave, not just exist —
// cleanups run LIFO at subtest end, TempDir/Setenv/Chdir are scoped to
// the subtest, a FailNow in a subtest fails only the subtest, and
// Deadline/Context/Parallel behave as documented.
func TestShimFeaturesRunUnderGC(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			t.Skip("go toolchain not on PATH")
		}
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/shimfeat\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package shimfeat\n\nvar order []string\n")
	writeFile(t, root, "p_test.go", `package shimfeat

import (
	"os"
	"testing"
)

func TestCleanupLIFO(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		t.Cleanup(func() { order = append(order, "first") })
		t.Cleanup(func() { order = append(order, "second") })
	})
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("cleanup order = %v", order)
	}
}

func TestTempDirSetenvChdir(t *testing.T) {
	d := t.TempDir()
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		t.Fatalf("TempDir %q not a dir: %v", d, err)
	}
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("env", func(t *testing.T) {
		t.Setenv("GRAALGO_SHIM_TEST_KEY", "v1")
		if os.Getenv("GRAALGO_SHIM_TEST_KEY") != "v1" {
			t.Fatal("setenv did not take")
		}
		t.Chdir(d)
		if wd, _ := os.Getwd(); wd != d {
			t.Fatalf("chdir wd = %q, want %q", wd, d)
		}
	})
	if _, ok := os.LookupEnv("GRAALGO_SHIM_TEST_KEY"); ok {
		t.Fatal("setenv leaked past subtest")
	}
	if wd, _ := os.Getwd(); wd != origWd {
		t.Fatalf("chdir leaked: wd = %q, want %q", wd, origWd)
	}
}

func TestFailNowSubtest(t *testing.T) {
	if ok := t.Run("failnow", func(t *testing.T) { t.FailNow() }); ok {
		t.Fatal("Run returned true after FailNow")
	}
}

func TestDeadlineContextParallel(t *testing.T) {
	t.Parallel()
	if _, ok := t.Deadline(); ok {
		t.Fatal("Deadline reported a deadline")
	}
	if t.Context() == nil {
		t.Fatal("Context is nil")
	}
}
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(driver)
	cmd := exec.Command(goBin, "run", driver)
	// TMPDIR points at the test's own temp dir so the run never depends
	// on the ambient /tmp; GOCACHE is inherited (shared cache, faster
	// repeat runs).
	cmd.Env = append(os.Environ(), "TMPDIR="+root)
	out, err := cmd.CombinedOutput()
	// The driver exits 1 by design: TestFailNowSubtest fails because
	// its subtest fails (gc propagates subtest failure to the parent).
	// Anything other than exit status 1 is a harness failure.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
			t.Fatalf("go run driver: %v\n%s", err, out)
		}
	} else {
		t.Fatalf("go run driver exited 0, want 1 (TestFailNowSubtest must fail via its subtest):\n%s", out)
	}
	for _, want := range []string{
		"--- PASS: TestCleanupLIFO",
		"--- PASS: TestTempDirSetenvChdir",
		"--- FAIL: TestFailNowSubtest/failnow",
		"--- PASS: TestDeadlineContextParallel",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("driver output lacks %q:\n%s", want, out)
		}
	}
	// The subtest's FailNow panic must have been recovered by Run, not
	// propagated to the top-level wrapper: a "panic:" line would mean
	// the parent body after Run never executed.
	if strings.Contains(string(out), "panic: graalgo test fatal") {
		t.Errorf("subtest FailNow panic propagated instead of ending only the subtest:\n%s", out)
	}
}

// TestAliasedAndDotImportDriversRunUnderGC is the L14R P1-1 regression
// test: tests discovered through an aliased testing import (tt.T) or a
// dot-import (T) must synthesize to a driver that compiles and runs
// under gc — the pre-fix rewriteParam only rewrote the literal
// qualifier "testing", so the driver kept *tt.T / *T while the testing
// import was dropped, and `go run` failed with "undefined: tt" /
// "undefined: T". Nested function-literal params are covered too.
func TestAliasedAndDotImportDriversRunUnderGC(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			t.Skip("go toolchain not on PATH")
		}
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/aliasdot\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package aliasdot\n")
	writeFile(t, root, "a_test.go", `package aliasdot

import tt "testing"

func TestAliased(t *tt.T) {
	t.Run("sub", func(t *tt.T) {
		if 1+1 != 2 {
			t.Fatal("bad")
		}
		t.Logf("aliased %d", 1)
	})
}
`)
	writeFile(t, root, "b_test.go", `package aliasdot

import . "testing"

func TestDot(t *T) {
	t.Run("sub", func(t *T) {
		if 2+2 != 4 {
			t.Fatal("bad")
		}
	})
}
`)
	aTests, err := discoverTests(filepath.Join(root, "a_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(aTests) != 1 || aTests[0] != "TestAliased" {
		t.Fatalf("discoverTests (aliased) = %v, want [TestAliased]", aTests)
	}
	bTests, err := discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bTests) != 1 || bTests[0] != "TestDot" {
		t.Fatalf("discoverTests (dot) = %v, want [TestDot]", bTests)
	}
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(root,
		[]fileTests{
			{file: filepath.Join(root, "a_test.go"), tests: aTests},
			{file: filepath.Join(root, "b_test.go"), tests: bTests},
		},
		[]string{filepath.Join(root, "a.go")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(driver)
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	// The rewrite must have reached every testing.T form: the top-level
	// params and the nested t.Run closures, aliased and dot-imported.
	// (go/printer renders the rewritten single-param lists with a
	// trailing comma, as in the existing TestSame assertion above.)
	for _, want := range []string{
		"func TestAliased(t *graalgoT,)",
		"func TestDot(t *graalgoT,)",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("driver lacks %q:\n%s", want, src)
		}
	}
	if strings.Count(string(src), "func(t *graalgoT,)") < 2 {
		t.Errorf("driver lacks both nested *graalgoT closure params:\n%s", src)
	}
	cmd := exec.Command(goBin, "run", driver)
	cmd.Env = append(os.Environ(), "TMPDIR="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run driver: %v\n%s", err, out)
	}
	for _, want := range []string{
		"--- PASS: TestAliased",
		"--- PASS: TestAliased/sub",
		"--- PASS: TestDot",
		"--- PASS: TestDot/sub",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("driver output lacks %q:\n%s", want, out)
		}
	}
}

// TestShadowedTMethodNotRejected is the L14R P1-2 regression test: a
// *graalgoT parameter shadowed by a block-scope declaration (:=, var,
// for/if initializers, range clauses, switch initializers) must not
// have its method calls flagged as unsupported testing.T methods — the
// pre-fix checker tracked shadowing only for function-literal
// parameters, so a shadowed time.Time t calling .Format was bogusly
// rejected.
func TestShadowedTMethodNotRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/shadow\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package shadow\n")
	writeFile(t, root, "a_test.go", `package shadow

import (
	"testing"
	"time"
)

func TestShadow(t *testing.T) {
	// if-initializer shadow: t is time.Time in the condition and body.
	if t := time.Now(); t.Format("2006") == "" {
		_ = t
	}
	// Block-scope := shadow.
	{
		t := time.Now()
		_ = t.Format("2006")
	}
	// Block-scope var shadow.
	{
		var t = time.Now()
		_ = t.Format("2006")
	}
	// for-initializer shadow.
	for t := time.Now(); t.Year() > 3000; {
		break
	}
	// Range-clause shadow.
	for _, t := range []time.Time{time.Now()} {
		_ = t.Format("2006")
	}
	// switch-initializer shadow.
	switch t := time.Now(); t.Format("2006") {
	case "2006":
	default:
	}
	// The outer t is usable again after every shadow ends.
	t.Log("outer")
}
`)
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: []string{"TestShadow"}}},
		[]string{filepath.Join(root, "a.go")}, false)
	if err != nil {
		t.Fatalf("synthesizeTestDriver rejected shadowed calls: %v", err)
	}
	defer os.Remove(driver)
}

// TestShadowedTDoesNotSuppressOuterT pins the scope exit of the P1-2
// fix: shadowing must not leak past its scope — a genuinely
// unsupported call on the outer *graalgoT parameter is still rejected,
// and the error names that call, not the shadowed one.
func TestShadowedTDoesNotSuppressOuterT(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/shadowout\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package shadowout\n")
	writeFile(t, root, "a_test.go", `package shadowout

import (
	"testing"
	"time"
)

func TestShadowOuter(t *testing.T) {
	if t := time.Now(); t.Format("2006") == "" {
	}
	t.NoSuchMethod()
}
`)
	importPkgNameCache = map[string]string{}
	_, err := synthesizeTestDriver(root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: []string{"TestShadowOuter"}}},
		[]string{filepath.Join(root, "a.go")}, false)
	if err == nil {
		t.Fatal("synthesizeTestDriver accepted an unsupported call on the outer t")
	}
	if !strings.Contains(err.Error(), "NoSuchMethod") {
		t.Fatalf("synthesis error = %v, want it to name NoSuchMethod (the outer call), not the shadowed Format", err)
	}
}

// TestShadowedTCallNotRewritten pins the rewriteTestCalls half of the
// P1-2 fix: a diagnostic call on a block-shadowed t must not be
// rewritten to the shim's position-capturing variant, while a call on
// the outer *graalgoT parameter still is.
func TestShadowedTCallNotRewritten(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/shadowrw\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package shadowrw\n")
	writeFile(t, root, "a_test.go", `package shadowrw

import "testing"

type fakeT struct{}

func (fakeT) Errorf(format string, args ...interface{}) {}

func TestRewriteShadow(t *testing.T) {
	{
		t := fakeT{}
		t.Errorf("x")
	}
	t.Logf("outer %d", 1)
}
`)
	src := synthDriver(t, root,
		[]fileTests{{file: filepath.Join(root, "a_test.go"), tests: []string{"TestRewriteShadow"}}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, `t.Errorf("x")`) {
		t.Errorf("driver rewrote the shadowed t.Errorf call:\n%s", src)
	}
	if strings.Contains(src, "t.graalgoErrorf(") {
		t.Errorf("driver rewrote the shadowed call to t.graalgoErrorf:\n%s", src)
	}
	if !strings.Contains(src, "graalgoLogf") {
		t.Errorf("driver lost the outer t.Logf rewrite:\n%s", src)
	}
}

// TestSkippedSubtestParentPassesUnderGC is the L14R P1-3 regression
// test: gc reports "--- PASS: TestParent" with "--- SKIP:
// TestParent/sub" when only a subtest skips — the pre-fix Run
// propagated sub.skipped to t.skipped, so the parent line read
// "--- SKIP: TestParent".
func TestSkippedSubtestParentPassesUnderGC(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			t.Skip("go toolchain not on PATH")
		}
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/skips\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package skips\n")
	writeFile(t, root, "p_test.go", `package skips

import "testing"

func TestParent(t *testing.T) {
	t.Run("sub", func(t *testing.T) { t.Skip("nope") })
}
`)
	testFile := filepath.Join(root, "p_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(driver)
	cmd := exec.Command(goBin, "run", driver)
	cmd.Env = append(os.Environ(), "TMPDIR="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run driver: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestParent (") {
		t.Errorf("parent line is not --- PASS:\n%s", out)
	}
	if !strings.Contains(string(out), "--- SKIP: TestParent/sub (") {
		t.Errorf("subtest SKIP line missing:\n%s", out)
	}
	if strings.Contains(string(out), "--- SKIP: TestParent (") {
		t.Errorf("parent line wrongly SKIP:\n%s", out)
	}
}
