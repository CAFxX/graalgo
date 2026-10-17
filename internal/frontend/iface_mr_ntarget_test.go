package main

// P4 (WS D, general interface MR): N-target assignments of multi-result
// interface methods.
//
// The assignment path (exportMultiResultDesugar via classifyMultiResultCall)
// must chain classifyUserIfaceMultiResultCall after the table classifiers,
// mirroring the call-expression path (exportIfaceMethodCall). Without it,
// an N-target assignment like `r, size, err = s.rs.ReadRune()`
// (fmt/scan.go:189, carried by every fmt-importing program) keeps the raw
// pair-typed ifacemethod node as the single RHS, and the backend fails the
// build with "assign.fields: lhs/rhs length mismatch" — the universal
// buildOk=false that turned master red.
//
// Red-green: TestIfaceMRNTargetAssign and TestIfaceMRNTargetDefine fail on
// the base (raw ifacemethod kept as the sole RHS / 3-name 1-value vardecl);
// they pass with the classifier chained.

import (
	"strings"
	"testing"
)

const ifaceMRNTargetSrc = `package main

type RuneReader interface {
	ReadRune() (r rune, size int, err error)
}

type impl struct{}

func (impl) ReadRune() (rune, int, error) { return 'a', 1, nil }

func readAll(rr RuneReader) (r rune, size int, err error) {
	r, size, err = rr.ReadRune()
	return r, size, err
}

func readDefine(rr RuneReader) {
	r, size, err := rr.ReadRune()
	println(r, size, err == nil)
}

func readReturn(rr RuneReader) (rune, int, error) {
	return rr.ReadRune()
}

func main() {
	var rr RuneReader = impl{}
	r, size, err := readAll(rr)
	println(r, size, err == nil)
	readDefine(rr)
	rr2, sz2, err2 := readReturn(rr)
	println(rr2, sz2, err2 == nil)
}
`

// mismatchedAssigns reports assign/vardecl nodes whose target count does
// not match the value count — the wire shape the backend rejects with
// "lhs/rhs length mismatch".
func mismatchedAssigns(doc map[string]any) []string {
	var bad []string
	var walk func(n any)
	walk = func(n any) {
		switch n := n.(type) {
		case map[string]any:
			kind, _ := n["kind"].(string)
			if kind == "assign" || kind == "vardecl" {
				f, _ := n["fields"].(map[string]any)
				var nt, nv int
				if kind == "assign" {
					nt = len(f["lhs"].([]any))
					nv = len(f["rhs"].([]any))
				} else {
					nt = len(f["names"].([]any))
					nv = len(f["values"].([]any))
				}
				if nv == 1 && nt != nv {
					if rhs, ok := f["rhs"].([]any); ok && len(rhs) == 1 {
						if rm, ok := rhs[0].(map[string]any); ok && rm["kind"] == "ifacemethod" {
							bad = append(bad, kind)
						}
					}
					if vals, ok := f["values"].([]any); ok && len(vals) == 1 {
						if vm, ok := vals[0].(map[string]any); ok && vm["kind"] == "ifacemethod" {
							bad = append(bad, kind)
						}
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(doc)
	return bad
}

// desugaredPairTemps counts __p5mr_N pair-temp vardecls holding an
// ifacemethod call — the expected desugar shape.
func desugaredPairTemps(doc map[string]any) int {
	n := 0
	for _, f := range doc["funcs"].([]any) {
		for _, vd := range findAllKind(f, "vardecl") {
			vf := vd["fields"].(map[string]any)
			names, _ := vf["names"].([]any)
			vals, _ := vf["values"].([]any)
			if len(names) == 1 && len(vals) == 1 {
				name, _ := names[0].(string)
				if vm, ok := vals[0].(map[string]any); ok &&
					strings.HasPrefix(name, "__p5mr_") && vm["kind"] == "ifacemethod" {
					n++
				}
			}
		}
	}
	return n
}

func exportIfaceMRNTarget(t *testing.T) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, ifaceMRNTargetSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// TestIfaceMRNTargetAssign: `r, size, err = rr.ReadRune()` (`=` position)
// desugars to a pair temp + field extractions; no raw ifacemethod survives
// as a sole RHS.
func TestIfaceMRNTargetAssign(t *testing.T) {
	doc := exportIfaceMRNTarget(t)
	if bad := mismatchedAssigns(doc); len(bad) > 0 {
		t.Fatalf("found %d mismatched assign/vardecl nodes with raw ifacemethod RHS: %v", len(bad), bad)
	}
	if n := desugaredPairTemps(doc); n < 3 {
		t.Fatalf("want >=3 desugared __p5mr_ ifacemethod pair temps (assign, define, return), got %d", n)
	}
}

// TestIfaceMRNTargetVarDeclStillFailFast: `var r, size, err = rr.ReadRune()`
// (ValueSpec position) keeps the named M8 limitation fail-fast instead of
// emitting a 3-name/1-pair-value vardecl.
func TestIfaceMRNTargetVarDeclStillFailFast(t *testing.T) {
	const src = `package main

type RuneReader interface {
	ReadRune() (r rune, size int, err error)
}

func main() {
	var rr RuneReader
	var r, size, err = rr.ReadRune()
	println(r, size, err == nil)
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected M8 limitation error for var-decl multi-result interface call, got success")
	}
	if !strings.Contains(err.Error(), "var declaration") {
		t.Fatalf("error %q does not mention the var-declaration limitation", err.Error())
	}
}
