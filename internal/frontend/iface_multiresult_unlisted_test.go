package main

// fix-p0-ss-readrune: statement-position multi-result desugar for UNLISTED
// interface methods.
//
// Observed P0: the disk-fmt closure's __ss_ReadRune contained a single
// invalid wire node — assign{lhs:[r,size,err], rhs:[ifacemethod
// (method=ReadRune)]}, 3 lhs / 1 rhs — which the JVM leg rejects with
// "assign.fields: lhs/rhs length mismatch".
//
// Root cause: classifyMultiResultCall's interface-only arm consulted the
// whitelist (classifyIfaceMultiResultCall) and the disk-table classifier
// (classifyDiskIfaceMultiResultCall) but never the P4 general classifier
// (classifyUserIfaceMultiResultCall) — even though the expression-position
// emitter (exportIfaceMethodCall) does. So a multi-result call on an
// unlisted interface method in `=`/`:=` position fell through and emitted
// a raw N-lhs/1-rhs node. The fix routes that arm through
// classifyUserIfaceMultiResultCall as well.
//
// Red-green: on the pre-fix tree the positive tests below fail — the raw
// 3-lhs/1-rhs node trips the arity scan — and pass with the fix. The
// scan mirrors the JVM leg's gate (mismatch rejected unless
// lhs==rhs or the 2-var comma-ok shape), so a green scan is exactly the
// P0's absence.

import (
	"fmt"
	"strings"
	"testing"
)

// wireArityViolations walks the decoded wire doc and reports every assign
// node whose lhs/rhs arities mismatch (other than the 2-var comma-ok
// shape the JVM leg accepts) and every vardecl whose objs/values arities
// mismatch with non-empty values (same comma-ok exclusion: u,ok :=
// err.(T) is 2 objs / 1 commaOk value). The empty string is reported when
// the doc is clean.
func wireArityViolations(doc map[string]any) string {
	var violations []string
	var walk func(n any, funcName string)
	walk = func(n any, funcName string) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if fl, ok := v["fields"].(map[string]any); ok {
					if nm, ok := fl["name"].(string); ok {
						funcName = nm
					}
				}
			}
			f, _ := v["fields"].(map[string]any)
			switch v["kind"] {
			case "assign":
				lhs, _ := f["lhs"].([]any)
				rhs, _ := f["rhs"].([]any)
				if len(lhs) != len(rhs) && !(len(lhs) == 2 && len(rhs) == 1) {
					violations = append(violations,
						fmt.Sprintf("func %s: assign lhs=%d rhs=%d", funcName, len(lhs), len(rhs)))
				}
			case "vardecl":
				objs, _ := f["objs"].([]any)
				vals, _ := f["values"].([]any)
				if len(vals) != 0 && len(objs) != len(vals) && !(len(objs) == 2 && len(vals) == 1) {
					violations = append(violations,
						fmt.Sprintf("func %s: vardecl objs=%d values=%d", funcName, len(objs), len(vals)))
				}
			}
			for _, c := range v {
				walk(c, funcName)
			}
		case []any:
			for _, c := range v {
				walk(c, funcName)
			}
		}
	}
	walk(doc, "")
	return strings.Join(violations, "\n")
}

// assertWireArityClean fails the test when the wire doc contains any
// lhs/rhs or objs/values arity mismatch per wireArityViolations.
func assertWireArityClean(t *testing.T, doc map[string]any) {
	t.Helper()
	if v := wireArityViolations(doc); v != "" {
		t.Fatalf("wire arity violations:\n%s", v)
	}
}

// desugarBlockForMethod finds the desugared block for a multi-result
// interface method call: a block whose first statement is a vardecl
// holding a single ifacemethod value for method, followed by one
// single-target extraction statement per wantName. For `=` positions the
// extractions are assign nodes (lhs ident names); for `:=` they are
// define vardecls (names). Returns the block's statements.
func desugarBlockForMethod(t *testing.T, doc map[string]any, funcName, method string, wantNames []string, wantAssign bool) []any {
	t.Helper()
	for _, f := range doc["funcs"].([]any) {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != funcName {
			continue
		}
		for _, b := range findAllKind(fm, "block") {
			stmts, _ := b["fields"].(map[string]any)["stmts"].([]any)
			if len(stmts) != len(wantNames)+1 {
				continue
			}
			s0, _ := stmts[0].(map[string]any)
			if s0["kind"] != "vardecl" {
				continue
			}
			vals, _ := s0["fields"].(map[string]any)["values"].([]any)
			if len(vals) != 1 {
				continue
			}
			v0, _ := vals[0].(map[string]any)
			if v0["kind"] != "ifacemethod" {
				continue
			}
			if v0["fields"].(map[string]any)["method"] != method {
				continue
			}
			ok := true
			for i, want := range wantNames {
				si, _ := stmts[i+1].(map[string]any)
				sf, _ := si["fields"].(map[string]any)
				if wantAssign {
					if si["kind"] != "assign" {
						ok = false
						break
					}
					lhs, _ := sf["lhs"].([]any)
					rhs, _ := sf["rhs"].([]any)
					if len(lhs) != 1 || len(rhs) != 1 {
						ok = false
						break
					}
					l0, _ := lhs[0].(map[string]any)
					if l0["kind"] != "ident" || l0["fields"].(map[string]any)["name"] != want {
						ok = false
						break
					}
				} else {
					if si["kind"] != "vardecl" {
						ok = false
						break
					}
					names, _ := sf["names"].([]any)
					if len(names) != 1 || names[0] != want {
						ok = false
						break
					}
					if def, _ := sf["define"].(bool); !def {
						ok = false
						break
					}
				}
			}
			if ok {
				return stmts
			}
		}
	}
	kind := "vardecl"
	if wantAssign {
		kind = "assign"
	}
	t.Fatalf("no desugared block for %s in %s: __p5mr_N := ifacemethod(%s) + %d %s extractions %v",
		method, funcName, method, len(wantNames), kind, wantNames)
	return nil
}

// countIfaceMethod counts ifacemethod nodes for method in funcName.
func countIfaceMethod(doc map[string]any, funcName, method string) int {
	n := 0
	for _, f := range doc["funcs"].([]any) {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] != funcName {
			continue
		}
		for _, im := range findAllKind(fm, "ifacemethod") {
			if im["fields"].(map[string]any)["method"] == method {
				n++
			}
		}
	}
	return n
}

const unlistedIfaceSrc = `package main

// RuneReader3 is deliberately NOT in any multi-result whitelist or
// disk table: the P4 general classifier must handle it.
type RuneReader3 interface {
	ReadRune() (rune, int, error)
}

type rrImpl struct{}

func (rrImpl) ReadRune() (rune, int, error) { return 'a', 1, nil }

func main() {
	var r RuneReader3 = rrImpl{}
	var r1 rune
	var size int
	var err error
	r1, size, err = r.ReadRune()
	println(r1, size, err == nil)
}
`

// TestUnlistedIfaceMR3ResultAssign: `r,size,err = iface.ReadRune()` on an
// unlisted 3-result interface method desugars instead of emitting a raw
// 3-lhs/1-rhs assign. Red on the pre-fix tree (arity scan trips), green
// with the fix.
func TestUnlistedIfaceMR3ResultAssign(t *testing.T) {
	doc := d1ExportIfaceProg(t, unlistedIfaceSrc)
	assertWireArityClean(t, doc)
	desugarBlockForMethod(t, doc, "main", "ReadRune",
		[]string{"r1", "size", "err"}, true)
}

// TestUnlistedIfaceMR3ResultDefine: `ch,size,err := iface.ReadRune()`
// desugars into a temp vardecl plus three single-target define vardecls.
func TestUnlistedIfaceMR3ResultDefine(t *testing.T) {
	const src = `package main

type RuneReader3 interface {
	ReadRune() (rune, int, error)
}

type rrImpl struct{}

func (rrImpl) ReadRune() (rune, int, error) { return 'a', 1, nil }

func main() {
	var r RuneReader3 = rrImpl{}
	ch, size, err := r.ReadRune()
	println(ch, size, err == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	assertWireArityClean(t, doc)
	desugarBlockForMethod(t, doc, "main", "ReadRune",
		[]string{"ch", "size", "err"}, false)
}

// TestUnlistedIfaceMRSingleResultUnaffected: a single-result interface
// method call in assignment position is not desugared — no __p5mr_ temp
// appears; the direct 1-name/1-value define carries the ifacemethod node
// itself.
func TestUnlistedIfaceMRSingleResultUnaffected(t *testing.T) {
	const src = `package main

type R interface {
	Read(p []byte) int
}

type bImpl struct{}

func (bImpl) Read(p []byte) int { return len(p) }

func main() {
	var r R = bImpl{}
	buf := make([]byte, 8)
	n := r.Read(buf)
	println(n)
}
`
	doc := d1ExportIfaceProg(t, src)
	assertWireArityClean(t, doc)
	var mainFn map[string]any
	for _, f := range doc["funcs"].([]any) {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] == "main" {
			mainFn = fm
		}
	}
	if mainFn == nil {
		t.Fatal("no main funcdecl in wire doc")
	}
	direct := false
	for _, vd := range findAllKind(mainFn, "vardecl") {
		vf, _ := vd["fields"].(map[string]any)
		names, _ := vf["names"].([]any)
		if len(names) == 1 {
			if nm, _ := names[0].(string); strings.HasPrefix(nm, "__p5mr_") {
				t.Fatalf("single-result interface call was desugared (temp %q present)", nm)
			}
			if names[0] == "n" {
				vals, _ := vf["values"].([]any)
				if len(vals) == 1 {
					if vm, ok := vals[0].(map[string]any); ok && vm["kind"] == "ifacemethod" &&
						vm["fields"].(map[string]any)["method"] == "Read" {
						direct = true
					}
				}
			}
		}
	}
	if !direct {
		t.Fatal("no direct n := <ifacemethod Read> define found in main")
	}
}

// TestUnlistedIfaceMR2ResultDesugar: a 2-result unlisted interface method
// (`n,err = r.Fetch(p)` — Fetch is in no whitelist/table) desugars via
// the new fallback; no arity violations. Red on the pre-fix tree (raw
// 2-lhs/1-rhs assign, no desugar block), green with the fix.
func TestUnlistedIfaceMR2ResultDesugar(t *testing.T) {
	const src = `package main

type Fetcher2 interface {
	Fetch(p []byte) (int, error)
}

type bImpl struct{}

func (bImpl) Fetch(p []byte) (int, error) { return len(p), nil }

func main() {
	var r Fetcher2 = bImpl{}
	buf := make([]byte, 8)
	var n int
	var err error
	n, err = r.Fetch(buf)
	println(n, err == nil)
}
`
	doc := d1ExportIfaceProg(t, src)
	assertWireArityClean(t, doc)
	desugarBlockForMethod(t, doc, "main", "Fetch",
		[]string{"n", "err"}, true)
}

// TestUnlistedIfaceMRBlankTargets: `_,_,_ = iface.ReadRune()` desugars
// and the call is still evaluated (the ifacemethod node survives in the
// temp) — blanks are skipped, not the call.
func TestUnlistedIfaceMRBlankTargets(t *testing.T) {
	const src = `package main

type RuneReader3 interface {
	ReadRune() (rune, int, error)
}

type rrImpl struct{}

func (rrImpl) ReadRune() (rune, int, error) { return 'a', 1, nil }

func main() {
	var r RuneReader3 = rrImpl{}
	_, _, _ = r.ReadRune()
	println("done")
}
`
	doc := d1ExportIfaceProg(t, src)
	assertWireArityClean(t, doc)
	if n := countIfaceMethod(doc, "main", "ReadRune"); n != 1 {
		t.Fatalf("want exactly 1 ifacemethod ReadRune node in main (call evaluated once), got %d", n)
	}
}

// TestSSReadRuneNoArityMismatch: the observed P0 trigger — a program whose
// closure includes disk fmt (Sscan reaches the scan state) must export
// wire with no lhs/rhs or objs/values arity mismatch anywhere, in
// particular inside __ss_ReadRune's `r, size, err = s.rs.ReadRune()`.
func TestSSReadRuneNoArityMismatch(t *testing.T) {
	const src = `package main

import "fmt"

func main() {
	var n int
	fmt.Sscan("42", &n)
	println(n)
}
`
	doc := d1ExportIfaceProg(t, src)
	// The disk-fmt closure must actually contain the shim, or this test
	// is vacuous.
	names := d1FuncIDByName(t, doc)
	found := false
	for _, nm := range names {
		if nm == "__ss_ReadRune" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("wire has no __ss_ReadRune funcdecl: trigger program does not reach the disk-fmt scan state")
	}
	assertWireArityClean(t, doc)
	// The shim's ReadRune call desugared: a temp vardecl holding the
	// ifacemethod, plus three single-target extractions.
	desugarBlockForMethod(t, doc, "__ss_ReadRune", "ReadRune",
		[]string{"r", "size", "err"}, true)
}
