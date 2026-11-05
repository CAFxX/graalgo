package main

// Regression tests for review-1105-template-diskmr (independent review
// 2026-11-05-05): two distinct multi-result mechanisms that failed with
// the WS D error, plus the general shapes they exposed.
//
//  1. Disk multi-result call as a call argument (parse): exportCallArgs
//     hoisted only mrUser/mrFuncField plans, so a disk mrM7 call such as
//     text/template/parse.Tree.parseControl used as an argument to
//     newIf/newRange/newWith fell through to callType, which has no
//     disk-table entry and failed with the NAMED error
//     "multi-valued call result for text/template/parse.Tree.parseControl
//     is not supported in frontend/v1 scope (WS D)". The assignment
//     desugar handled the same call fine; only the call-arg position
//     was gated. (Failed pre-fix.)
//  2. Variable holding a multi-result function value produced by a call
//     (html): var entityMaps = sync.OnceValues(func() (map, map) {...})
//     had no userMRVars link — mrInfoForFuncValueInit covered func lits,
//     named functions and aliases, but not call initializers — so calls
//     through the variable failed with the UNNAMED WS D error (the
//     callee is a *types.Var, not a *types.Func, so callType cannot name
//     it). A second, independent gap in the same chain: the OnceValues
//     instantiation body calls d.f() on an ANONYMOUS struct, and
//     classifyGeneralFuncField required a named receiver type, so the
//     field call also failed. (Both failed pre-fix.)
//
// The tests export whole programs through ExportFile (the same entry
// point the gofrontend binary uses) and assert success; they do not
// assert wire details, so they stay valid if the desugar's node shape
// changes.

import (
	"os"
	"path/filepath"
	"testing"
)

func exportReview1105Prog(t *testing.T, name, src string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile(%s) failed: %v", name, err)
	}
}

func TestReview1105DiskMRCallArg(t *testing.T) {
	// text/template/parse imported for its Parse value; exporting the
	// parse package itself exercises parseControl-as-argument inside
	// ifControl/rangeControl/withControl.
	exportReview1105Prog(t, "textparse.go", `package main
import ("fmt"; "text/template/parse")
var _ = fmt.Sprint
var _ = parse.Parse
func main() {}
`)
}

func TestReview1105HTMLBlankImport(t *testing.T) {
	// Blank import of html exports the html package tree, including
	// the sync.OnceValues entityMaps variable and its call sites in
	// UnescapeString.
	exportReview1105Prog(t, "htmlblank.go", `package main
import _ "html"
func main() {}
`)
}

func TestReview1105OnceValuesVar(t *testing.T) {
	// The html shape reduced to a user package: a package-level var
	// holding the result of a generic wrapper call over a multi-result
	// func lit, called in a 2-target assignment.
	exportReview1105Prog(t, "oncevalues.go", `package main
import "sync"
var ov = sync.OnceValues(func() (int, int) { return 1, 2 })
func main() {
	a, b := ov()
	_, _ = a, b
}
`)
}

func TestReview1105AnonStructFuncField(t *testing.T) {
	// A func-typed field on an anonymous struct, called in a 2-target
	// assignment — the shape inside the OnceValues instantiation body.
	exportReview1105Prog(t, "anonfield.go", `package main
func main() {
	d := struct { f func() (int, int) }{f: func() (int, int) { return 1, 2 }}
	a, b := d.f()
	_, _ = a, b
}
`)
}

func TestReview1105SyntheticMRCallArg(t *testing.T) {
	// The same call-arg gate for a synthetic-table (mrM7) call:
	// encoding/hex.DecodeString as the sole argument of a 2-parameter
	// function. Guards against a parse-only fix.
	exportReview1105Prog(t, "hexcallarg.go", `package main
import "encoding/hex"
func use(a []byte, err error) {}
func main() { use(hex.DecodeString("00")) }
`)
}
