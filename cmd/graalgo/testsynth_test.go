package main

import (
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// TestMain support (P1-3). TestMain is an entry point, not a test: it
// must never be discovered as one, and the driver must call it with a
// testing.M equivalent instead of a *graalgoT.

// TestDiscoverTestsExcludesTestMain: TestMain is not returned by
// discoverTests, while ordinary tests are — including multi-arity
// Test functions, which `go test` ignores as tests, and tests using an
// aliased or dot-imported testing package.
func TestDiscoverTestsExcludesTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tm\n\ngo 1.21\n")
	writeFile(t, root, "a_test.go", `package tm

import "testing"

func TestMain(m *testing.M) {
}

func TestReal(t *testing.T) {
}

func TestTwo(t *testing.T, u *testing.T) {
}

func TestNone() {
}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestReal" {
		t.Fatalf("discoverTests = %v, want [TestReal]", tests)
	}
	found, err := discoverTestMain(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Errorf("discoverTestMain = false, want true")
	}

	// Aliased and dot-imported testing spellings are the same entry
	// points, not different ones.
	writeFile(t, root, "b_test.go", `package tm

import tt "testing"

func TestMain(m *tt.M) {
}

func TestAliased(t *tt.T) {
}
`)
	// b_test.go's TestMain would be a second TestMain in the package;
	// discovery is per file, so each file is checked on its own.
	tests, err = discoverTests(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestAliased" {
		t.Fatalf("discoverTests (aliased) = %v, want [TestAliased]", tests)
	}
	found, err = discoverTestMain(filepath.Join(root, "b_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Errorf("discoverTestMain (aliased) = false, want true")
	}

	writeFile(t, root, "c_test.go", `package tm

import . "testing"

func TestMain(m *M) {
}

func TestDot(t *T) {
}
`)
	tests, err = discoverTests(filepath.Join(root, "c_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestDot" {
		t.Fatalf("discoverTests (dot-import) = %v, want [TestDot]", tests)
	}
	found, err = discoverTestMain(filepath.Join(root, "c_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Errorf("discoverTestMain (dot-import) = false, want true")
	}
}

// TestDiscoverTestsRejectsNonTParam: a Test function taking exactly one
// parameter that is not *testing.T, and a TestMain taking anything but
// *testing.M, are rejected with a clear diagnostic — not miscompiled
// into a driver that fails with a generated-code type error.
func TestDiscoverTestsRejectsNonTParam(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmrej\n\ngo 1.21\n")
	writeFile(t, root, "bad_test.go", `package tmrej

import "testing"

func TestBad(x int) {
}
`)
	if _, err := discoverTests(filepath.Join(root, "bad_test.go")); err == nil {
		t.Errorf("discoverTests accepted TestBad(x int), want an error")
	} else if !strings.Contains(err.Error(), "TestBad") || !strings.Contains(err.Error(), "*testing.T") {
		t.Errorf("discoverTests error = %v, want it to name TestBad and *testing.T", err)
	}

	writeFile(t, root, "badmain_test.go", `package tmrej

import "testing"

func TestMain(m *testing.T) {
}
`)
	if _, err := discoverTestMain(filepath.Join(root, "badmain_test.go")); err == nil {
		t.Errorf("discoverTestMain accepted TestMain(m *testing.T), want an error")
	} else if !strings.Contains(err.Error(), "TestMain") || !strings.Contains(err.Error(), "*testing.M") {
		t.Errorf("discoverTestMain error = %v, want it to name TestMain and *testing.M", err)
	}

	// Two TestMains in one file are a duplicate, not two entry points.
	writeFile(t, root, "dup_test.go", `package tmrej

import "testing"

func TestMain(m *testing.M) {
}

func TestMain(m *testing.M) {
}
`)
	if _, err := discoverTestMain(filepath.Join(root, "dup_test.go")); err == nil {
		t.Errorf("discoverTestMain accepted two TestMains, want an error")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("discoverTestMain error = %v, want it to say duplicate", err)
	}

	// A zero-parameter TestMain is not an entry point at all.
	writeFile(t, root, "noparam_test.go", `package tmrej

import "testing"

func TestMain() {
}
`)
	found, err := discoverTestMain(filepath.Join(root, "noparam_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("discoverTestMain = true for zero-parameter TestMain, want false")
	}
}

// TestSynthDriverTestMain: the driver calls TestMain with a *graalgoM
// (whose Run runs the discovered tests) and never as a test with a
// *graalgoT. Every assertion below fails on the pre-fix driver, which
// emitted TestMain(tt) and left the TestMain parameter as *testing.M.
func TestSynthDriverTestMain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmdrv\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package tmdrv\n\nvar SetupRan bool\n")
	writeFile(t, root, "a_test.go", `package tmdrv

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	SetupRan = true
	os.Exit(m.Run())
}

func TestReal(t *testing.T) {
	if !SetupRan {
		t.Fatal("setup did not run")
	}
}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestReal" {
		t.Fatalf("discoverTests = %v, want [TestReal]", tests)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "driver.go", src, 0); err != nil {
		t.Fatalf("generated driver does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(src, "func TestMain(m *graalgoM") {
		t.Errorf("driver did not rewrite TestMain parameter to *graalgoM:\n%s", src)
	}
	if !strings.Contains(src, "func (m *graalgoM) Run() int") {
		t.Errorf("driver has no graalgoM.Run:\n%s", src)
	}
	if !strings.Contains(src, "func graalgoRunAllTests() int") {
		t.Errorf("driver has no graalgoRunAllTests:\n%s", src)
	}
	if strings.Contains(src, "TestMain(tt)") {
		t.Errorf("driver still calls TestMain as a test with *graalgoT:\n%s", src)
	}
	main := src[strings.LastIndex(src, "func main()"):]
	if !strings.Contains(main, "TestMain(tm)") {
		t.Errorf("generated main does not call TestMain(tm):\n%s", main)
	}
	if strings.Contains(main, "TestReal(tt)") {
		t.Errorf("generated main calls TestReal directly instead of via TestMain:\n%s", main)
	}
}

// TestSynthDriverTestMainExternalPackage: TestMain in an external test
// package (package foo_test) is called the same way; the package's own
// sources are not inlined, but that changes nothing about TestMain.
func TestSynthDriverTestMainExternalPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmext\n\ngo 1.21\n")
	writeFile(t, root, "foo.go", "package tmext\n\nfunc Hello() string { return \"hi\" }\n")
	writeFile(t, root, "foo_test.go", `package tmext_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestExt(t *testing.T) {
}
`)
	testFile := filepath.Join(root, "foo_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "foo.go")})
	main := src[strings.LastIndex(src, "func main()"):]
	if !strings.Contains(main, "TestMain(tm)") {
		t.Errorf("generated main does not call external TestMain(tm):\n%s", main)
	}
	if !strings.Contains(src, "func TestMain(m *graalgoM") {
		t.Errorf("driver did not rewrite external TestMain parameter to *graalgoM:\n%s", src)
	}
}

// TestSynthDriverTestMainRenamedOnCollision: when the package under test
// itself declares a top-level TestMain (an ordinary function, legal in
// its own package), the flattened driver must rename the external test
// package's TestMain and call the renamed one — calling the plain name
// would invoke the package's own zero-parameter function with a
// *graalgoM argument.
func TestSynthDriverTestMainRenamedOnCollision(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmcol\n\ngo 1.21\n")
	writeFile(t, root, "foo.go", "package tmcol\n\nfunc TestMain() {}\n")
	writeFile(t, root, "a_test.go", `package tmcol

import "testing"

func TestWhite(t *testing.T) {
}
`)
	writeFile(t, root, "b_test.go", `package tmcol_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestExt(t *testing.T) {
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
		[]string{filepath.Join(root, "foo.go")})
	main := src[strings.LastIndex(src, "func main()"):]
	if !strings.Contains(main, "TestMain2(tm)") {
		t.Errorf("generated main does not call renamed TestMain2(tm):\n%s", main)
	}
	if !strings.Contains(src, "func TestMain2(m *graalgoM") {
		t.Errorf("driver did not rename external TestMain to TestMain2:\n%s", src)
	}
	if !strings.Contains(src, "func TestMain()\t{}") {
		t.Errorf("driver lost the package's own TestMain function:\n%s", src)
	}
}

// TestSynthDriverTestMainAliasedImport: an aliased testing import
// (import tt "testing") rewrites TestMain's parameter to *graalgoM, not
// just the plain `testing` spelling.
func TestSynthDriverTestMainAliasedImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmalias\n\ngo 1.21\n")
	writeFile(t, root, "a.go", "package tmalias\n\nvar SetupRan bool\n")
	writeFile(t, root, "a_test.go", `package tmalias

import (
	"os"
	tt "testing"
)

func TestMain(m *tt.M) {
	SetupRan = true
	os.Exit(m.Run())
}

func TestAliased(t *tt.T) {
	if !SetupRan {
		t.Fatal("setup did not run")
	}
}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 1 || tests[0] != "TestAliased" {
		t.Fatalf("discoverTests = %v, want [TestAliased]", tests)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "a.go")})
	if !strings.Contains(src, "func TestMain(m *graalgoM") {
		t.Errorf("driver did not rewrite aliased TestMain parameter to *graalgoM:\n%s", src)
	}
	if !strings.Contains(src, "func TestAliased(t *graalgoT") {
		t.Errorf("driver did not rewrite aliased test parameter to *graalgoT:\n%s", src)
	}
	main := src[strings.LastIndex(src, "func main()"):]
	if !strings.Contains(main, "TestMain(tm)") {
		t.Errorf("generated main does not call TestMain(tm):\n%s", main)
	}
}

// TestSynthDriverTestMainSiblingFile: TestMain in a test file whose own
// tests were not selected still runs — it is a sibling file, not a
// selected one, and TestMain runs regardless of selection.
func TestSynthDriverTestMainSiblingFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/tmsib\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package tmsib\n")
	writeFile(t, root, "a_test.go", `package tmsib

import "testing"

func TestSelected(t *testing.T) {
}
`)
	writeFile(t, root, "b_test.go", `package tmsib

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestOther(t *testing.T) {
}
`)
	testFile := filepath.Join(root, "a_test.go")
	tests, err := discoverTests(testFile)
	if err != nil {
		t.Fatal(err)
	}
	src := synthDriver(t, root,
		[]fileTests{{file: testFile, tests: tests}},
		[]string{filepath.Join(root, "p.go")})
	main := src[strings.LastIndex(src, "func main()"):]
	if !strings.Contains(main, "TestMain(tm)") {
		t.Errorf("generated main does not call sibling TestMain(tm):\n%s", main)
	}
	if strings.Contains(src, "TestOther(tt)") {
		t.Errorf("driver runs unselected sibling TestOther as a test:\n%s", src)
	}
}
