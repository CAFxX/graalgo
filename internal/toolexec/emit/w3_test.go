package emit

import (
	"slices"
	"strings"
	"testing"

	"graalgo-toolexec/ir"
)

// Tests for the approximations toolexec-design.md §13 listed for W3. Each
// was written against the W2 emitter and failed there.

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
