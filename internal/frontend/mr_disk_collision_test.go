package main

// Tests for the disk multi-result transform's collision policy
// (transformDiskMultiResult in mr_disk.go, review-bytes-mr lane).
//
// A multi-result function whose generated helper (__F) or pair type
// (__FPair) name already exists as a top-level declaration was
// silently skipped: no spec, no funcdecl, and the method vanished from
// the type's method table, so any interface dispatch on it failed when
// the itab was built. The fix renames deterministically instead:
// __F_mr2, __F_mr3, ... — the first suffix whose __-names are both
// free. Renaming keeps the function served (spec + helper + pair) and
// can never redeclare an existing name.
//
// Red-green: before the fix, the specs below were empty (the function
// was dropped); after the fix, each case yields exactly one spec with
// the renamed helper/pair, and the rewritten AST type-checks cleanly
// (no redeclaration).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestDiskMRCollisionRenamesDeterministically(t *testing.T) {
	src := `package p

func __Parse() int { return 0 }

func Parse(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(b[0]), nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1 (colliding function must be renamed, not dropped)", len(specs))
	}
	if specs[0].helper != "__Parse_mr2" {
		t.Errorf("spec helper = %q, want %q", specs[0].helper, "__Parse_mr2")
	}
	if specs[0].pairType != "__Parse_mr2Pair" {
		t.Errorf("spec pairType = %q, want %q", specs[0].pairType, "__Parse_mr2Pair")
	}
	// The rewritten AST must type-check: the renamed helper/pair do
	// not redeclare the pre-existing __Parse.
	checkDiskMRClean(t, src)
}

func TestDiskMRCollisionSuffixChain(t *testing.T) {
	src := `package p

func __Parse() int { return 0 }

func __Parse_mr2() int { return 0 }

type __Parse_mr2Pair struct{ x int }

func Parse(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(b[0]), nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if specs[0].helper != "__Parse_mr3" {
		t.Errorf("spec helper = %q, want %q", specs[0].helper, "__Parse_mr3")
	}
	if specs[0].pairType != "__Parse_mr3Pair" {
		t.Errorf("spec pairType = %q, want %q", specs[0].pairType, "__Parse_mr3Pair")
	}
	checkDiskMRClean(t, src)
}

func TestDiskMRCollisionMethodRenames(t *testing.T) {
	src := `package p

type T struct{}

func __T_Parse() int { return 0 }

func (t *T) Parse(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	return int(b[0]), nil
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if specs[0].helper != "__T_Parse_mr2" {
		t.Errorf("spec helper = %q, want %q", specs[0].helper, "__T_Parse_mr2")
	}
	if specs[0].pairType != "__T_Parse_mr2Pair" {
		t.Errorf("spec pairType = %q, want %q", specs[0].pairType, "__T_Parse_mr2Pair")
	}
	checkDiskMRClean(t, src)
}
