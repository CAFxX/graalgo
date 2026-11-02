// Regression tests for the 2026-11-02 independent review findings on
// top-level declarations and node emitters (rev251102-v2-emit-decls).
//
// Each test pins one finding; before the fix it fails in the way the
// finding describes (silent drop or loud rejection), after the fix it
// passes. The tests are demonstrably capable of failing: they assert on
// the emitted document structure, not merely on "no error".
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitSrc(t *testing.T, src string) (*Document, error) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return Emit(cres, pres.Pkg, pres.Files)
}

func funcFields(doc *Document, i int) map[string]any {
	m, _ := doc.Funcs[i].(map[string]any)
	f, _ := m["fields"].(map[string]any)
	return f
}

func funcBodyStmts(doc *Document, i int) []any {
	f := funcFields(doc, i)
	body, _ := f["body"].(map[string]any)
	bf, _ := body["fields"].(map[string]any)
	stmts, _ := bf["stmts"].([]any)
	return stmts
}

func stmtFields(s any) map[string]any {
	m, _ := s.(map[string]any)
	f, _ := m["fields"].(map[string]any)
	return f
}

func stmtKind(s any) string {
	m, _ := s.(map[string]any)
	k, _ := m["kind"].(string)
	return k
}

// Issue 1: top-level var/const/type must reach Globals/Types/Objects.
func TestDeclsTopLevelGlobals(t *testing.T) {
	doc, err := emitSrc(t, `package demo

var Counter = 41
var Name string = "x"
const Answer = 42
const (
	First  = iota
	Second
)
type Point struct{ X, Y int }
type Alias = Point

func Get() int { return Counter + Answer }
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Globals) == 0 {
		t.Fatalf("Globals is empty: top-level declarations were dropped")
	}
	// Collect global kinds and names.
	var kinds []string
	var names []string
	for _, g := range doc.Globals {
		m, _ := g.(map[string]any)
		k, _ := m["kind"].(string)
		kinds = append(kinds, k)
		f, _ := m["fields"].(map[string]any)
		if ns, ok := f["names"].([]string); ok {
			names = append(names, ns...)
		}
		if n, ok := f["name"].(string); ok {
			names = append(names, n)
		}
	}
	t.Logf("globals kinds=%v names=%v", kinds, names)
	for _, want := range []string{"Counter", "Name", "Answer", "First", "Second", "Point", "Alias"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("global %q missing from Globals (kinds=%v names=%v)", want, kinds, names)
		}
	}
	// The iota-derived second constant must carry its computed value, not
	// an empty/absent value: First=0, Second=1.
	for _, g := range doc.Globals {
		f := stmtFields(g)
		if ns, ok := f["names"].([]string); ok {
			for i, n := range ns {
				if n == "Second" {
					vals, _ := f["values"].([]any)
					if i >= len(vals) {
						t.Errorf("Second has no const value")
						continue
					}
					vf := stmtFields(vals[i])
					if vf["value"] != "1" {
						t.Errorf("Second const value = %v, want 1", vf["value"])
					}
				}
			}
		}
	}
	if len(doc.Objects) == 0 {
		t.Errorf("Objects table is empty")
	}
}

// Issue 2: methods must carry their receiver.
func TestDeclsMethodReceiver(t *testing.T) {
	doc, err := emitSrc(t, `package demo

type Counter struct{ n int }

func (c Counter) Get() int { return c.n }
func (c *Counter) Inc() { c.n++ }
func Plain() int { return 0 }
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Funcs) != 3 {
		t.Fatalf("funcs = %d, want 3", len(doc.Funcs))
	}
	for i, wantName := range []string{"Get", "Inc", "Plain"} {
		f := funcFields(doc, i)
		if f["name"] != wantName {
			t.Errorf("funcs[%d].name = %v, want %s", i, f["name"], wantName)
		}
		recv, hasRecv := f["recv"]
		if wantName == "Plain" {
			if hasRecv && recv != nil {
				t.Errorf("Plain should have nil recv, got %v", recv)
			}
			continue
		}
		if !hasRecv || recv == nil {
			t.Errorf("%s: missing recv field (method emitted as plain function)", wantName)
			continue
		}
		rm, ok := recv.(map[string]any)
		if !ok {
			t.Errorf("%s: recv is not a map: %T", wantName, recv)
			continue
		}
		if rm["name"] != "c" {
			t.Errorf("%s: recv.name = %v, want c", wantName, rm["name"])
		}
		if _, ok := rm["obj"].(int); !ok {
			t.Errorf("%s: recv.obj missing or not an int: %v", wantName, rm["obj"])
		}
		if rm["type"] == nil {
			t.Errorf("%s: recv.type is nil", wantName)
		}
	}
}

// Issue 2b: an unnamed receiver still gets a recv field (from the
// signature), so the method is distinguishable from a plain function.
func TestDeclsUnnamedReceiver(t *testing.T) {
	doc, err := emitSrc(t, `package demo

type Counter struct{ n int }

func (Counter) Zero() int { return 0 }
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Funcs) != 1 {
		t.Fatalf("funcs = %d, want 1", len(doc.Funcs))
	}
	f := funcFields(doc, 0)
	recv, hasRecv := f["recv"]
	if !hasRecv || recv == nil {
		t.Fatalf("unnamed-receiver method has no recv field")
	}
	rm, _ := recv.(map[string]any)
	if rm["name"] != "" {
		t.Errorf("recv.name = %v, want empty (unnamed)", rm["name"])
	}
	if _, ok := rm["obj"].(int); !ok {
		t.Errorf("recv.obj = %v, want int object id", rm["obj"])
	}
	if rm["type"] == nil {
		t.Errorf("recv.type is nil")
	}
}

// Issue 3a: named results must be emitted so a bare return is meaningful.
func TestDeclsNamedResults(t *testing.T) {
	doc, err := emitSrc(t, `package demo

func Split() (a, b int) {
	a = 1
	b = 2
	return
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	f := funcFields(doc, 0)
	results, _ := f["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results = %d nodes, want 2 (named results dropped)", len(results))
	}
	for i, want := range []string{"a", "b"} {
		rf := stmtFields(results[i])
		if rf["name"] != want {
			t.Errorf("results[%d].name = %v, want %s", i, rf["name"], want)
		}
	}
	// The bare return must still be present (backend needs results to
	// expand it); the emitter's job is to provide the results list.
	stmts := funcBodyStmts(doc, 0)
	last := stmts[len(stmts)-1]
	if stmtKind(last) != "return" {
		t.Errorf("last stmt kind = %s, want return", stmtKind(last))
	}
	if res, _ := stmtFields(last)["results"].([]any); len(res) != 0 {
		t.Errorf("bare return should emit zero explicit results, got %d", len(res))
	}
}

// Issue 3b: unnamed params must be emitted positionally.
func TestDeclsUnnamedParams(t *testing.T) {
	doc, err := emitSrc(t, `package demo

func F(int, string) int { return 0 }
func G(string, int) int { return 0 }
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	f0 := funcFields(doc, 0)
	params, _ := f0["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("F params = %d, want 2 (unnamed params dropped)", len(params))
	}
	if stmtFields(params[0])["name"] != "" {
		t.Errorf("F params[0].name = %v, want empty (unnamed)", stmtFields(params[0])["name"])
	}
	if stmtFields(params[1])["name"] != "" {
		t.Errorf("F params[1].name = %v, want empty (unnamed)", stmtFields(params[1])["name"])
	}
	f1 := funcFields(doc, 1)
	params1, _ := f1["params"].([]any)
	if len(params1) != 2 {
		t.Fatalf("G params = %d, want 2", len(params1))
	}
	if stmtFields(params1[0])["name"] != "" {
		t.Errorf("G params[0].name = %v, want empty (unnamed)", stmtFields(params1[0])["name"])
	}
}

// Issue 4: previously rejected expression kinds must emit.
func TestDeclsNewExpressions(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"star", "package demo\nfunc F(p *int) int { return *p }\n"},
		{"funclit", "package demo\nfunc F() int { f := func(x int) int { return x * 2 }; return f(21) }\n"},
		{"typeassert", "package demo\nfunc F(x any) int { y := x.(int); return y }\n"},
		{"indexlist", "package demo\nfunc pair[A, B any](a A, b B) A { return a }\nfunc F() int { return pair[int, string](1, \"x\") }\n"},
		{"indexgeneric", "package demo\nfunc id[T any](x T) T { return x }\nfunc F() int { return id[int](42) }\n"},
		{"conversion", "package demo\nfunc F(s string) []byte { return []byte(s) }\n"},
		{"conversionNamed", "package demo\ntype MyInt int\nfunc F(x float64) MyInt { return MyInt(x) }\n"},
		{"conversion2", "package demo\nfunc F(x any) int { return int(x.(float64)) }\n"},
	}
	// Note: indexlist uses a two-param generic to exercise
	// IndexListExpr; indexgeneric exercises single-arg IndexExpr.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := emitSrc(t, tc.src)
			if err != nil {
				t.Fatalf("emit %s: %v (was: unsupported expr)", tc.name, err)
			}
		})
	}
}

// Issue 5: previously rejected statements must emit, and branch labels
// must survive.
func TestDeclsNewStatements(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"switch", "package demo\nfunc F(x int) int { switch x { case 1: return 1; default: return 0 } }\n"},
		{"typeswitch", "package demo\nfunc F(x any) int { switch v := x.(type) { case int: return v; default: return 0 } }\n"},
		{"select", "package demo\nfunc F(c chan int) int { select { case x := <-c: return x; default: return 0 } }\n"},
		{"go", "package demo\nfunc G() {}\nfunc F() { go G() }\n"},
		{"defer", "package demo\nfunc F() { defer println(\"x\") }\n"},
		{"send", "package demo\nfunc F(c chan int) { c <- 1 }\n"},
		{"labeled", "package demo\nfunc F() int { outer: for i := 0; i < 3; i++ { break outer }; return 0 }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := emitSrc(t, tc.src)
			if err != nil {
				t.Fatalf("emit %s: %v (was: unsupported stmt)", tc.name, err)
			}
		})
	}
	// Branch label must be present on the break node.
	doc, err := emitSrc(t, `package demo
func F() int {
outer:
	for i := 0; i < 3; i++ {
		break outer
	}
	return 0
}
`)
	if err != nil {
		t.Fatalf("emit labeled: %v", err)
	}
	stmts := funcBodyStmts(doc, 0)
	labeled := stmts[0]
	if stmtKind(labeled) != "labeled" {
		t.Fatalf("first stmt kind = %s, want labeled", stmtKind(labeled))
	}
	lf := stmtFields(labeled)
	if lf["label"] != "outer" {
		t.Errorf("labeled.label = %v, want outer", lf["label"])
	}
	inner, _ := lf["stmt"].(map[string]any)
	inf, _ := inner["fields"].(map[string]any)
	body, _ := inf["body"].(map[string]any)
	bf, _ := body["fields"].(map[string]any)
	innerStmts, _ := bf["stmts"].([]any)
	if len(innerStmts) == 0 {
		t.Fatalf("loop body is empty")
	}
	br := stmtFields(innerStmts[0])
	if br["tok"] != "break" {
		t.Fatalf("inner stmt tok = %v, want break", br["tok"])
	}
	if br["label"] != "outer" {
		t.Errorf("branch.label = %v, want outer (label dropped)", br["label"])
	}
}

// Issue 6: range with = targets and non-ident targets must not be nil.
func TestDeclsRangeAssignTargets(t *testing.T) {
	// = target: previously key stayed nil (Uses, not Defs).
	doc, err := emitSrc(t, `package demo
func F(xs []int) int {
	i := 0
	s := 0
	for i = range xs {
		s += i
	}
	return s
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	stmts := funcBodyStmts(doc, 0)
	var rng map[string]any
	for _, s := range stmts {
		if stmtKind(s) == "range" {
			rng = stmtFields(s)
		}
	}
	if rng == nil {
		t.Fatalf("no range stmt found")
	}
	if rng["key"] == nil {
		t.Errorf("range.key is nil for 'for i = range xs' (Uses target dropped)")
	}
	if _, ok := rng["key"].(int); !ok {
		t.Errorf("range.key = %v (%T), want int object id", rng["key"], rng["key"])
	}
	if rng["keyExpr"] == nil {
		t.Errorf("range.keyExpr is nil for = target")
	}

	// Non-ident targets: index and selector.
	doc2, err := emitSrc(t, `package demo
type S struct{ f int }
func F(xs []int, m map[int]int, s *S) {
	for m[1] = range xs {
	}
	for s.f = range xs {
	}
}
`)
	if err != nil {
		t.Fatalf("emit non-ident range: %v", err)
	}
	stmts2 := funcBodyStmts(doc2, 0)
	if len(stmts2) != 2 {
		t.Fatalf("stmts = %d, want 2 range stmts", len(stmts2))
	}
	for i, s := range stmts2 {
		f := stmtFields(s)
		if f["keyExpr"] == nil {
			t.Errorf("range[%d].keyExpr is nil (non-ident target dropped)", i)
		} else {
			ke, _ := f["keyExpr"].(map[string]any)
			t.Logf("range[%d] keyExpr kind=%v", i, ke["kind"])
		}
	}
	if ke, _ := stmtFields(stmts2[0])["keyExpr"].(map[string]any); ke["kind"] != "index" {
		t.Errorf("m[1] target kind = %v, want index", ke["kind"])
	}
	if ke, _ := stmtFields(stmts2[1])["keyExpr"].(map[string]any); ke["kind"] != "selector" {
		t.Errorf("s.f target kind = %v, want selector", ke["kind"])
	}
}

// Issue 7: multi-spec var blocks must not be positionally zipped.
func TestDeclsMultiSpecVarBlock(t *testing.T) {
	doc, err := emitSrc(t, `package demo
func F() (int, int) {
	var (
		a int
		b = 2
	)
	_ = a
	return b, a
}
func G() (int, int) {
	var a, b = Pair()
	return a, b
}
func Pair() (int, int) { return 3, 4 }
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// F's vardecl: specs must be per-spec, not flattened.
	stmts := funcBodyStmts(doc, 0)
	var vd map[string]any
	for _, s := range stmts {
		if stmtKind(s) == "vardecl" {
			vd = stmtFields(s)
		}
	}
	if vd == nil {
		t.Fatalf("no vardecl found in F")
	}
	specs, _ := vd["specs"].([]any)
	if len(specs) != 2 {
		t.Fatalf("vardecl specs = %d, want 2 (flattened?)", len(specs))
	}
	// Specs are field maps, not nodes (emitValueSpec returns fields).
	s0, _ := specs[0].(map[string]any)
	s1, _ := specs[1].(map[string]any)
	names0, _ := s0["names"].([]string)
	names1, _ := s1["names"].([]string)
	if len(names0) != 1 || names0[0] != "a" {
		t.Errorf("specs[0].names = %v, want [a]", names0)
	}
	if len(names1) != 1 || names1[0] != "b" {
		t.Errorf("specs[1].names = %v, want [b]", names1)
	}
	vals0, _ := s0["values"].([]any)
	vals1, _ := s1["values"].([]any)
	if len(vals0) != 0 {
		t.Errorf("specs[0].values = %d, want 0 (a has no initializer)", len(vals0))
	}
	if len(vals1) != 1 {
		t.Fatalf("specs[1].values = %d, want 1 (b = 2)", len(vals1))
	}
	if bf := stmtFields(vals1[0]); bf["value"] != "2" {
		t.Errorf("specs[1].values[0] = %v, want basiclit 2", bf["value"])
	}
	if s0["type"] == nil {
		t.Errorf("specs[0].type is nil, want int type id")
	}
	// G's var a, b = Pair(): single multi-value RHS must be flagged expand.
	stmtsG := funcBodyStmts(doc, 1)
	var vdG map[string]any
	for _, s := range stmtsG {
		if stmtKind(s) == "vardecl" {
			vdG = stmtFields(s)
		}
	}
	if vdG == nil {
		t.Fatalf("no vardecl found in G")
	}
	specsG, _ := vdG["specs"].([]any)
	if len(specsG) != 1 {
		t.Fatalf("G vardecl specs = %d, want 1", len(specsG))
	}
	sg, _ := specsG[0].(map[string]any)
	namesG, _ := sg["names"].([]string)
	if len(namesG) != 2 {
		t.Fatalf("G specs[0].names = %v, want [a b]", namesG)
	}
	valsG, _ := sg["values"].([]any)
	if len(valsG) != 1 {
		t.Fatalf("G specs[0].values = %d, want 1 call node", len(valsG))
	}
	if sg["expand"] != true {
		t.Errorf("G specs[0].expand = %v, want true (multi-value call must expand to both names)", sg["expand"])
	}
}
