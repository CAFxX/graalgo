package main

// Tests for REVIEW-2026-11-05-05 (review-1105-regexp-methodexpr):
// concrete method expressions T.M in value position.
//
// Previously any program importing regexp/syntax failed frontend export
// with the internal error `identifier "Regexp" denotes *types.TypeName`:
// exportSelector exported x.X (a type name, not an expression) before
// reaching the named concrete-method-expression scope error. The fix
// handles concrete method expressions before x.X is exported and emits
// a "methodexpr" node — an unbound GoFunc whose first call argument is
// the receiver — via the method-value resolution machinery.

import (
	"strings"
	"testing"
)

// TestMethodExprExport: the minimal repro — (*T).Equal in value
// position, bound to a var and called — must export to exactly one
// "methodexpr" node (no "methodvalue": no receiver is bound) with an
// integer obj id and a type id.
func TestMethodExprExport(t *testing.T) {
	src := `package main

type T struct{ n int }

func (t *T) Equal(o *T) bool { return t.n == o.n }

var f = (*T).Equal

func main() {
	_ = f(&T{n: 1}, &T{n: 1})
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with (*T).Equal method expression: %v", err)
	}
	if strings.Contains(out, "denotes") {
		t.Errorf("exported JSON still contains an internal-error marker")
	}
	nodes := findNodes(t, out, "methodexpr")
	if len(nodes) != 1 {
		t.Fatalf("exported JSON holds %d methodexpr nodes, want 1", len(nodes))
	}
	fields := nodeFields(t, nodes[0])
	if _, ok := fields["obj"].(float64); !ok {
		t.Errorf("methodexpr node has no integer obj field: %v", fields)
	}
	if _, ok := fields["type"].(float64); !ok {
		t.Errorf("methodexpr node has no integer type field: %v", fields)
	}
	if _, hasRecv := fields["recv"]; hasRecv {
		t.Errorf("methodexpr node must not carry a bound recv field: %v", fields)
	}
	if len(findNodes(t, out, "methodvalue")) != 0 {
		t.Errorf("exported JSON holds methodvalue nodes for a method expression")
	}
}

// TestMethodExprValueReceiver: a value-receiver method expression
// (T.Eq) exports the same way as the pointer-receiver form.
func TestMethodExprValueReceiver(t *testing.T) {
	src := `package main

type T struct{ n int }

func (t T) Eq(o T) bool { return t.n == o.n }

func main() {
	f := T.Eq
	_ = f(T{n: 1}, T{n: 2})
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with T.Eq method expression: %v", err)
	}
	if len(findNodes(t, out, "methodexpr")) != 1 {
		t.Errorf("exported JSON holds %d methodexpr nodes, want 1", len(findNodes(t, out, "methodexpr")))
	}
}

// TestMethodExprPassedAsArg: the regexp/syntax shape — the method
// expression passed as a function argument (slices.EqualFunc's third
// argument) — must export.
func TestMethodExprPassedAsArg(t *testing.T) {
	src := `package main

type T struct{ n int }

func (t *T) Equal(o *T) bool { return t.n == o.n }

func apply(f func(a, b *T) bool, a, b *T) bool { return f(a, b) }

func main() {
	_ = apply((*T).Equal, &T{n: 1}, &T{n: 1})
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with method expression as call argument: %v", err)
	}
	if len(findNodes(t, out, "methodexpr")) != 1 {
		t.Errorf("exported JSON holds %d methodexpr nodes, want 1", len(findNodes(t, out, "methodexpr")))
	}
}

// TestMethodExprGenericInstantiation: a method expression on a generic
// instantiation ((*Box[int]).Get) resolves through the same
// instantiation machinery as method values.
func TestMethodExprGenericInstantiation(t *testing.T) {
	src := `package main

type Box[T any] struct{ v T }

func (b *Box[T]) Get() T { return b.v }

func main() {
	f := (*Box[int]).Get
	_ = f(&Box[int]{v: 7})
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with Box[int].Get method expression: %v", err)
	}
	if len(findNodes(t, out, "methodexpr")) != 1 {
		t.Errorf("exported JSON holds %d methodexpr nodes, want 1", len(findNodes(t, out, "methodexpr")))
	}
}

// TestMethodExprPromotedScopeError: a promoted method expression
// (Outer.Get, declared on embedded Inner) keeps failing, but with a
// named scope error — the unbound GoFunc would pass the outer value
// where the callee expects the embedded field, so a call-time field
// extraction wrapper is needed (future work).
func TestMethodExprPromotedScopeError(t *testing.T) {
	src := `package main

type Inner struct{ x int }

func (i Inner) Get() int { return i.x }

type Outer struct{ Inner }

func main() {
	f := Outer.Get
	_ = f(Outer{Inner{x: 3}})
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatal("expected promoted method-expression scope error, got success")
	}
	const want = "promoted method (methodexpr scope)"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}
