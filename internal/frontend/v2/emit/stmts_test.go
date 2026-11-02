// Positive tests for the statement kinds the v2 emitter used to reject:
// go, defer, send, switch, type switch, select, and labelled statements
// (R22), plus labelled branches (R21). Each test asserts specific node
// kinds and fields, not just "emission succeeded" — a test that only
// asserted success would pass even if the construct were silently
// dropped, which is exactly the failure mode these tests pin against.
//
// The negative cases these constructs used to occupy live in
// negative_test.go; as each construct was implemented, its case moved
// here.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitDoc runs a source string through the full pipeline and returns the
// emitted document. It fails the test on any stage error.
func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "prog.go")
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

// nodeFields returns the "fields" map of an emitted node, failing the
// test if the value is not a node.
func nodeFields(t *testing.T, n any) map[string]any {
	t.Helper()
	m, ok := n.(map[string]any)
	if !ok {
		t.Fatalf("not a node: %#v", n)
	}
	fields, ok := m["fields"].(map[string]any)
	if !ok {
		t.Fatalf("node has no fields map: %#v", n)
	}
	return fields
}

// funcNode finds a funcdecl node by name in Document.Funcs.
func funcNode(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, f := range doc.Funcs {
		fields := nodeFields(t, f)
		if fields["name"] == name {
			return fields
		}
	}
	t.Fatalf("no funcdecl named %q in %d funcs", name, len(doc.Funcs))
	return nil
}

// bodyStmts returns the statement nodes of a funcdecl's body.
func bodyStmts(t *testing.T, fn map[string]any) []any {
	t.Helper()
	body, ok := fn["body"].(map[string]any)
	if !ok {
		t.Fatalf("funcdecl has no body node: %#v", fn)
	}
	fields := nodeFields(t, body)
	stmts, ok := fields["stmts"].([]any)
	if !ok {
		t.Fatalf("body has no stmts list: %#v", fields)
	}
	return stmts
}

// findKind collects every node of the given kind in the tree rooted at n,
// in traversal order.
func findKind(n any, kind string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["kind"] == kind {
				out = append(out, x)
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(n)
	return out
}

// findOneKind returns the single node of the given kind under n, failing
// the test unless exactly one exists.
func findOneKind(t *testing.T, n any, kind string) map[string]any {
	t.Helper()
	found := findKind(n, kind)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 %q node, found %d", kind, len(found))
	}
	return found[0]
}

// typeEntry resolves a type-table id to its entry map.
func typeEntry(t *testing.T, doc *Document, id any) map[string]any {
	t.Helper()
	i, ok := id.(int)
	if !ok || i < 0 || i >= len(doc.Types) {
		t.Fatalf("bad type id %#v", id)
	}
	m, ok := doc.Types[i].(map[string]any)
	if !ok {
		t.Fatalf("type %d is not an entry: %#v", i, doc.Types[i])
	}
	return m
}

// objectEntry resolves an object-table id to its entry map.
func objectEntry(t *testing.T, doc *Document, id any) map[string]any {
	t.Helper()
	i, ok := id.(int)
	if !ok || i < 0 || i >= len(doc.Objects) {
		t.Fatalf("bad object id %#v", id)
	}
	m, ok := doc.Objects[i].(map[string]any)
	if !ok {
		t.Fatalf("object %d is not an entry: %#v", i, doc.Objects[i])
	}
	return m
}

// guardIdent finds the ident node named name under n, failing unless
// exactly one exists (builtin idents like len carry no obj and are
// filtered out by the name match).
func guardIdent(t *testing.T, n any, name string) map[string]any {
	t.Helper()
	for _, id := range findKind(n, "ident") {
		if nodeFields(t, id)["name"] == name {
			return id
		}
	}
	t.Fatalf("no ident named %q under %#v", name, n)
	return nil
}

func TestGoDeferSend(t *testing.T) {
	doc := emitDoc(t, `package main

func work() {}

func main() {
	go work()
	defer work()
}

func send(c chan int) {
	c <- 1
}
`)
	main := funcNode(t, doc, "main")
	stmts := bodyStmts(t, main)
	if len(stmts) != 2 {
		t.Fatalf("main body has %d stmts, want 2 (go, defer)", len(stmts))
	}
	goNode := nodeFields(t, stmts[0])
	if goNode["call"] == nil {
		t.Fatalf("go node has no call: %#v", goNode)
	}
	callFields := nodeFields(t, goNode["call"])
	if callFields["fun"] == nil {
		t.Fatalf("go call has no fun: %#v", callFields)
	}
	deferNode := nodeFields(t, stmts[1])
	if deferNode["call"] == nil {
		t.Fatalf("defer node has no call: %#v", deferNode)
	}

	sendFn := funcNode(t, doc, "send")
	sendStmts := bodyStmts(t, sendFn)
	if len(sendStmts) != 1 {
		t.Fatalf("send body has %d stmts, want 1", len(sendStmts))
	}
	sendNode := nodeFields(t, sendStmts[0])
	if sendNode["chan"] == nil || sendNode["value"] == nil {
		t.Fatalf("send node lacks chan/value: %#v", sendNode)
	}
}

func TestSwitch(t *testing.T) {
	doc := emitDoc(t, `package main

func f(x int) int {
	switch x {
	case 1:
		return 1
	case 2:
		return 2
	default:
		return 0
	}
}

func g(x int) int {
	y := 0
	switch {
	case x > 0:
		y = 1
	default:
		y = -1
	}
	return y
}

func h() int {
	x := 3
	switch y := x + 1; y {
	case 4:
		return y
	default:
		return 0
	}
}
`)
	// Tagged switch with three clauses, last one default.
	f := funcNode(t, doc, "f")
	sw := findOneKind(t, f["body"], "switch")
	swFields := nodeFields(t, sw)
	if swFields["tag"] == nil {
		t.Fatalf("switch tag is nil, want the tag expression")
	}
	if swFields["init"] != nil {
		t.Fatalf("switch init = %#v, want nil", swFields["init"])
	}
	cases, ok := swFields["body"].([]any)
	if !ok || len(cases) != 3 {
		t.Fatalf("switch body has %d cases, want 3", len(cases))
	}
	last := nodeFields(t, cases[2])
	if last["default"] != true {
		t.Fatalf("last case default = %v, want true", last["default"])
	}
	first := nodeFields(t, cases[0])
	if first["default"] == true {
		t.Fatalf("first case default = true, want false")
	}
	if len(first["list"].([]any)) != 1 {
		t.Fatalf("first case list has %d exprs, want 1", len(first["list"].([]any)))
	}

	// Tagless switch: tag nil, not a fabricated default.
	g := funcNode(t, doc, "g")
	gsw := findOneKind(t, g["body"], "switch")
	if nodeFields(t, gsw)["tag"] != nil {
		t.Fatalf("tagless switch tag = %#v, want nil", nodeFields(t, gsw)["tag"])
	}

	// Switch with init: init is the emitted init statement.
	h := funcNode(t, doc, "h")
	hsw := findOneKind(t, h["body"], "switch")
	if nodeFields(t, hsw)["init"] == nil {
		t.Fatalf("switch init is nil, want the init statement")
	}
}

func TestTypeSwitch(t *testing.T) {
	doc := emitDoc(t, `package main

func f(x any) int {
	switch v := x.(type) {
	case int:
		return v
	case []byte:
		return len(v)
	default:
		return -1
	}
}

func g(x any) int {
	switch x.(type) {
	case string:
		return 1
	default:
		return 0
	}
}

func h(x any) int {
	y := x.(int)
	return y
}

func nm(x any) int {
	switch v := x.(type) {
	case nil:
		return 0
	case int, string:
		_ = v
		return 1
	default:
		return -1
	}
}
`)
	// Guarded switch: guard node carries the variable name and the
	// switched expression; the guard variable itself has no object.
	f := funcNode(t, doc, "f")
	ts := findOneKind(t, f["body"], "typeswitch")
	tsFields := nodeFields(t, ts)
	guard := nodeFields(t, tsFields["guard"])
	if guard["var"] != "v" {
		t.Fatalf("guard var = %v, want v", guard["var"])
	}
	if guard["x"] == nil {
		t.Fatalf("guard has no x expression")
	}
	cases, ok := tsFields["body"].([]any)
	if !ok || len(cases) != 3 {
		t.Fatalf("typeswitch body has %d cases, want 3", len(cases))
	}
	// `case []byte:` is a type expression, not a value expression.
	byteCase := nodeFields(t, cases[1])
	list, ok := byteCase["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("[]byte case list has %d entries, want 1", len(list))
	}
	typeExpr, ok := list[0].(map[string]any)
	if !ok || typeExpr["kind"] != "typeexpr" {
		t.Fatalf("[]byte case entry kind = %v, want typeexpr", typeExpr["kind"])
	}
	if nodeFields(t, typeExpr)["type"] == nil {
		t.Fatalf("[]byte typeexpr has no type id")
	}
	// Default case flagged explicitly.
	if nodeFields(t, cases[2])["default"] != true {
		t.Fatalf("last case default = %v, want true", nodeFields(t, cases[2])["default"])
	}
	// Per-clause guard objects: in `case int:` v has type int; in
	// `case []byte:` v has type []byte. The guard variable has a
	// different object per clause — asserting the object type pins
	// that per-clause resolution, not just the case type list.
	intIdent := guardIdent(t, cases[0], "v")
	intType := typeEntry(t, doc, nodeFields(t, intIdent)["type"])
	if intType["kind"] != "basic" || intType["detail"].(map[string]any)["name"] != "int" {
		t.Fatalf("case int v type = %#v, want basic int", intType)
	}
	byteIdent := guardIdent(t, cases[1], "v")
	byteType := typeEntry(t, doc, nodeFields(t, byteIdent)["type"])
	if byteType["kind"] != "slice" {
		t.Fatalf("case []byte v type kind = %v, want slice", byteType["kind"])
	}
	elem := typeEntry(t, doc, byteType["detail"].(map[string]any)["elem"])
	// go/types names the element "byte" (the alias as written), not
	// "uint8", even though both are the same basic kind.
	if elem["kind"] != "basic" || elem["detail"].(map[string]any)["name"] != "byte" {
		t.Fatalf("case []byte v elem = %#v, want basic byte", elem)
	}

	// `case nil:` is a value, not a type expression: its list entry is
	// an ident resolving to the nil object, never a typeexpr node.
	// `case int, string:` carries one typeexpr per type.
	nm := funcNode(t, doc, "nm")
	nts := findOneKind(t, nm["body"], "typeswitch")
	nmCases, ok := nodeFields(t, nts)["body"].([]any)
	if !ok || len(nmCases) != 3 {
		t.Fatalf("nm typeswitch body has %d cases, want 3", len(nmCases))
	}
	nilCase := nodeFields(t, nmCases[0])
	nilList, ok := nilCase["list"].([]any)
	if !ok || len(nilList) != 1 {
		t.Fatalf("nil case list has %d entries, want 1", len(nilList))
	}
	nilEntry, ok := nilList[0].(map[string]any)
	if !ok || nilEntry["kind"] != "ident" {
		t.Fatalf("nil case entry kind = %v, want ident (a value, not typeexpr)", nilEntry["kind"])
	}
	nilObj := objectEntry(t, doc, nodeFields(t, nilEntry)["obj"])
	if nilObj["kind"] != "nil" {
		t.Fatalf("nil case ident object kind = %v, want nil", nilObj["kind"])
	}
	multiCase := nodeFields(t, nmCases[1])
	multiList, ok := multiCase["list"].([]any)
	if !ok || len(multiList) != 2 {
		t.Fatalf("multi-type case list has %d entries, want 2", len(multiList))
	}
	wantMulti := []string{"int", "string"}
	for i, want := range wantMulti {
		entry, ok := multiList[i].(map[string]any)
		if !ok || entry["kind"] != "typeexpr" {
			t.Fatalf("multi-type case entry %d kind = %v, want typeexpr", i, entry["kind"])
		}
		ty := typeEntry(t, doc, nodeFields(t, entry)["type"])
		if ty["kind"] != "basic" || ty["detail"].(map[string]any)["name"] != want {
			t.Fatalf("multi-type case entry %d type = %#v, want basic %s", i, ty, want)
		}
	}
	if nodeFields(t, nmCases[2])["default"] != true {
		t.Fatalf("nm last case default = %v, want true", nodeFields(t, nmCases[2])["default"])
	}

	// Unguarded switch (`switch x.(type)`): guard var nil.
	g := funcNode(t, doc, "g")
	gts := findOneKind(t, g["body"], "typeswitch")
	if nodeFields(t, nodeFields(t, gts)["guard"])["var"] != nil {
		t.Fatalf("unguarded switch guard var = %v, want nil",
			nodeFields(t, nodeFields(t, gts)["guard"])["var"])
	}

	// Plain assertion: typeassert node with a resolved type.
	h := funcNode(t, doc, "h")
	ta := findOneKind(t, h["body"], "typeassert")
	if nodeFields(t, ta)["type"] == nil {
		t.Fatalf("typeassert has no type id")
	}
	if nodeFields(t, ta)["x"] == nil {
		t.Fatalf("typeassert has no x expression")
	}
}

func TestSelect(t *testing.T) {
	doc := emitDoc(t, `package main

func sel(c chan int, d chan int) {
	select {
	case x := <-c:
		println(x)
	case d <- 2:
	default:
	}
}
`)
	fn := funcNode(t, doc, "sel")
	sel := findOneKind(t, fn["body"], "select")
	selFields := nodeFields(t, sel)
	comms, ok := selFields["body"].([]any)
	if !ok || len(comms) != 3 {
		t.Fatalf("select body has %d clauses, want 3", len(comms))
	}
	// Receive-assign clause.
	recv := nodeFields(t, comms[0])
	if recv["comm"] == nil {
		t.Fatalf("receive clause has no comm statement")
	}
	recvComm, ok := recv["comm"].(map[string]any)
	if !ok || recvComm["kind"] != "assign" {
		t.Fatalf("receive comm kind = %v, want assign", recvComm["kind"])
	}
	// Send clause.
	send := nodeFields(t, comms[1])
	sendComm, ok := send["comm"].(map[string]any)
	if !ok || sendComm["kind"] != "send" {
		t.Fatalf("send comm kind = %v, want send", sendComm["kind"])
	}
	// Default clause: comm nil, flagged default.
	def := nodeFields(t, comms[2])
	if def["comm"] != nil {
		t.Fatalf("default clause comm = %#v, want nil", def["comm"])
	}
	if def["default"] != true {
		t.Fatalf("default clause default = %v, want true", def["default"])
	}
}

func TestLabeledBranches(t *testing.T) {
	doc := emitDoc(t, `package main

func f() {
outer:
	for i := 0; i < 3; i++ {
		if i == 1 {
			continue outer
		}
		if i == 2 {
			break outer
		}
	}
	goto end
end:
	println("done")
}

func g() {
	for {
		break
	}
}
`)
	f := funcNode(t, doc, "f")
	// Two labelled statements: the loop and the trailing println.
	labeled := findKind(f["body"], "labeled")
	if len(labeled) != 2 {
		t.Fatalf("found %d labeled nodes, want 2", len(labeled))
	}
	labels := map[string]bool{}
	for _, l := range labeled {
		labels[nodeFields(t, l)["label"].(string)] = true
	}
	if !labels["outer"] || !labels["end"] {
		t.Fatalf("labeled labels = %v, want outer and end", labels)
	}
	// Branches carry their labels; unlabelled break carries nil.
	branches := findKind(f["body"], "branch")
	if len(branches) != 3 {
		t.Fatalf("found %d branch nodes, want 3 (continue, break, goto)", len(branches))
	}
	want := map[string]string{"continue": "outer", "break": "outer", "goto": "end"}
	for _, b := range branches {
		fields := nodeFields(t, b)
		tok, _ := fields["tok"].(string)
		label, _ := fields["label"].(string)
		if want[tok] != label {
			t.Fatalf("branch %s label = %q, want %q", tok, label, want[tok])
		}
	}

	g := funcNode(t, doc, "g")
	gBranches := findKind(g["body"], "branch")
	if len(gBranches) != 1 {
		t.Fatalf("g has %d branch nodes, want 1", len(gBranches))
	}
	if nodeFields(t, gBranches[0])["label"] != nil {
		t.Fatalf("unlabelled break label = %#v, want nil", nodeFields(t, gBranches[0])["label"])
	}
}
