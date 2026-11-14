package main

// R2314-P1-1 regression: a comma-ok receive in the `=` form whose value
// (or ok) target is not a plain identifier (index, selector, dereference,
// map index) must export — the value is received into a fresh plain temp
// and copied back after the select. The temp type is the position type
// (channel element type for the value, bool for ok); an ok temp typed
// as the element type is a mistyped frame slot and must fail here.
//
// The `:=` form with a non-identifier target is NOT covered as a
// positive case: gc rejects it ("non-name a[0] on left side of :="),
// so TestCommaOkRecvDefineNonIdentRejected pins the rejection instead.
//
// Evaluation-order note: the copy-back runs after the select, which is
// exactly gc's timing for plain-variable operands (gc re-reads a
// variable index/container after the receive); a side-effecting
// non-variable operand (a[f()]) runs before the receive under gc and
// after it here — a known limitation, documented in
// exportRecvCommaOkDesugar, not asserted here.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommaOkRecvNonIdentTargets(t *testing.T) {
	src := `package main

import "fmt"

type box struct{ x int }

var g int

func main() {
	arr := [1]int{}
	b := box{}
	x := 0
	p := &x
	m := map[string]int{}
	buf2 := [1]int{}
	okArr := [1]bool{}
	okArr2 := [1]bool{}
	var ok bool
	v := 0
	w := 0
	var ok2 bool
	ch0 := make(chan int, 1)
	ch0 <- 0
	v, ok2 = <-ch0
	ch1 := make(chan int, 1)
	ch1 <- 1
	arr[0], ok = <-ch1
	ch2 := make(chan int, 1)
	ch2 <- 2
	b.x, ok = <-ch2
	ch3 := make(chan int, 1)
	ch3 <- 3
	*p, ok = <-ch3
	ch4 := make(chan int, 1)
	ch4 <- 4
	m["k"], ok = <-ch4
	ch5 := make(chan int, 1)
	ch5 <- 5
	v, okArr[0] = <-ch5
	ch6 := make(chan int, 1)
	ch6 <- 6
	buf2[0], okArr2[0] = <-ch6
	ch7 := make(chan int, 1)
	ch7 <- 7
	g, ok = <-ch7
	ch8 := make(chan int, 1)
	ch8 <- 8
	w, _ = <-ch8
	fmt.Println(arr[0], b.x, x, m["k"], v, okArr[0], buf2[0], okArr2[0], g, ok, w, ok2)
}
`
	doc := exportSrc(t, src)
	var raw map[string]any
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatal(err)
	}
	// Scope the walk to main: ExportFile also exports the generic
	// instantiations of every imported package (fmt alone pulls in
	// 2000+ funcs), so a whole-document walk would count stdlib
	// selects.
	funcs, _ := raw["funcs"].([]any)
	var mainFn map[string]any
	for _, f := range funcs {
		fm, _ := f.(map[string]any)
		ff, _ := fm["fields"].(map[string]any)
		if ff["name"] == "main" {
			mainFn = fm
			break
		}
	}
	if mainFn == nil {
		t.Fatal("no main func in export")
	}
	// No recv node in main may carry commaOk=true (the desugar must
	// have run). Scoped to main: the whole-doc string "commaOk" also
	// appears on stdlib funcs — the exporter leaves imported
	// packages' comma-ok receives undesugared (31 such nodes on base
	// too, none in main) — and as `"commaOk": false` fields, so a
	// whole-doc Contains would fail on any program.
	var walkCommaOk func(v any)
	walkCommaOk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if b, ok := n["commaOk"].(bool); ok && b {
				t.Fatalf("main still carries a commaOk recv node")
			}
			for _, c := range n {
				walkCommaOk(c)
			}
		case []any:
			for _, c := range n {
				walkCommaOk(c)
			}
		}
	}
	walkCommaOk(mainFn)
	typesRaw, _ := raw["types"].([]any)
	types := make([]map[string]any, len(typesRaw))
	for i, tr := range typesRaw {
		m, _ := tr.(map[string]any)
		types[i] = m
	}
	// Object table: objid -> (name, type id), for temp-type checks
	// (including blanks, which have no copy-back ident to read a
	// type from).
	objsRaw, _ := raw["objects"].([]any)
	objName := map[float64]string{}
	objType := map[float64]int{}
	for i, o := range objsRaw {
		om, _ := o.(map[string]any)
		if om == nil {
			continue
		}
		if n, ok := om["name"].(string); ok {
			objName[float64(i)] = n
		}
		if tid, ok := om["type"].(float64); ok {
			objType[float64(i)] = int(tid)
		}
	}
	// Collect the comma-ok desugars (single-case recv selects with
	// two targets), the copy-backs ("=" assigns whose rhs are all
	// idents), and every vardecl's declared objs.
	type desugar struct {
		targets []any
	}
	var desugars []desugar
	declared := map[any]bool{}
	var copyBacks []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "select" {
				if f, ok := v["fields"].(map[string]any); ok {
					if cases, ok := f["cases"].([]any); ok && len(cases) == 1 {
						if c, ok := cases[0].(map[string]any); ok {
							if cf, ok := c["fields"].(map[string]any); ok && cf["kind"] == "recv" {
								if tgts, ok := cf["targets"].([]any); ok && len(tgts) == 2 {
									desugars = append(desugars, desugar{targets: tgts})
								}
							}
						}
					}
				}
			}
			if v["kind"] == "assign" {
				if f, ok := v["fields"].(map[string]any); ok && f["tok"] == "=" {
					if lhs, ok := f["lhs"].([]any); ok && len(lhs) >= 1 {
						if rhs, ok := f["rhs"].([]any); ok && len(rhs) == len(lhs) {
							allIdent := true
							for _, r := range rhs {
								if rn, ok := r.(map[string]any); !ok || rn["kind"] != "ident" {
									allIdent = false
								}
							}
							if allIdent {
								copyBacks = append(copyBacks, v)
							}
						}
					}
				}
			}
			if v["kind"] == "vardecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if objs, ok := f["objs"].([]any); ok {
						for _, o := range objs {
							declared[o] = true
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(mainFn)
	// 9 comma-ok receives: 1 identifier control + 8 under test.
	if len(desugars) != 9 {
		t.Fatalf("want 9 comma-ok desugars, got %d", len(desugars))
	}
	// Every desugar target must be declared by a dominating vardecl
	// (the p3chan builder requires a frame slot for every target).
	// Identifier targets are declared by the user's own vardecls;
	// temps by the desugar's dominating vardecl.
	for _, d := range desugars {
		for _, tgt := range d.targets {
			if !declared[tgt] {
				t.Errorf("desugar target obj %v is not declared by any vardecl", tgt)
			}
		}
	}
	// Match each copy-back to its desugar by rhs ident obj, and record
	// the copy-back positions per desugar. The source's main has no
	// other "=" assigns with an ident rhs, so every collected assign
	// is a copy-back; a copy-back assign may carry several (lhs, rhs)
	// pairs (the globals branch copies both positions back in one
	// assign node), so pairs are counted individually.
	type pair struct {
		pos     string
		lhsKind string
	}
	var pairs []pair
	for _, cb := range copyBacks {
		f, _ := cb["fields"].(map[string]any)
		lhs, _ := f["lhs"].([]any)
		rhs, _ := f["rhs"].([]any)
		if len(lhs) != len(rhs) {
			t.Errorf("copy-back assign has %d lhs vs %d rhs", len(lhs), len(rhs))
			continue
		}
		for k := range lhs {
			rn, _ := rhs[k].(map[string]any)
			if rn["kind"] != "ident" {
				t.Errorf("copy-back rhs[%d] is %q, want ident", k, rn["kind"])
				continue
			}
			rf, _ := rn["fields"].(map[string]any)
			obj := rf["obj"]
			pos := ""
			for _, d := range desugars {
				for j, tgt := range d.targets {
					if tgt == obj {
						pos = string(rune('0' + j))
						// An ok-position temp must be bool-typed.
						if j == 1 {
							tid, ok := rf["type"].(float64)
							if !ok {
								t.Errorf("ok copy-back temp ident has no numeric type field: %v", rf)
							} else if kind, name := typeKindName(types, int(tid)); kind != "basic" || name != "bool" {
								t.Errorf("ok copy-back temp type = %s %q, want basic bool", kind, name)
							}
						}
					}
				}
			}
			if pos == "" {
				t.Errorf("copy-back rhs obj %v does not reference any desugar target", obj)
				continue
			}
			ln, _ := lhs[k].(map[string]any)
			kind, _ := ln["kind"].(string)
			pairs = append(pairs, pair{pos: pos, lhsKind: kind})
		}
	}
	sigs := map[string]int{}
	seenLhs := map[string]bool{}
	for _, p := range pairs {
		sigs[p.pos]++
		seenLhs[p.lhsKind] = true
	}
	// Expected position counts: value-position copy-backs are the
	// four non-ident values (ch1–ch4) + ch6's value + ch7's global
	// ident = 6; ok-position copy-backs are ch5's okArr[0], ch6's
	// okArr2[0], and ch7's ok (globals branch copies both back) = 3.
	// The control (ch0) and the blank-ok receive (ch8) have none.
	if sigs["0"] != 6 {
		t.Errorf("want 6 value-position copy-backs, got %d (sigs %v)", sigs["0"], sigs)
	}
	if sigs["1"] != 3 {
		t.Errorf("want 3 ok-position copy-backs, got %d (sigs %v)", sigs["1"], sigs)
	}
	if len(pairs) != 9 {
		t.Errorf("want 9 copy-back pairs total, got %d", len(pairs))
	}
	for _, want := range []string{"index", "selector", "deref", "ident"} {
		if !seenLhs[want] {
			t.Errorf("no copy-back with %q lhs (index=arr[0]/m[k], selector=b.x, deref=*p, ident=g)", want)
		}
	}
	// The blank ok temp (w, _ = <-ch8) has no copy-back ident to read
	// a type from: check it via the object table. It must be bool.
	foundBlank := false
	for oid, name := range objName {
		if !strings.HasPrefix(name, "__rcv_blank_") {
			continue
		}
		foundBlank = true
		tid, ok := objType[oid]
		if !ok {
			t.Errorf("blank temp %q has no type in object table", name)
			continue
		}
		if kind, tname := typeKindName(types, tid); kind != "basic" || tname != "bool" {
			t.Errorf("blank ok temp %q type = %s %q, want basic bool", name, kind, tname)
		}
	}
	if !foundBlank {
		t.Errorf("no __rcv_blank_ temp in object table; want one for w, _ = <-ch8")
	}
}

// TestCommaOkRecvDefineNonIdentRejected pins the deliberate scope
// boundary: `a[i], ok := <-ch` is invalid Go — gc rejects it with
// "non-name a[0] on left side of :=" — so the exporter must keep
// rejecting it, not "fix" it into acceptance. The rejection comes
// from go/types (ExportFile type-checks before exporting), not from
// the desugar — the desugar's own scopeErrorf is defense-in-depth.
func TestCommaOkRecvDefineNonIdentRejected(t *testing.T) {
	src := `package main

func main() {
	ch := make(chan int, 1)
	a := []int{0}
	a[0], ok := <-ch
	_, _ = a, ok
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatalf("ExportFile accepted a[i], ok := <-ch; want rejection (gc: non-name on left side of :=)")
	}
	if !strings.Contains(err.Error(), "non-name") {
		t.Fatalf("ExportFile error = %v; want the go/types non-name rejection", err)
	}
}
