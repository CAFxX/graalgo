package main

// Regression tests for the disk multi-result transform's named-result
// local synthesis (transformMRFunc in mr_disk.go).
//
// The pair-returning helper drops the named results from its signature, so
// any result the body still references must be declared as a local. The
// rule is per-name and scope-aware: declaring a result the rewritten body
// never references trips go/types' "declared and not used" — a false
// positive on valid Go (gc accepts unused named results). The reported
// instance was protowire.ConsumeVarint's n:
// wire.go:267:41: declared and not used: n
//
// Each test parses a snippet, runs transformDiskMultiResult, and
// type-checks the rewritten AST, asserting zero errors. The pinned
// behaviors:
//  1. TestDiskMRPartiallyUsedNamedResults: the reported shape — only some
//     named results referenced, explicit returns. (Failed pre-fix.)
//  2. TestDiskMRBareReturnDeclaresAll: a bare return reads every named
//     result, so all are declared.
//  3. TestDiskMRUnnamedResults: no names, no locals.
//  4. TestDiskMRShadowingRedecl: a body-level := re-declaring a result
//     name is a shadowing declaration, not a use. (Failed pre-fix.)
//  5. TestDiskMRClosureCapture: a result referenced only inside a nested
//     func literal still needs its local. (Failed pre-fix on the unused
//     sibling.)
//  6. TestDiskMRLitBareReturnIgnored: a bare return inside a nested func
//     literal belongs to the literal, not the outer function. (Failed
//     pre-fix.)
//  7. TestDiskMRMethodNamedResults: the receiver path.
//  8. TestDiskMRStructFieldKey: a struct literal field name matching an
//     unused result is not a use. (Failed pre-fix.)

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// transformAndCheckDiskMR parses src, runs the disk multi-result
// transform, and type-checks the rewritten AST, returning the type errors.
func transformAndCheckDiskMR(t *testing.T, src string) []error {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	conf := types.Config{Importer: importer.Default()}
	var errs []error
	conf.Error = func(e error) { errs = append(errs, e) }
	_, _ = conf.Check("example.com/p", fset, []*ast.File{f}, nil)
	return errs
}

func checkDiskMRClean(t *testing.T, src string) {
	t.Helper()
	if errs := transformAndCheckDiskMR(t, src); len(errs) != 0 {
		t.Fatalf("got %d type errors, want 0: %v", len(errs), errs)
	}
}

func TestDiskMRPartiallyUsedNamedResults(t *testing.T) {
	// The reported shape: (v uint64, n int) with only v referenced and
	// all returns explicit. n must not be declared as a local.
	checkDiskMRClean(t, `package p

const errCodeTruncated = -1

func ConsumeVarint(b []byte) (v uint64, n int) {
	if len(b) <= 0 {
		return 0, errCodeTruncated
	}
	v = uint64(b[0])
	if v < 0x80 {
		return v, 1
	}
	v -= 0x80
	return v, 2
}
`)
}

func TestDiskMRBareReturnDeclaresAll(t *testing.T) {
	// A bare return reads every named result, so both need locals.
	checkDiskMRClean(t, `package p

func bare() (a int, b string) {
	a = 1
	b = "x"
	return
}
`)
}

func TestDiskMRUnnamedResults(t *testing.T) {
	checkDiskMRClean(t, `package p

func unnamed() (int, string) {
	return 1, "x"
}
`)
}

func TestDiskMRShadowingRedecl(t *testing.T) {
	// v := 5 re-declares v in the body scope (shadowing the result); it is
	// not a use of the result, so no local is synthesized for v or n.
	// (Pre-fix this declared both and failed to type-check.)
	checkDiskMRClean(t, `package p

func shadow(b bool) (v int, n int) {
	v := 5
	_ = v
	if b {
		return 1, 2
	}
	return 0, 0
}
`)
}

func TestDiskMRClosureCapture(t *testing.T) {
	// v is referenced only inside the nested func literal: it still needs
	// its local (no "undefined: v"), while the unreferenced n must not get
	// one (no "declared and not used: n").
	checkDiskMRClean(t, `package p

func capture() (v int, n int) {
	defer func() { println(v) }()
	v = 1
	return 0, 0
}
`)
}

func TestDiskMRLitBareReturnIgnored(t *testing.T) {
	// The bare return belongs to the func literal, not to litbare; it
	// must not force locals for v and n.
	checkDiskMRClean(t, `package p

func litbare() (v int, n int) {
	g := func() { return }
	_ = g
	return 0, 0
}
`)
}

func TestDiskMRMethodNamedResults(t *testing.T) {
	checkDiskMRClean(t, `package p

type T struct{ x int }

func (m *T) meth() (v int, n int) {
	v = m.x
	return v, 0
}
`)
}

func TestDiskMRStructFieldKey(t *testing.T) {
	// The n in P{n: 1} is a struct field name, not a use of the result n.
	checkDiskMRClean(t, `package p

type P struct{ n int }

func fieldkey() (v int, n int) {
	p := P{n: 1}
	v = p.n
	return v, 0
}
`)
}

// TestDiskMRNoDeclaredNotUsed is a guard against the error class itself:
// no rewritten package may ever produce "declared and not used" for a
// synthesized named-result local.
func TestDiskMRNoDeclaredNotUsed(t *testing.T) {
	srcs := []string{
		`package p
func f1(b []byte) (v uint64, n int) {
	if len(b) == 0 {
		return 0, -1
	}
	v = uint64(b[0])
	return v, 1
}
`,
		`package p
func f2() (a, b, c int) {
	a, b = 1, 2
	return a, b, 0
}
`,
	}
	for i, src := range srcs {
		for _, err := range transformAndCheckDiskMR(t, src) {
			if strings.Contains(err.Error(), "declared and not used") {
				t.Errorf("src %d: false positive: %v", i, err)
			}
		}
	}
}
