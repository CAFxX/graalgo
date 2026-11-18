package ir

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// Canonical keys are equal exactly for identical types, across packages.
func TestTypeKeys(t *testing.T) {
	src := map[string]string{
		"a": `package a
type T struct{ x int }
type G[P any] struct{ v P }
type S = struct{ x int }
var V1 struct{ x int }
var V2 struct{ X int }
var V3 struct{ X int "tag" }
var C1 chan (<-chan int)
var C2 chan<- chan int
var I1 interface{ M(); N() int }
var I2 interface{ N() int; M() }
var F1 func(int, ...string) (bool, error)
var F2 func(int, []string) (bool, error)
func L1() any { type T int; return T(0) }
func L2() any { type T int; return T(0) }
`,
		"b": `package b
var V1 struct{ x int }
var V2 struct{ X int }
`,
	}
	fset := token.NewFileSet()
	pkgs := map[string]*types.Package{}
	locals := map[*types.TypeName]string{}
	for _, name := range []string{"a", "b"} {
		f, err := parser.ParseFile(fset, name+".go", src[name], 0)
		if err != nil {
			t.Fatal(err)
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
		p, err := (&types.Config{Importer: importer.Default()}).Check("x/"+name, fset, []*ast.File{f}, info)
		if err != nil {
			t.Fatal(err)
		}
		pkgs[name] = p
		n := 0
		for id, obj := range info.Defs {
			if tn, ok := obj.(*types.TypeName); ok && tn.Parent() != p.Scope() && !isTypeParam(tn) {
				locals[tn] = tn.Name() + "·" + string(rune('0'+n))
				_ = id
				n++
			}
		}
	}
	k := &TypeKeyer{LocalName: func(tn *types.TypeName) string { return locals[tn] }, TypeParamOwner: func(*types.TypeParam) string { return "x/a.G" }}
	typ := func(pkg, name string) types.Type { return pkgs[pkg].Scope().Lookup(name).Type() }
	key := func(t types.Type) string { return k.Key(t) }
	same := [][2]types.Type{
		{typ("a", "S"), typ("a", "V1")},
		{typ("a", "I1"), typ("a", "I2")},
	}
	for _, p := range same {
		if !types.Identical(p[0], p[1]) || key(p[0]) != key(p[1]) {
			t.Errorf("identical types %s, %s: keys %q, %q", p[0], p[1], key(p[0]), key(p[1]))
		}
	}
	different := [][2]types.Type{
		{typ("a", "V1"), typ("b", "V1")}, // unexported field of different packages
		{typ("a", "V2"), typ("a", "V3")}, // tag
		{typ("a", "C1"), typ("a", "C2")},
		{typ("a", "F1"), typ("a", "F2")}, // variadic
		{typ("a", "T"), typ("a", "V1")},
	}
	var localTs []types.Type
	for tn := range locals {
		localTs = append(localTs, tn.Type())
	}
	if len(localTs) != 2 {
		t.Fatalf("%d local types", len(localTs))
	}
	different = append(different, [2]types.Type{localTs[0], localTs[1]})
	for _, p := range different {
		if types.Identical(p[0], p[1]) || key(p[0]) == key(p[1]) {
			t.Errorf("different types %s, %s: keys %q, %q", p[0], p[1], key(p[0]), key(p[1]))
		}
	}
	if got := key(typ("b", "V2")); got != "struct { X int }" {
		t.Errorf("key = %q", got)
	}
	if got := key(typ("a", "V1")); got != "struct { x/a.x int }" {
		t.Errorf("key = %q", got)
	}
	g := typ("a", "G").(*types.Named)
	inst, err := types.Instantiate(nil, g, []types.Type{types.Typ[types.String]}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := key(inst); got != "x/a.G[string]" {
		t.Errorf("instantiated key = %q", got)
	}
}

func isTypeParam(tn *types.TypeName) bool {
	_, ok := tn.Type().(*types.TypeParam)
	return ok
}

// Constraint interfaces: keys are equal exactly when go/types says the
// interfaces are identical (same type set: comparable flag, normalized
// terms, methods), however the type set is spelled.
func TestConstraintKeys(t *testing.T) {
	src := `package c
type Num interface{ ~int | ~float64 }
type IntOnly interface{ ~int }
type Str interface{ String() string }
type MyInt int
func F[
	P0 interface{},
	P1 interface{ ~int | ~float64 },
	P2 interface{ ~float64 | ~int },
	P3 interface{ Num },
	P4 interface{ Num; IntOnly },
	P5 interface{ ~int },
	P6 interface{ ~int; int },
	P7 interface{ int },
	P8 interface{ MyInt | int },
	P9 interface{ comparable },
	P10 interface{ comparable; Str },
	P11 interface{ Str },
	P12 interface{ Num; comparable },
	P13 interface{ int; string },
	P14 interface{ int8; string },
	P15 interface{ Num | string },
	P16 interface{ ~string | ~int | ~float64 },
	P17 interface{ ~[]byte | ~string },
	P18 interface{ Num; Str },
	P19 interface{ ~int | ~float64; String() string },
]() {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "c.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	if _, err := (&types.Config{}).Check("c", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	var ifaces []types.Type
	pkg, _ := (&types.Config{}).Check("c", fset, []*ast.File{f}, info)
	tps := pkg.Scope().Lookup("F").Type().(*types.Signature).TypeParams()
	for i := range tps.Len() {
		ifaces = append(ifaces, tps.At(i).Constraint().Underlying())
	}
	k := &TypeKeyer{TypeParamOwner: func(*types.TypeParam) string { return "c.F" }}
	for i, a := range ifaces {
		for _, b := range ifaces[i+1:] {
			same := k.Key(a) == k.Key(b)
			if same != types.Identical(a, b) {
				t.Errorf("%s vs %s: keys %q, %q; identical %v", a, b, k.Key(a), k.Key(b), types.Identical(a, b))
			}
		}
	}
}
