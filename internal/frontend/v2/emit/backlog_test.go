// Positive tests for the V2-18 backlog constructs.
//
// Each test feeds a program containing one backlog construct through the
// full pipeline (parse → check → emit) and asserts the emitted document
// has the expected node shape — not just that emission didn't fail.
// Every assertion is structural: it fails if the emitter drops,
// mis-emits, or silently accepts the construct.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitDoc runs the full pipeline on src and returns the document.
func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "pos.go")
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
	doc, err := Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// collectKind walks the document and returns every node map with the
// given "kind" (top-level nodes and inline maps like caseclause).
func collectKind(root any, kind string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if k, ok := n["kind"].(string); ok && k == kind {
				out = append(out, n)
			}
			for _, val := range n {
				walk(val)
			}
		case []any:
			for _, val := range n {
				walk(val)
			}
		}
	}
	walk(root)
	return out
}

// fieldsOf returns the "fields" map of a node map.
func fieldsOf(t *testing.T, n map[string]any) map[string]any {
	t.Helper()
	f, ok := n["fields"].(map[string]any)
	if !ok {
		t.Fatalf("node %v has no fields map", n["kind"])
	}
	return f
}

// typeKindOf resolves a type-table id to its "kind" string.
func typeKindOf(t *testing.T, doc *Document, id any) string {
	t.Helper()
	i, ok := id.(int)
	if !ok {
		t.Fatalf("type id %v (%T) is not an int", id, id)
	}
	if i < 0 || i >= len(doc.Types) {
		t.Fatalf("type id %d out of range (have %d types)", i, len(doc.Types))
	}
	tm, ok := doc.Types[i].(map[string]any)
	if !ok {
		t.Fatalf("type %d is not a map", i)
	}
	k, ok := tm["kind"].(string)
	if !ok {
		t.Fatalf("type %d has no kind string", i)
	}
	return k
}

func TestEmptyStmtEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	x := 1
	;
	println(x)
}
`)
	nodes := collectKind(doc.Funcs, "emptystmt")
	if len(nodes) != 1 {
		t.Fatalf("want 1 emptystmt node, got %d", len(nodes))
	}
}

func TestAddrDerefEmit(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	x := 42
	p := &x
	println(*p)
}
`)
	addrs := collectKind(doc.Funcs, "addrof")
	if len(addrs) != 1 {
		t.Fatalf("want 1 addrof node, got %d", len(addrs))
	}
	if typeKindOf(t, doc, fieldsOf(t, addrs[0])["type"]) != "pointer" {
		t.Fatalf("addrof type is not a pointer")
	}
	derefs := collectKind(doc.Funcs, "deref")
	if len(derefs) != 1 {
		t.Fatalf("want 1 deref node, got %d", len(derefs))
	}
	if typeKindOf(t, doc, fieldsOf(t, derefs[0])["type"]) != "basic" {
		t.Fatalf("deref type is not the int basic type")
	}
	// &x must not be smuggled through as a generic unary op.
	for _, u := range collectKind(doc.Funcs, "unary") {
		if op, _ := fieldsOf(t, u)["op"].(string); op == "&" {
			t.Fatalf("addrof leaked through as unary \"&\"")
		}
	}
}

func TestDerefAssignLHS(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	x := 42
	p := &x
	*p = 7
	println(x)
}
`)
	assigns := collectKind(doc.Funcs, "assign")
	if len(assigns) == 0 {
		t.Fatalf("no assign nodes emitted")
	}
	found := false
	for _, a := range assigns {
		lhs, _ := fieldsOf(t, a)["lhs"].([]any)
		if len(lhs) == 1 {
			if lm, ok := lhs[0].(map[string]any); ok && lm["kind"] == "deref" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no assign with deref LHS emitted")
	}
}

func TestFuncLitEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	f := func(x int) int { return x * 2 }
	println(f(21))
}
`)
	lits := collectKind(doc.Funcs, "funclit")
	if len(lits) != 1 {
		t.Fatalf("want 1 funclit node, got %d", len(lits))
	}
	f := fieldsOf(t, lits[0])
	params, _ := f["params"].([]any)
	if len(params) != 1 {
		t.Fatalf("want 1 funclit param, got %d", len(params))
	}
	pm := params[0].(map[string]any)
	if name, _ := fieldsOf(t, pm)["name"].(string); name != "x" {
		t.Fatalf("funclit param name = %q, want x", name)
	}
	if _, ok := f["body"].(map[string]any); !ok {
		t.Fatalf("funclit has no body map")
	}
	if typeKindOf(t, doc, f["type"]) != "signature" {
		t.Fatalf("funclit type is not a signature")
	}
}

func TestFuncLitNamedResults(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	f := func() (y int) { y = 3; return }
	f()
}
`)
	lits := collectKind(doc.Funcs, "funclit")
	if len(lits) != 1 {
		t.Fatalf("want 1 funclit node, got %d", len(lits))
	}
	f := fieldsOf(t, lits[0])
	results, _ := f["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("want 1 named result, got %d", len(results))
	}
	rm := results[0].(map[string]any)
	if rm["kind"] != "result" {
		t.Fatalf("result node kind = %v, want result", rm["kind"])
	}
	if name, _ := fieldsOf(t, rm)["name"].(string); name != "y" {
		t.Fatalf("result name = %q, want y", name)
	}
}

func TestGoDeferEmit(t *testing.T) {
	doc := emitDoc(t, `package main
func f() { println("f") }
func main() {
	go f()
	defer f()
}
`)
	gos := collectKind(doc.Funcs, "gostmt")
	if len(gos) != 1 {
		t.Fatalf("want 1 gostmt node, got %d", len(gos))
	}
	defers := collectKind(doc.Funcs, "defer")
	if len(defers) != 1 {
		t.Fatalf("want 1 defer node, got %d", len(defers))
	}
	for _, n := range append(gos, defers...) {
		call, ok := fieldsOf(t, n)["call"].(map[string]any)
		if !ok {
			t.Fatalf("%v node has no call map", n["kind"])
		}
		if call["kind"] != "call" {
			t.Fatalf("%v.call kind = %v, want call", n["kind"], call["kind"])
		}
	}
}

func TestSendEmit(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	c := make(chan int, 1)
	c <- 1
	println(<-c)
}
`)
	sends := collectKind(doc.Funcs, "send")
	if len(sends) != 1 {
		t.Fatalf("want 1 send node, got %d", len(sends))
	}
	f := fieldsOf(t, sends[0])
	ch, ok := f["chan"].(map[string]any)
	if !ok {
		t.Fatalf("send.chan is not a node map")
	}
	if typeKindOf(t, doc, fieldsOf(t, ch)["type"]) != "chan" {
		t.Fatalf("send.chan type is not a chan")
	}
	if _, ok := f["value"].(map[string]any); !ok {
		t.Fatalf("send.value is not a node map")
	}
	// Receive must be a first-class recv node, not a unary op.
	recvs := collectKind(doc.Funcs, "recv")
	if len(recvs) != 1 {
		t.Fatalf("want 1 recv node, got %d", len(recvs))
	}
	for _, u := range collectKind(doc.Funcs, "unary") {
		if op, _ := fieldsOf(t, u)["op"].(string); op == "<-" {
			t.Fatalf("recv leaked through as unary \"<-\"")
		}
	}
	// The chan int in make(chan int, 1) must emit as a typeexpr node.
	typeexprs := collectKind(doc.Funcs, "typeexpr")
	if len(typeexprs) != 1 {
		t.Fatalf("want 1 typeexpr node, got %d", len(typeexprs))
	}
	if typeKindOf(t, doc, fieldsOf(t, typeexprs[0])["type"]) != "chan" {
		t.Fatalf("typeexpr type is not a chan")
	}
}

func TestChanRangeEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func drain(c chan int) {
	for v := range c {
		println(v)
	}
}
func main() {}
`)
	ranges := collectKind(doc.Funcs, "range")
	if len(ranges) != 1 {
		t.Fatalf("want 1 range node, got %d", len(ranges))
	}
	x := fieldsOf(t, ranges[0])["x"].(map[string]any)
	if typeKindOf(t, doc, fieldsOf(t, x)["type"]) != "chan" {
		t.Fatalf("channel range operand type is not a chan")
	}
}

func TestSwitchEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	x := 2
	switch x {
	case 1:
		println("one")
	case 2, 3:
		println("two")
	default:
		println("other")
	}
}
`)
	sws := collectKind(doc.Funcs, "switch")
	if len(sws) != 1 {
		t.Fatalf("want 1 switch node, got %d", len(sws))
	}
	f := fieldsOf(t, sws[0])
	clauses, _ := f["clauses"].([]any)
	if len(clauses) != 3 {
		t.Fatalf("want 3 caseclauses, got %d", len(clauses))
	}
	wantCases := []int{1, 2, 0}
	for i, c := range clauses {
		cm := c.(map[string]any)
		if cm["kind"] != "caseclause" {
			t.Fatalf("clause %d kind = %v, want caseclause", i, cm["kind"])
		}
		cf := fieldsOf(t, cm)
		cases, _ := cf["cases"].([]any)
		if len(cases) != wantCases[i] {
			t.Fatalf("clause %d has %d case exprs, want %d", i, len(cases), wantCases[i])
		}
		isDefault, _ := cf["default"].(bool)
		if isDefault != (i == 2) {
			t.Fatalf("clause %d default = %v, want %v", i, isDefault, i == 2)
		}
	}
}

func TestSwitchNoTag(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	x := 1
	switch {
	case x > 0:
		println("pos")
	}
}
`)
	sws := collectKind(doc.Funcs, "switch")
	if len(sws) != 1 {
		t.Fatalf("want 1 switch node, got %d", len(sws))
	}
	if tag := fieldsOf(t, sws[0])["tag"]; tag != nil {
		t.Fatalf("tagless switch has non-nil tag %v", tag)
	}
}

func TestTypeSwitchEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	var x any = 42
	switch v := x.(type) {
	case int:
		println(v)
	case string:
		println(v)
	case nil:
		println("nil")
	default:
		println("other")
	}
}
`)
	sws := collectKind(doc.Funcs, "typeswitch")
	if len(sws) != 1 {
		t.Fatalf("want 1 typeswitch node, got %d", len(sws))
	}
	f := fieldsOf(t, sws[0])
	if _, ok := f["tag"].(map[string]any); !ok {
		t.Fatalf("typeswitch has no tag node")
	}
	clauses, _ := f["clauses"].([]any)
	if len(clauses) != 4 {
		t.Fatalf("want 4 typecases, got %d", len(clauses))
	}
	// First clause: bound to int.
	c0 := fieldsOf(t, clauses[0].(map[string]any))
	if c0["bindobj"] == nil {
		t.Fatalf("int clause has no bindobj")
	}
	types0, _ := c0["types"].([]any)
	if len(types0) != 1 {
		t.Fatalf("int clause has %d types, want 1", len(types0))
	}
	if typeKindOf(t, doc, types0[0]) != "basic" {
		t.Fatalf("int clause type is not basic")
	}
	// Nil clause: nilcase flag set.
	c2 := fieldsOf(t, clauses[2].(map[string]any))
	if nilcase, _ := c2["nilcase"].(bool); !nilcase {
		t.Fatalf("nil clause missing nilcase flag")
	}
	// Default clause.
	c3 := fieldsOf(t, clauses[3].(map[string]any))
	if isDefault, _ := c3["default"].(bool); !isDefault {
		t.Fatalf("last clause missing default flag")
	}
}

func TestTypeAssertEmits(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
	var x any = 42
	y := x.(int)
	println(y)
}
`)
	asserts := collectKind(doc.Funcs, "typeassert")
	if len(asserts) != 1 {
		t.Fatalf("want 1 typeassert node, got %d", len(asserts))
	}
	f := fieldsOf(t, asserts[0])
	if typeKindOf(t, doc, f["type"]) != "basic" {
		t.Fatalf("typeassert type is not the int basic type")
	}
	if _, ok := f["x"].(map[string]any); !ok {
		t.Fatalf("typeassert.x is not a node map")
	}
}

func TestLabelsEmit(t *testing.T) {
	doc := emitDoc(t, `package main
func main() {
outer:
	for i := 0; i < 3; i++ {
		if i == 2 {
			break outer
		}
		continue
	}
	goto done
done:
	println("ok")
}
`)
	labeled := collectKind(doc.Funcs, "labeled")
	if len(labeled) != 2 {
		t.Fatalf("want 2 labeled nodes (outer, done), got %d", len(labeled))
	}
	for _, l := range labeled {
		f := fieldsOf(t, l)
		label, _ := f["label"].(string)
		if label != "outer" && label != "done" {
			t.Fatalf("unexpected label %q", label)
		}
		if _, ok := f["stmt"].(map[string]any); !ok {
			t.Fatalf("labeled %q has no stmt map", label)
		}
	}
	branches := collectKind(doc.Funcs, "branch")
	byLabel := map[string]string{}
	for _, b := range branches {
		f := fieldsOf(t, b)
		tok, _ := f["tok"].(string)
		label, _ := f["label"].(string)
		byLabel[tok] = label
	}
	if byLabel["break"] != "outer" {
		t.Fatalf("break label = %q, want outer", byLabel["break"])
	}
	if byLabel["continue"] != "" {
		t.Fatalf("unlabeled continue has label %q", byLabel["continue"])
	}
	if byLabel["goto"] != "done" {
		t.Fatalf("goto label = %q, want done", byLabel["goto"])
	}
}
