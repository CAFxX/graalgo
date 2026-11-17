// Regression test for net.IP.To4 (P1-B, REVIEW-2026-11-17-09).
//
// The synthetic net source is an embedded string, not compiled by the
// frontend's own `go test` — so this test pins the fixed mechanism in
// the parsed AST, following the existing checkDesugarWiringSynthetic
// pattern (getSyntheticPackage + AST walk):
//
//  1. (IP).To4 exists with the gc signature (IP) -> IP.
//  2. Its body routes the 16-byte decision through the file's canonical
//     __isV4InV6Prefix predicate (the same predicate Mask uses) — the
//     ::ffff:0:0/96 prefix is required, exactly as in gc net/ip.go.
//  3. Its body never accepts an all-zero prefix: no comparison of
//     ip[10] or ip[11] against 0 appears anywhere in it.
//
// (2)+(3) jointly pin the fix: the pre-fix code fails (3) — it contained
// `is4in6 = ip[10] == 0 && ip[11] == 0` — and any reintroduction of the
// all-zero acceptance fails (3) again. A future rewrite that inlines
// gc's check instead of calling the predicate must update this test
// deliberately.
//
// Behavioral equivalence to gc was verified out-of-band by extracting
// the exact To4 code from the embedded source and running it against
// gc net.IP.To4 over a 22-case matrix (4-byte; ::ffff: mapped;
// all-zero prefix incl. ::1.2.3.4, ::, ::1; half prefixes; 6to4;
// wrong lengths; nil): 22/22 match (see
// /var/tmp/review-1709/p1b-to4-diff-POSTFIX.out). The pre-fix code
// failed 3 of those 22 (::1.2.3.4, ::, ::1 all wrongly mapped).
package main

import (
	"go/ast"
	"go/token"
	"testing"
)

func TestIPTo4RequiresMappedPrefix(t *testing.T) {
	sp := getSyntheticPackage("net")
	if sp == nil || sp.pkg == nil {
		t.Fatalf("synthetic package %q not loaded", "net")
	}
	var to4 *ast.FuncDecl
	for _, f := range sp.files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "To4" || fd.Recv == nil {
				continue
			}
			if to4 != nil {
				t.Fatalf("duplicate (IP).To4 method in synthetic net source")
			}
			to4 = fd
		}
	}
	if to4 == nil {
		t.Fatalf("synthetic net.(IP).To4 method missing")
	}

	// 1. gc signature: receiver `ip IP`, single result `IP`.
	if len(to4.Recv.List) != 1 {
		t.Fatalf("To4 has %d receivers, want 1", len(to4.Recv.List))
	}
	recv, ok := to4.Recv.List[0].Type.(*ast.Ident)
	if !ok || recv.Name != "IP" {
		t.Fatalf("To4 receiver type is %T %v, want IP", to4.Recv.List[0].Type, to4.Recv.List[0].Type)
	}
	if to4.Type.Results == nil || len(to4.Type.Results.List) != 1 {
		t.Fatalf("To4 has %d results, want 1", len(to4.Type.Results.List))
	}
	res, ok := to4.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || res.Name != "IP" {
		t.Fatalf("To4 result type is %T %v, want IP", to4.Type.Results.List[0].Type, to4.Type.Results.List[0].Type)
	}

	// 2+3. Walk the body.
	callsPrefixPredicate := false
	acceptsZeroPrefix := false
	ast.Inspect(to4.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "__isV4InV6Prefix" {
				callsPrefixPredicate = true
			}
		}
		bin, ok := n.(*ast.BinaryExpr)
		if ok && bin.Op == token.EQL {
			if isZeroIndexCmp(bin.X, bin.Y) || isZeroIndexCmp(bin.Y, bin.X) {
				acceptsZeroPrefix = true
			}
		}
		return true
	})
	if !callsPrefixPredicate {
		t.Errorf("To4 does not call __isV4InV6Prefix: the ::ffff:0:0/96 prefix requirement must go through the file's canonical predicate (gc net/ip.go)")
	}
	if acceptsZeroPrefix {
		t.Errorf("To4 compares ip[10] or ip[11] against 0: the all-zero prefix (::a.b.c.d) must not map to IPv4")
	}
}

// isZeroIndexCmp reports whether the comparison is `ip[10] == 0` or
// `ip[11] == 0` (in either operand order).
func isZeroIndexCmp(idx, lit ast.Expr) bool {
	ix, ok := idx.(*ast.IndexExpr)
	if !ok {
		return false
	}
	x, ok := ix.X.(*ast.Ident)
	if !ok || x.Name != "ip" {
		return false
	}
	n, ok := ix.Index.(*ast.BasicLit)
	if !ok || (n.Value != "10" && n.Value != "11") {
		return false
	}
	v, ok := lit.(*ast.BasicLit)
	return ok && v.Value == "0"
}
