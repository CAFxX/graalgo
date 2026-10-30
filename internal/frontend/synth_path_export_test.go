package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticPathErrBadPattern is the PKG-029 regression test: the
// synthetic `path` package must expose ErrBadPattern. The generator used
// to port only path.go, dropping match.go's ErrBadPattern along with the
// scope-limited pattern-matching machinery — but ErrBadPattern is a
// dependency-free exported var that consumers reference directly (io/fs's
// Glob reports path.ErrBadPattern; io/fs/glob_test.go asserts on it).
// Without it, any path.ErrBadPattern reference fails type-checking with
// "undefined: path.ErrBadPattern" even though the importing file's import
// block is correct (the test-driver synthesis was exonerated: the
// generated driver does carry `import "path"`, and gc accepts it).
//
// The test mirrors the real io/fs driver shape: "path" imported both
// plain and under an explicit name (glob_test.go and walk_test.go do
// exactly this), with the qualified reference through the plain import.
func TestSyntheticPathErrBadPattern(t *testing.T) {
	src := `package main

import (
	"path"
	pathpkg "path"
)

func isBadPattern(err error) bool { return err == path.ErrBadPattern }

func main() {
	println(isBadPattern(pathpkg.ErrBadPattern))
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synpath.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}

// TestSyntheticPathMatchDesugarWiring pins the L13 (review 2026-10-30,
// R1-18) Appendix-M wiring for path.Match: exactly one m7 table entry
// for ("path", "Match", "") with helper __pathMatch, pair pathMatchPair,
// fields [matched err] in result order, plus the shared
// synthetic-package checks (Match exists with 2 results, __pathMatch
// exists with matching params, pair struct fields match the result
// types). It fails on the pre-L13 tree (no Match at all in the
// synthetic package: "undefined: path.Match") and on any wiring typo.
func TestSyntheticPathMatchDesugarWiring(t *testing.T) {
	checkM7DesugarWiring(t, m7StrconvJsonExpect{
		pkgPath:    "path",
		name:       "Match",
		recvType:   "",
		helper:     "__pathMatch",
		pairType:   "pathMatchPair",
		pairFields: []string{"matched", "err"},
	})
}

// TestSyntheticPathMatchExports verifies a user program calling
// path.Match in 2-target form exports cleanly through the frontend —
// the pre-L13 tree fails type-checking here with
// "undefined: path.Match". The calls exercise the star, character-class,
// unicode, and bad-pattern paths of __pathMatch (whose values are pinned
// against gc by the L13 differential, not here: the frontend has no
// evaluator, so this test pins acceptance + desugar wiring, while the
// differential pins semantics).
func TestSyntheticPathMatchExports(t *testing.T) {
	src := `package main

import "path"

func main() {
	ok, err := path.Match("*.txt", "a.txt")
	println(ok, err == nil)
	ok, err = path.Match("a/*/c", "a/b/c")
	println(ok, err == nil)
	ok, err = path.Match("[a-z]*", "éx")
	println(ok, err == nil)
	ok, err = path.Match("[", "a")
	println(ok, err == path.ErrBadPattern)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synpathmatch.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !containsPathMatchCall(out) {
		t.Fatalf("exported doc has no desugared __pathMatch call site")
	}
}

// containsPathMatchCall reports whether the exported doc contains a
// desugared call site for path.Match. The name "__pathMatch" occurs
// once for the helper's own funcdecl; every additional occurrence is a
// call site the Appendix-M desugar rewrote from path.Match. (A tree
// where the desugar never fired references "Match", not "__pathMatch",
// so the count stays 1.) The count is deliberately a lower bound, not
// exact: it pins that desugar fired at least once without overfitting
// the wire encoding.
func containsPathMatchCall(out string) bool {
	return strings.Count(out, `"name": "__pathMatch"`) >= 2
}
