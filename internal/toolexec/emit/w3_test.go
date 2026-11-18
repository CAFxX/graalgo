package emit

import (
	"slices"
	"strings"
	"testing"

	"graalgo-toolexec/ir"
)

// Tests for the approximations toolexec-design.md §13 listed for W3. Each
// was written against the W2 emitter and failed there.

// Unnamed struct types with promoted methods get a TYPEDEFS entry
// (underlying 0) carrying both method sets with their embedding paths.
func TestUnnamedStructMethodSet(t *testing.T) {
	f := emitSrc(t, `package p
type In struct{ n int }
func (In) Get() int { return 0 }
func (*In) Set(int) {}
type G interface{ Get() int }
func F() G { return struct{ x int; In }{} }
func H() any { return &struct{ *In }{} }
`)
	found := map[string]ir.TypeDef{}
	for _, td := range f.TypeDefs {
		if td.Underlying == 0 {
			found[typeKey(f, td.Type)] = td
		}
	}
	td, ok := found["struct { p.x int; embed p.In }"]
	if !ok {
		t.Fatalf("no TYPEDEFS entry for struct{x int; In}; have %v", found)
	}
	if len(td.Mset) != 1 || td.Mset[0].Name != "Get" || !slices.Equal(td.Mset[0].Path, []uint64{1}) {
		t.Errorf("value method set: %+v", td.Mset)
	}
	if len(td.PtrMset) != 2 || td.PtrMset[1].Name != "Set" || f.SymRefs[td.PtrMset[1].Method].Name != "(*In).Set" {
		t.Errorf("pointer method set: %+v", td.PtrMset)
	}
	td, ok = found["struct { embed *p.In }"]
	if !ok || len(td.Mset) != 2 || !td.Mset[1].Indirect {
		t.Errorf("struct{*In}: %+v (ok %v)", td, ok)
	}
}

// parameter for conversions of constants to a type parameter).
func TestNoUntypedExpressions(t *testing.T) {
	f := emitSrc(t, `package p
const big = 1 << 40
const yes = 1 < 2
func F[T ~int64 | ~float64](x int, s uint) T {
	println(1, 'a', 2.5, "s", big, yes, 1 < 2, x < 3)
	print(3i)
	y := x << 3
	y <<= 2
	z := 1 << s
	b := x < y
	if x < z && yes {
	}
	_ = b
	var a any = 1 << 3
	_ = a
	switch {
	case x > 1:
	}
	return T(1 << 62)
}
`)
	assertNoUntyped(t, f)
	body := funcBody(t, f, "F")
	for _, b := range nodesOf[*ir.BinaryExpr](body) {
		if b.Op == ir.BinOpShl {
			if c, ok := b.Y.(*ir.ConstExpr); ok && typeKey(f, c.Type) != "uint" {
				t.Errorf("constant shift count has type %s, want uint", typeKey(f, c.Type))
			}
		}
	}
	ret := nodesOf[*ir.ReturnStmt](body)
	if c, ok := ret[0].Results[0].(*ir.ConstExpr); !ok || !strings.HasPrefix(typeKey(f, c.Type), "$p.F.") || c.Value.Text != "4611686018427387904" {
		t.Errorf("T(1<<62): %#v", ret[0].Results[0])
	}
}

func assertNoUntyped(t *testing.T, f *ir.File) {
	t.Helper()
	untyped := func(id ir.TypeID) bool {
		if id == 0 {
			return false
		}
		b, ok := f.Types[id-1].Type.(*ir.BasicType)
		return ok && b.Kind >= ir.BasicKindUntypedBool
	}
	for i, b := range f.Bodies {
		walkNodes(reflectOf(b), func(n any) {
			if x, ok := n.(ir.Expr); ok {
				if id := reflectField(x, "Type"); untyped(id) {
					t.Errorf("%s: %T has untyped type %s", f.Funcs[i].Name, x, typeKey(f, id))
				}
			}
		})
	}
}

// Switch cases: nil takes the tag's type; an interface case against a
// non-interface tag compares the tag converted to the case's type.
func TestSwitchCaseTyping(t *testing.T) {
	f := emitSrc(t, `package p
func F(p *int, x int, a any) int {
	switch p {
	case nil:
		return 1
	}
	switch x {
	case a:
		return 2
	}
	return 0
}
`)
	assertNoUntyped(t, f)
	body := funcBody(t, f, "F")
	sw := nodesOf[*ir.SwitchStmt](body)
	if n, ok := sw[0].Clauses[0].Exprs[0].(*ir.NilExpr); !ok || typeKey(f, n.Type) != "*int" {
		t.Errorf("case nil: %#v", sw[0].Clauses[0].Exprs[0])
	}
	// The second switch compares any(x) == a.
	ok := false
	for _, c := range nodesOf[*ir.ConvertExpr](sw[1]) {
		if c.Implicit && typeKey(f, c.Type) == "interface {}" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("switch x { case a }: tag not converted to the case's interface type")
	}
}

// A type declared in a generic function is a different type per
// instantiation (gc: L[int]() != L[string]() even when T does not mention
// the type parameter), so its key carries the enclosing type parameters.
func TestLocalTypeInGenericKey(t *testing.T) {
	f := emitSrc(t, `package p
func L[X any]() any {
	type T struct{ a int }
	return T{1}
}
func M() any { type U int; return U(1) }
type R[E any] struct{}
func (R[E]) Meth() any { type V struct{ e E }; return V{} }
`)
	for _, want := range []string{"p.T·L·0[$p.L.0]", "p.U·M·0", "p.V·R.Meth·0[$p.R.Meth.0]"} {
		if !hasKey(f, want) {
			t.Errorf("no type key %q; have %v", want, keysContaining(f, "·"))
		}
	}
	for _, td := range f.TypeDefs {
		if typeKey(f, td.Type) == "p.T·L·0[$p.L.0]" && len(td.Tparams) != 1 {
			t.Errorf("local type T in L: typedef tparams %v, want the function's", td.Tparams)
		}
	}
}

// Constraint interfaces carry their normalized type set: interface{ Num }
// is not interface{} (W2 dropped embedded interfaces' terms).
func TestConstraintStructure(t *testing.T) {
	f := emitSrc(t, `package p
type Num interface{ ~int | ~float64 }
func F[T interface{ Num; comparable }](x T) T { return x }
`)
	for _, te := range f.Types {
		if it, ok := te.Type.(*ir.InterfaceType); ok && len(it.Methods) == 0 && te.Key != "interface {}" {
			if te.Key != "interface { ~float64 | ~int }" || len(it.Embeddeds) != 1 {
				t.Errorf("%s: embeddeds %v", te.Key, it.Embeddeds)
			}
			if u, ok := f.Types[it.Embeddeds[0]-1].Type.(*ir.UnionType); !ok || len(u.Terms) != 2 {
				t.Errorf("%s: term list %#v", te.Key, f.Types[it.Embeddeds[0]-1].Type)
			}
		}
	}
}

// for k, v = range x into variables of interface type converts each
// iteration value: lowered to fresh := range + converting assignment.
func TestRangeAssignConversion(t *testing.T) {
	f := emitSrc(t, `package p
func F(m map[string]int) {
	var k, v any
	for k, v = range m {
	}
	_, _ = k, v
}
`)
	body := funcBody(t, f, "F")
	rs := nodesOf[*ir.RangeStmt](body)
	if len(rs) != 1 {
		t.Fatal("no range")
	}
	if !rs[0].Define {
		t.Fatalf("range with conversion not lowered to := form: %+v", rs[0])
	}
	as, ok := rs[0].Body[0].(*ir.AssignStmt)
	if !ok || len(as.Rhs) != 2 {
		t.Fatalf("first body statement: %#v", rs[0].Body[0])
	}
	for _, r := range as.Rhs {
		if c, ok := r.(*ir.ConvertExpr); !ok || !c.Implicit || typeKey(f, c.Type) != "interface {}" {
			t.Errorf("rhs not converted to any: %#v", r)
		}
	}
}

// var a, b I = f() converts element-wise, locally and at package level.
func TestMultiVarConversion(t *testing.T) {
	f := emitSrc(t, `package p
func two() (int, string) { return 1, "a" }
var A, B any = two()
func F() {
	var a, b any = two()
	_, _ = a, b
}
`)
	for _, name := range []string{"F", "init·vars"} {
		if len(nodesOf[*ir.TupleConvertExpr](funcBody(t, f, name))) != 1 {
			t.Errorf("%s: var a, b any = two() has no TupleConvert", name)
		}
	}
}

// x.M() with M a pointer method promoted through a non-pointer embedded
// field takes &x.f: x must be flagged address-taken.
func TestPromotedPtrMethodAddrTaken(t *testing.T) {
	f := emitSrc(t, `package p
type In struct{ n int }
func (i *In) Inc() { i.n++ }
type Out struct{ In }
func F() int {
	var o Out
	o.Inc()
	return o.n
}
`)
	body := funcBody(t, f, "F")
	for _, l := range body.Locals {
		if l.Name == "o" && l.Flags&flagAddrTaken == 0 {
			t.Errorf("o not flagged address-taken")
		}
	}
}

// Conversions of nil are typed nil values.
func TestConvertNil(t *testing.T) {
	f := emitSrc(t, `package p
func F() ([]int, any, *int) { return []int(nil), any(nil), (*int)(nil) }
`)
	assertNoUntyped(t, f)
}

// Call-family nodes are positioned at their '(' — the line gc attributes
// to the call (runtime.Caller, tracebacks; measured on go1.27.1: a call
// split after '(' reports the '(' line, a call whose selector is split
// reports the line of the '(' after the method name). Index and slice
// nodes are at '[', binary operations at the operator (the line of an
// index or divide panic). A function without results that can fall off
// its end ends with a Return at the closing brace (the line a deferred
// call's caller frame reports).
func TestCallPositions(t *testing.T) {
	f := emitSrc(t, `package p

func g(a, b int) int { return a }

type T struct{}

func (T) M(a int) int { return a }

func F(s []int, d int) {
	_ = g(1, // 10
		2)
	_ = g(
		1, 2) // 12 is the '(' line
	_ = T{}.
		M(1) // 15
	_ = s[ // 16
		0]
	_ = 1 / // 18
		d
	println( // 20
		1)
} // 22
`)
	body := funcBody(t, f, "F")
	var calls []int
	for _, c := range nodesOf[*ir.CallExpr](body) {
		calls = append(calls, posLine(body, c.Pos))
	}
	for _, c := range nodesOf[*ir.MethodCallExpr](body) {
		calls = append(calls, posLine(body, c.Pos))
	}
	for _, c := range nodesOf[*ir.BuiltinCallExpr](body) {
		calls = append(calls, posLine(body, c.Pos))
	}
	if !slices.Equal(calls, []int{10, 12, 15, 20}) {
		t.Errorf("call lines %v, want [10 12 15 20]", calls)
	}
	ix := nodesOf[*ir.IndexExpr](body)
	if len(ix) != 1 || posLine(body, ix[0].Pos) != 16 {
		t.Errorf("index line: %v", ix)
	}
	bin := nodesOf[*ir.BinaryExpr](body)
	if len(bin) != 1 || posLine(body, bin[0].Pos) != 18 {
		t.Errorf("binary line: %v", bin)
	}
	last, ok := body.Body[len(body.Body)-1].(*ir.ReturnStmt)
	if !ok || posLine(body, last.Pos) != 22 {
		t.Errorf("no trailing return at the closing brace: %#v", body.Body[len(body.Body)-1])
	}
}

// T.M where M is promoted through an embedded field lowers to a function
// literal that calls the method on the embedded receiver (W2: unsupported).
func TestPromotedMethodExpr(t *testing.T) {
	f := emitSrc(t, `package p
type In struct{ n int }
func (i In) Get(d int) int { return i.n + d }
func (i *In) Ptr() int { return i.n }
type Out struct{ In }
type POut struct{ *In }
type I interface{ Get(int) int }
type HasI struct{ I }
func F() (func(Out, int) int, func(*Out) int, func(POut) int, func(HasI, int) int) {
	return Out.Get, (*Out).Ptr, POut.Ptr, HasI.Get
}
`)
	body := funcBody(t, f, "F")
	lits := nodesOf[*ir.FuncLitExpr](body)
	if len(lits) != 4 {
		t.Fatalf("%d function literals, want 4", len(lits))
	}
	if len(nodesOf[*ir.MethodCallExpr](lits[0].Body)) != 1 || len(nodesOf[*ir.IfaceCallExpr](lits[3].Body)) != 1 {
		t.Errorf("wrappers do not call the method")
	}
}
