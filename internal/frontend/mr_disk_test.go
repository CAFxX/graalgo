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

// Regression tests for GAP-GENERIC-MR: the disk multi-result transform
// must support GENERIC multi-result functions. Before the fix, the
// generated __-helper and pair type dropped the type parameter list, so
// any body or field referencing T failed type-checking with
// "undefined: T" (the reported instance was segmentio/encoding/json's
// decodeInto[T any] returning ([]byte, error), called with explicit
// type arguments).
//
// Each test parses a snippet, runs transformDiskMultiResult, and
// type-checks the rewritten AST, asserting zero errors — plus structural
// assertions that the helper and pair type are generic (a type-check
// alone would not catch a silently dropped [T any]).

// transformDiskMR parses src, runs the transform, and returns the
// rewritten file, the specs, and the type errors.
func transformDiskMR(t *testing.T, src string) (*ast.File, []diskMRSpec, []error) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	conf := types.Config{Importer: importer.Default()}
	var errs []error
	conf.Error = func(e error) { errs = append(errs, e) }
	_, _ = conf.Check("example.com/p", fset, []*ast.File{f}, nil)
	return f, specs, errs
}

// findFuncDecl returns the named FuncDecl in f, or nil.
func findFuncDecl(f *ast.File, name string) *ast.FuncDecl {
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// checkDiskMRGenericClean asserts the transform type-checks and that the
// generated helper and pair type carry the type parameter list.
func checkDiskMRGenericClean(t *testing.T, src, funcName, helperName, pairName string, wantTparams int) []diskMRSpec {
	t.Helper()
	f, specs, errs := transformDiskMR(t, src)
	if len(errs) != 0 {
		t.Fatalf("got %d type errors, want 0: %v", len(errs), errs)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	sp := specs[0]
	if sp.name != funcName || sp.helper != helperName || sp.pairType != pairName {
		t.Fatalf("spec = %+v, want name=%s helper=%s pairType=%s", sp, funcName, helperName, pairName)
	}
	helper := findFuncDecl(f, helperName)
	if helper == nil {
		t.Fatalf("helper %s not found in rewritten file", helperName)
	}
	if got := len(tparamNamesOf(helper.Type)); got != wantTparams {
		t.Fatalf("helper %s has %d type params, want %d", helperName, got, wantTparams)
	}
	// The pair type declaration must be generic too.
	foundPair := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts, ok := s.(*ast.TypeSpec)
			if !ok || ts.Name.Name != pairName {
				continue
			}
			foundPair = true
			var got int
			if ts.TypeParams != nil {
				for _, fld := range ts.TypeParams.List {
					got += len(fld.Names)
				}
			}
			if got != wantTparams {
				t.Fatalf("pair type %s has %d type params, want %d", pairName, got, wantTparams)
			}
		}
	}
	if !foundPair {
		t.Fatalf("pair type %s not found in rewritten file", pairName)
	}
	// The stub keeps the original generic multi-result signature.
	stub := findFuncDecl(f, funcName)
	if stub == nil {
		t.Fatalf("stub %s not found in rewritten file", funcName)
	}
	if got := len(tparamNamesOf(stub.Type)); got != wantTparams {
		t.Fatalf("stub %s has %d type params, want %d", funcName, got, wantTparams)
	}
	return specs
}

func TestDiskMRGenericTwoResult(t *testing.T) {
	// The reported shape: decodeInto[T any] returning ([]byte, error),
	// called with explicit type arguments — the segmentio/encoding/json
	// instance of GAP-GENERIC-MR.
	checkDiskMRGenericClean(t, `package p

func DecodeInto[T any](v []byte, dest *T) ([]byte, error) {
	var zero T
	_ = zero
	*dest = zero
	return v, nil
}

func Use() {
	var i int
	rest, err := DecodeInto[int]([]byte{1, 2}, &i)
	_, _ = rest, err
}
`, "DecodeInto", "__DecodeInto", "__DecodeIntoPair", 1)
}

func TestDiskMRGenericBareReturn(t *testing.T) {
	// Named results + bare return in a generic function: the pair
	// literal must carry explicit type arguments (go/types cannot infer
	// them from the literal elements).
	checkDiskMRGenericClean(t, `package p

func First[T any](s []T) (v T, ok bool) {
	if len(s) == 0 {
		return
	}
	v = s[0]
	ok = true
	return
}
`, "First", "__First", "__FirstPair", 1)
}

func TestDiskMRGenericRecursion(t *testing.T) {
	// return F(args) inside F's own generic body: the recursive helper
	// call must forward the type arguments explicitly.
	checkDiskMRGenericClean(t, `package p

func Strip[T any](s []T, n int) ([]T, int) {
	if n <= 0 || len(s) == 0 {
		return s, n
	}
	return Strip[T](s[1:], n-1)
}
`, "Strip", "__Strip", "__StripPair", 1)
}

func TestDiskMRGenericTwoTypeParams(t *testing.T) {
	// Two type parameters: IndexListExpr references on the pair literal.
	checkDiskMRGenericClean(t, `package p

func Zip[A, B any](a []A, b []B) ([]A, []B) {
	return a, b
}

func Use() {
	x, y := Zip[int, string]([]int{1}, []string{"a"})
	_, _ = x, y
}
`, "Zip", "__Zip", "__ZipPair", 2)
}

func TestDiskMRGenericVarHoist(t *testing.T) {
	// var X = id(F[T](args)): the hoisted helper call must keep the
	// explicit type arguments, and the outer call spreads the pair
	// fields.
	f, specs, errs := transformDiskMR(t, `package p

func DecodeInto[T any](v []byte) ([]byte, T) {
	var zero T
	return v, zero
}

func id(b []byte, t int) int { return t }

var X = id(DecodeInto[int]([]byte{1}))
`)
	_ = specs
	if len(errs) != 0 {
		t.Fatalf("got %d type errors, want 0: %v", len(errs), errs)
	}
	// The hoisted var must call __DecodeInto[int], not __DecodeInto.
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 {
				continue
			}
			call, ok := vs.Values[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			if idx, ok := call.Fun.(*ast.IndexExpr); ok {
				if id, ok := idx.X.(*ast.Ident); ok && id.Name == "__DecodeInto" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("hoisted var does not call __DecodeInto[int]")
	}
}

func TestDiskMRGenericMethod(t *testing.T) {
	// A generic METHOD (own type params, non-generic receiver) on a disk
	// type: the helper is generic over the method's type params.
	checkDiskMRGenericClean(t, `package p

type Box struct{ n int }

func (b Box) GetOr[T any](dflt T) (T, bool) {
	if b.n > 0 {
		return dflt, true
	}
	var zero T
	return zero, false
}
`, "GetOr", "__Box_GetOr", "__Box_GetOrPair", 1)
}

func TestDiskMRFuncValueNotRewrittenToHelper(t *testing.T) {
	// A return-position call to a func VALUE (parameter decode of func
	// type decodeFunc) must not be rewritten to a __decode helper call:
	// there is no such helper. The call is kept for the export desugar
	// via the two-value form. (Reported instance: segmentio's
	// decodeFromString returning decode(d, b, p).)
	f, specs, errs := transformDiskMR(t, `package p

type decodeFunc func([]byte) ([]byte, error)

func decodePlain(b []byte) ([]byte, error) {
	return b, nil
}

func wrap(b []byte, decode decodeFunc) ([]byte, error) {
	return decode(b)
}
`)
	if len(errs) != 0 {
		t.Fatalf("got %d type errors, want 0: %v", len(errs), errs)
	}
	if len(specs) != 2 {
		t.Fatalf("specs = %+v, want [decodePlain wrap]", specs)
	}
	helper := findFuncDecl(f, "__wrap")
	if helper == nil {
		t.Fatalf("helper __wrap not found")
	}
	// The rewritten body must not reference __decode.
	foundBad := false
	ast.Inspect(helper.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "__decode" {
			foundBad = true
			return false
		}
		return true
	})
	if foundBad {
		t.Fatalf("rewritten __wrap body references __decode")
	}
	// It must use the two-value form: __mr_t1, __mr_t2 := decode(b).
	foundTwoValue := false
	ast.Inspect(helper.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE && len(as.Lhs) == 2 {
			if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "decode" {
					foundTwoValue = true
					return false
				}
			}
		}
		return true
	})
	if !foundTwoValue {
		t.Fatalf("rewritten __wrap body lacks the two-value func-value call")
	}
}

func TestDiskMRLabeledLoopReturn(t *testing.T) {
	// Returns inside a labeled for loop must be rewritten: the old
	// rewriteStmt did not descend into *ast.LabeledStmt, leaving
	// multi-value returns in the single-result helper body (reported
	// instance: segmentio's parseUintHex with its parseLoop label).
	checkDiskMRClean(t, `package p

func parseHex(b []byte) (uint64, []byte, error) {
	var value uint64
parseLoop:
	for i, c := range b {
		switch {
		case c >= '0' && c <= '9':
			value = value*16 + uint64(c-'0')
		default:
			if i == 0 {
				return 0, b, errHex
			}
			break parseLoop
		}
	}
	return value, b, nil
}

var errHex = error(nil)
`)
}

func TestDiskMRGenericReceiverMethod(t *testing.T) {
	// Methods cannot declare type parameters in Go; a method on a generic
	// receiver uses the receiver type's parameters. The disk MR transform
	// must propagate those to the generated pair struct and __-helper,
	// or the rewritten AST fails with "undefined: V" (reported instance:
	// internal/sync/hashtriemap.go:64 HashTrieMap[K, V].Load).
	// (Failed pre-fix.)
	checkDiskMRClean(t, `package p

type Box[K comparable, V any] struct {
	m map[K]V
}

func (b *Box[K, V]) Load(key K) (value V, ok bool) {
	v, ok := b.m[key]
	return v, ok
}

func (b *Box[K, V]) Store(key K, val V) {
	b.m[key] = val
}

func (b *Box[K, V]) Swap(key K, val V) (old V, swapped bool) {
	old, ok := b.m[key]
	b.m[key] = val
	return old, ok
}
`)
}

func TestDiskMRGenericReceiverMethodValueReceiver(t *testing.T) {
	// Value (non-pointer) generic receiver: same propagation requirement.
	checkDiskMRClean(t, `package p

type Pair[A, B any] struct {
	First  A
	Second B
}

func (p Pair[A, B]) Split() (A, B) {
	return p.First, p.Second
}
`)
}

// ---- Disk MR hoist: switch/for/unhandled-statement regression tests ----
// (REVIEW-2026-11-04 P1: hoistNestedMRInStmt drops switch-case temps,
// hoists for-cond/post temps out of the loop, and ignores TypeSwitch,
// Select, Send, Decl, and Labeled statements.)
//
// Shared snippet decls: f is the multi-result function whose spread call
// h(f()) must be hoisted; h/check are single-result consumers.

const diskMRHoistHelpers = `
func f() (int, string) { return 1, "x" }
func h(a int, b string) int { return a }
func check(a int, b string) bool { return a > 0 }
`

// transformDiskMRFile parses src, runs the disk multi-result transform,
// and returns the rewritten file for AST inspection.
func transformDiskMRFile(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	return f
}

// diskMRFuncBody returns the body of the named function in f.
func diskMRFuncBody(t *testing.T, f *ast.File, name string) *ast.BlockStmt {
	t.Helper()
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == name {
			if fd.Body == nil {
				t.Fatalf("function %s has no body", name)
			}
			return fd.Body
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}

// diskMRFindFor returns the first ForStmt under node, or nil.
func diskMRFindFor(node ast.Node) *ast.ForStmt {
	var found *ast.ForStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if x, ok := n.(*ast.ForStmt); ok {
			found = x
			return false
		}
		return true
	})
	return found
}

// diskMRFindSwitch returns the first SwitchStmt under node, or nil.
func diskMRFindSwitch(node ast.Node) *ast.SwitchStmt {
	var found *ast.SwitchStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if x, ok := n.(*ast.SwitchStmt); ok {
			found = x
			return false
		}
		return true
	})
	return found
}

// diskMRFindTypeSwitch returns the first TypeSwitchStmt under node, or nil.
func diskMRFindTypeSwitch(node ast.Node) *ast.TypeSwitchStmt {
	var found *ast.TypeSwitchStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if x, ok := n.(*ast.TypeSwitchStmt); ok {
			found = x
			return false
		}
		return true
	})
	return found
}

// diskMRFindSelect returns the first SelectStmt under node, or nil.
func diskMRFindSelect(node ast.Node) *ast.SelectStmt {
	var found *ast.SelectStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if x, ok := n.(*ast.SelectStmt); ok {
			found = x
			return false
		}
		return true
	})
	return found
}

// diskMRHasCallTo reports whether node contains a call to the bare
// identifier name (an unrewritten spread call still naming the stub).
func diskMRHasCallTo(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// diskMRTempAssign reports whether stmts contains a hoisted temp
// assignment (__mr_arg_N := __f(...)) at that level.
func diskMRTempAssign(stmts []ast.Stmt) bool {
	for _, st := range stmts {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			continue
		}
		lid, ok := as.Lhs[0].(*ast.Ident)
		if !ok || !strings.HasPrefix(lid.Name, "__mr_arg_") {
			continue
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "__f" {
			return true
		}
	}
	return false
}

// diskMRHasTempSel reports whether node references a hoisted temp
// (__mr_arg_N.fK selector).
func diskMRHasTempSel(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__mr_arg_") {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// diskMRForCondIIFEBody checks that loop.Cond is an immediately-invoked
// func() bool literal and returns its body: the hoisted temps must live
// here (re-evaluated on every iteration check), not before the loop.
func diskMRForCondIIFEBody(t *testing.T, loop *ast.ForStmt) []ast.Stmt {
	t.Helper()
	call, ok := loop.Cond.(*ast.CallExpr)
	if !ok {
		t.Fatalf("for cond is %T, want IIFE call", loop.Cond)
	}
	lit, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatal("for cond is not an immediately-invoked func literal (temp would be evaluated once before the loop)")
	}
	return lit.Body.List
}

// diskMRForPostIIFEBody checks that loop.Post is an immediately-invoked
// func() literal used as a statement and returns its body: the hoisted
// temps must live here (re-evaluated on every iteration), not before
// the loop. The post slot stays a single statement, so continue/break
// semantics are unchanged.
func diskMRForPostIIFEBody(t *testing.T, loop *ast.ForStmt) []ast.Stmt {
	t.Helper()
	es, ok := loop.Post.(*ast.ExprStmt)
	if !ok {
		t.Fatalf("for post is %T, want IIFE ExprStmt", loop.Post)
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		t.Fatalf("for post expr is %T, want IIFE call", es.X)
	}
	lit, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatal("for post is not an immediately-invoked func literal (temp would be evaluated once before the loop)")
	}
	return lit.Body.List
}

func TestDiskMRHoistSwitchCaseBody(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use(x int) int {
	switch x {
	case 1:
		y := h(f())
		return y
	default:
		return 0
	}
}
`
	// Pre-fix this fails to type-check: the case-body temp assignment
	// is dropped while the rewritten args still reference it.
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	sw := diskMRFindSwitch(diskMRFuncBody(t, f, "use"))
	if sw == nil {
		t.Fatal("no SwitchStmt found")
	}
	var caseBody []ast.Stmt
	for _, cc := range sw.Body.List {
		if c, ok := cc.(*ast.CaseClause); ok && len(c.List) > 0 {
			caseBody = c.Body
		}
	}
	if !diskMRTempAssign(caseBody) {
		t.Error("switch case body has no hoisted temp assignment")
	}
	if diskMRHasCallTo(&ast.BlockStmt{List: caseBody}, "f") {
		t.Error("switch case body still calls stub f")
	}
	if !diskMRHasTempSel(&ast.BlockStmt{List: caseBody}) {
		t.Error("switch case body has no temp selector use")
	}
}

func TestDiskMRHoistForCondPerIteration(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use() int {
	n := 0
	for check(f()) {
		n++
		if n > 3 {
			break
		}
	}
	return n
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if diskMRTempAssign(body.List) {
		t.Error("for-cond temp hoisted before the loop (evaluated once, stale on later iterations)")
	}
	loop := diskMRFindFor(body)
	if loop == nil {
		t.Fatal("no ForStmt found")
	}
	iifeBody := diskMRForCondIIFEBody(t, loop)
	if !diskMRTempAssign(iifeBody) {
		t.Error("for-cond IIFE body has no hoisted temp assignment")
	}
	if diskMRHasCallTo(&ast.BlockStmt{List: iifeBody}, "f") {
		t.Error("for-cond IIFE body still calls stub f")
	}
}

func TestDiskMRHoistForPostPerIteration(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use() int {
	s := 0
	for i := 0; i < 3; s = h(f()) {
		_ = i
	}
	return s
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if diskMRTempAssign(body.List) {
		t.Error("for-post temp hoisted before the loop (evaluated once, stale on later iterations)")
	}
	loop := diskMRFindFor(body)
	if loop == nil {
		t.Fatal("no ForStmt found")
	}
	iifeBody := diskMRForPostIIFEBody(t, loop)
	if !diskMRTempAssign(iifeBody) {
		t.Error("for-post IIFE body has no hoisted temp assignment")
	}
	if diskMRHasCallTo(&ast.BlockStmt{List: iifeBody}, "f") {
		t.Error("for-post IIFE body still calls stub f")
	}
	foundPost := false
	for _, st := range iifeBody {
		if _, ok := st.(*ast.AssignStmt); ok && !diskMRTempAssign([]ast.Stmt{st}) {
			foundPost = true
		}
	}
	if !foundPost {
		t.Error("for-post IIFE body lost the original post statement")
	}
}

func TestDiskMRHoistForPostExprStmt(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use() int {
	s := 0
	for i := 0; i < 3; h(f()) {
		_ = i
		s++
	}
	return s
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if diskMRTempAssign(body.List) {
		t.Error("for-post temp hoisted before the loop (evaluated once, stale on later iterations)")
	}
	loop := diskMRFindFor(body)
	if loop == nil {
		t.Fatal("no ForStmt found")
	}
	iifeBody := diskMRForPostIIFEBody(t, loop)
	if !diskMRTempAssign(iifeBody) {
		t.Error("for-post IIFE body has no hoisted temp assignment")
	}
	if diskMRHasCallTo(&ast.BlockStmt{List: iifeBody}, "f") {
		t.Error("for-post IIFE body still calls stub f")
	}
}

func TestDiskMRHoistTypeSwitchCaseBody(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use(v any) int {
	switch v.(type) {
	case int:
		y := h(f())
		return y
	default:
		return 0
	}
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	ts := diskMRFindTypeSwitch(diskMRFuncBody(t, f, "use"))
	if ts == nil {
		t.Fatal("no TypeSwitchStmt found")
	}
	var caseBody []ast.Stmt
	for _, cc := range ts.Body.List {
		if c, ok := cc.(*ast.CaseClause); ok && len(c.List) > 0 {
			caseBody = c.Body
		}
	}
	if !diskMRTempAssign(caseBody) {
		t.Error("typeswitch case body has no hoisted temp assignment")
	}
	if diskMRHasCallTo(&ast.BlockStmt{List: caseBody}, "f") {
		t.Error("typeswitch case body still calls stub f")
	}
	if !diskMRHasTempSel(&ast.BlockStmt{List: caseBody}) {
		t.Error("typeswitch case body has no temp selector use")
	}
}

func TestDiskMRHoistSelect(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use(ch chan int, done chan int) int {
	select {
	case v := <-ch:
		_ = v
		y := h(f())
		return y
	case done <- h(f()):
		return 1
	default:
		return 0
	}
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	sel := diskMRFindSelect(body)
	if sel == nil {
		t.Fatal("no SelectStmt found")
	}
	// Send-clause operands are evaluated once on entering the select,
	// so the send temp legitimately sits before the select.
	if !diskMRTempAssign(body.List) {
		t.Error("select send-clause temp missing before the select")
	}
	for _, cc := range sel.Body.List {
		c, ok := cc.(*ast.CommClause)
		if !ok {
			continue
		}
		switch comm := c.Comm.(type) {
		case *ast.SendStmt:
			if diskMRHasCallTo(comm, "f") {
				t.Error("select send clause still calls stub f")
			}
			if !diskMRHasTempSel(comm) {
				t.Error("select send clause value has no temp selector")
			}
		case *ast.AssignStmt:
			// Receive clause: body hoisted in place.
			if !diskMRTempAssign(c.Body) {
				t.Error("select receive-clause body has no hoisted temp assignment")
			}
			if diskMRHasCallTo(&ast.BlockStmt{List: c.Body}, "f") {
				t.Error("select receive-clause body still calls stub f")
			}
		}
	}
}

func TestDiskMRHoistSendStmt(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use(ch chan int) {
	ch <- h(f())
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if !diskMRTempAssign(body.List) {
		t.Error("send statement has no hoisted temp assignment before it")
	}
	if diskMRHasCallTo(body, "f") {
		t.Error("send statement still calls stub f")
	}
	if !diskMRHasTempSel(body) {
		t.Error("send statement value has no temp selector")
	}
}

func TestDiskMRHoistDeclStmt(t *testing.T) {
	src := `package p
` + diskMRHoistHelpers + `
func use() int {
	var y = h(f())
	return y
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if !diskMRTempAssign(body.List) {
		t.Error("var decl has no hoisted temp assignment before it")
	}
	if diskMRHasCallTo(body, "f") {
		t.Error("var decl still calls stub f")
	}
	if !diskMRHasTempSel(body) {
		t.Error("var decl value has no temp selector")
	}
}

func TestDiskMRHoistLabeledStmt(t *testing.T) {
	// Labeled loop (break target): the old default case never
	// descended into the labeled statement at all, so a spread call
	// inside the loop body was never hoisted. The temp must land
	// inside the loop body (per iteration), not before the label.
	// (A spread call in a goto-target labeled simple statement is a
	// separate known limitation, documented on the LabeledStmt case
	// in mr_disk.go: a goto jumping to the label skips any
	// pre-statement, so that shape stays a loud compile error
	// rather than silently miscompiling.)
	src := `package p
` + diskMRHoistHelpers + `
func use() int {
	n := 0
done:
	for n < 3 {
		n++
		if n == 2 {
			break done
		}
		y := h(f())
		_ = y
	}
	return n
}
`
	checkDiskMRClean(t, src)
	f := transformDiskMRFile(t, src)
	body := diskMRFuncBody(t, f, "use")
	if diskMRHasCallTo(body, "f") {
		t.Error("labeled loop body still calls stub f")
	}
	if !diskMRHasTempSel(body) {
		t.Error("labeled loop body has no temp selector")
	}
	loop := diskMRFindFor(body)
	if loop == nil {
		t.Fatal("no ForStmt found")
	}
	if !diskMRTempAssign(loop.Body.List) {
		t.Error("labeled loop body has no hoisted temp assignment inside the loop")
	}
}
