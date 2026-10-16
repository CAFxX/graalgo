package main

// P4 (fix-multival-rhs): exporter tests for multi-valued RHS assignment
// desugaring of method values and method expressions.
//
// Red-green: before the fix, every shape below failed export — a call
// through a variable holding a method value (mv := T{}.M; a, b := mv())
// and a method expression call (a, b := (*T).M(t)) hit the lane's
// fail-fast diagnostic "multi-result call not desugared (classifier
// missed it)", and even a single-result method expression failed with
// `identifier "T" denotes *types.TypeName` (exportMethodCall exported
// sel.X, the type, as the receiver).
//
// The fix is three general mechanisms, none name-keyed:
//  1. mrInfoForFuncValueInit resolves method values (Selections kind
//     MethodVal on a concrete type) via mrPlanForFuncValue, requiring the
//     pair-transformed (mrUser) kind so the value genuinely yields the
//     pair when called.
//  2. classifyMultiResultCall handles Selections kind MethodExpr on
//     concrete types via mrPlanForFuncValue (mrUser only).
//  3. exportMethodCall/methodRefNode take the receiver from Args[0] for
//     method expressions (sel.X is the type), sharing the exact
//     resolution (methodIndex, promotion, instantiation) with MethodVal.
//
// Tests:
//  1. TestMRMethodValueVarDesugar — mv := T{}.M then a, b := mv()
//     desugars to a __p5mr_N pair temp holding the mv() call.
//  2. TestMRMethodValueVarAssign — same through plain = assignment.
//  3. TestMRMethodValueVarThreeResults — 3-result method value.
//  4. TestMRMethodExprDesugar — a, b := (*T).M(t) desugars to a
//     __p5mr_N pair temp holding a methodref call.
//  5. TestMRMethodExprValueReceiver — T.N(t) with a value receiver.
//  6. TestMRMethodExprSingleResult — single-result method expression
//     exports (covers the exportMethodCall change off the MR path).
//  7. TestMRMethodValueTableKindStaysLoud (negative) — a method value of
//     a table-dispatched (non-mrUser) method is NOT linked: the call site
//     fails loud with the classifier-missed diagnostic instead of
//     miscompiling silently.
//  8. TestMRMethodExprInterfaceStaysLoud (negative) — a method expression
//     on an interface receiver still fails loud.

import (
	"strings"
	"testing"
)

// mrPairTempCall finds a __p5mr_N pair-temp vardecl whose initializer is a
// call node, and returns the call's fun node.
func mrPairTempCall(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		return call
	}
	t.Fatal("no __p5mr_N pair-temp vardecl holding a call found")
	return nil
}

func TestMRMethodValueVarDesugar(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type T struct{ v int }

func (t T) M() (int, error) { return t.v, nil }

func main() {
	mv := T{}.M
	a, err := mv()
	println(a, err == nil)
}
`)
	call := mrPairTempCall(t, doc)
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "ident" {
		t.Fatalf("pair-temp call fun kind = %v, want ident", fun["kind"])
	}
	if name := fun["fields"].(map[string]any)["name"]; name != "mv" {
		t.Fatalf("pair-temp call fun name = %v, want mv", name)
	}
}

func TestMRMethodValueVarAssign(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type T struct{ v int }

func (t T) M() (int, error) { return t.v, nil }

func main() {
	mv := T{}.M
	var a int
	var err error
	a, err = mv()
	println(a, err == nil)
}
`)
	call := mrPairTempCall(t, doc)
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "ident" {
		t.Fatalf("pair-temp call fun kind = %v, want ident", fun["kind"])
	}
	if name := fun["fields"].(map[string]any)["name"]; name != "mv" {
		t.Fatalf("pair-temp call fun name = %v, want mv", name)
	}
}

func TestMRMethodValueVarThreeResults(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type T struct{}

func (T) M() (int, int, error) { return 1, 2, nil }

func main() {
	mv := T{}.M
	a, b, err := mv()
	println(a, b, err == nil)
}
`)
	call := mrPairTempCall(t, doc)
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "ident" {
		t.Fatalf("pair-temp call fun kind = %v, want ident", fun["kind"])
	}
	if name := fun["fields"].(map[string]any)["name"]; name != "mv" {
		t.Fatalf("pair-temp call fun name = %v, want mv", name)
	}
}

func TestMRMethodExprDesugar(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type T struct{ v int }

func (t *T) M() (int, error) { return t.v, nil }

func main() {
	t := &T{v: 3}
	a, err := (*T).M(t)
	println(a, err == nil)
}
`)
	call := mrPairTempCall(t, doc)
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "methodref" {
		t.Fatalf("pair-temp call fun kind = %v, want methodref", fun["kind"])
	}
}

func TestMRMethodExprValueReceiver(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type T struct{ v int }

func (t T) N() (int, error) { return t.v, nil }

func main() {
	t := T{v: 4}
	a, err := T.N(t)
	println(a, err == nil)
}
`)
	call := mrPairTempCall(t, doc)
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "methodref" {
		t.Fatalf("pair-temp call fun kind = %v, want methodref", fun["kind"])
	}
}

func TestMRMethodExprSingleResult(t *testing.T) {
	// A single-result method expression exercises the exportMethodCall
	// receiver handling off the multi-result path: before the fix it
	// failed with `identifier "T" denotes *types.TypeName`.
	doc := exportFuncValueProg(t, `package main

type T struct{ v int }

func (t *T) S() int { return t.v }

func main() {
	t := &T{v: 5}
	x := (*T).S(t)
	println(x)
}
`)
	found := false
	for _, n := range findAllKind(doc, "call") {
		fun, _ := n["fields"].(map[string]any)["fun"].(map[string]any)
		if fun != nil && fun["kind"] == "methodref" {
			found = true
		}
	}
	if !found {
		t.Fatal("no methodref call found for single-result method expression")
	}
}

func TestMRMethodValueTableKindStaysLoud(t *testing.T) {
	// (*bytes.Buffer).Read is table-dispatched (mrM7 via __bytesBufferRead),
	// not pair-transformed: a method value of it must NOT be linked as a
	// pair-yielding value. The call site fails loud with the
	// classifier-missed diagnostic instead of miscompiling silently.
	_, err := ExportFile(writeIfaceProg(t, `package main

import "bytes"

func main() {
	var buf bytes.Buffer
	rd := buf.Read
	p := make([]byte, 8)
	n, err := rd(p)
	_, _ = n, err
}
`))
	if err == nil {
		t.Fatal("expected classifier-missed diagnostic for table-method value call, got nil")
	}
	if !strings.Contains(err.Error(), "multi-result call not desugared (classifier missed it)") {
		t.Fatalf("error = %q, want the classifier-missed diagnostic", err.Error())
	}
	if !strings.Contains(err.Error(), "rd") {
		t.Fatalf("error = %q, want it to name the unclassified call", err.Error())
	}
}

func TestMRMethodExprInterfaceStaysLoud(t *testing.T) {
	// A method expression on an interface receiver is out of scope
	// (dynamic dispatch deferred to call time, P3-M3): it must fail loud,
	// naming the method.
	_, err := ExportFile(writeIfaceProg(t, `package main

type RR interface{ ReadRune() (rune, int, error) }

func main() {
	var rs RR
	r, size, err := (*RR).ReadRune(rs)
	_, _, _ = r, size, err
}
`))
	if err == nil {
		t.Fatal("expected loud failure for interface method expression, got nil")
	}
	if !strings.Contains(err.Error(), "ReadRune") {
		t.Fatalf("error = %q, want it to name the method", err.Error())
	}
}
