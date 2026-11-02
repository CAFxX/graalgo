package main

// Lane F (review 2026-11-02 R12): type-parameter substitution must resolve
// by declaration identity, never by bare name.
//
// go/types gives a generic type's own type parameters and its methods'
// receiver type parameters distinct objects with the same name (Box[T]'s
// T vs the Box[T]-receiver T: different positions, same name). The old
// resolveByName returned the innermost frame's binding for a name,
// which is the wrong binding whenever a chain holds two same-named
// parameters with different bindings — a silently wrong
// monomorphisation. These tests pin the identity behaviour at unit
// level (including the cross-check case: the same declaration observed
// through two separate type-checks) and end to end through ExportFiles.

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const typeParamIdentitySrc = `package p

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }
`

// checkTypeParamIdentitySrc type-checks src and returns the package, the
// FileSet, the named type's parameter and the method receiver's parameter.
func checkTypeParamIdentitySrc(t *testing.T, src string) (*types.Package, *token.FileSet, *types.TypeParam, *types.TypeParam) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Instances: make(map[*ast.Ident]types.Instance),
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	boxNamed := pkg.Scope().Lookup("Box").Type().(*types.Named)
	boxTP := boxNamed.TypeParams().At(0)
	var recvTP *types.TypeParam
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "Get" {
			continue
		}
		mo := info.Defs[fd.Name].(*types.Func)
		recvTP = mo.Type().(*types.Signature).RecvTypeParams().At(0)
	}
	if recvTP == nil {
		t.Fatal("Get receiver type parameter not found")
	}
	return pkg, fset, boxTP, recvTP
}

// TestResolveByIdentityShadowedParams pins that two distinct parameters
// both named T resolve to their own frame's binding, not to the
// innermost frame's binding. The old name lookup returned string for
// both queries below; the identity lookup returns int for the outer
// declaration and string for the inner one.
func TestResolveByIdentityShadowedParams(t *testing.T) {
	pkg, fset, boxTP, recvTP := checkTypeParamIdentitySrc(t, typeParamIdentitySrc)
	if boxTP == recvTP {
		t.Fatal("Box T and receiver T are the same object; test premise broken")
	}
	if boxTP.Obj().Name() != "T" || recvTP.Obj().Name() != "T" {
		t.Fatal("test premise broken: parameters are not both named T")
	}
	e := &exporter{pkgFsets: map[*types.Package]*token.FileSet{pkg: fset}}
	// The declaration keys must differ: same name, different positions.
	boxKey, ok := e.typeParamDeclKey(boxTP)
	if !ok {
		t.Fatal("no declaration key for Box T")
	}
	recvKey, ok := e.typeParamDeclKey(recvTP)
	if !ok {
		t.Fatal("no declaration key for receiver T")
	}
	if boxKey == recvKey {
		t.Fatalf("declaration keys collide for distinct parameters: %q", boxKey)
	}
	intT := types.Typ[types.Int]
	strT := types.Typ[types.String]
	// Chain: inner frame binds the receiver T, outer frame binds Box's T.
	chain := &typeSubst{
		parent: &typeSubst{bind: map[*types.TypeParam]types.Type{boxTP: intT}},
		bind:   map[*types.TypeParam]types.Type{recvTP: strT},
	}
	if got := e.resolveByIdentity(chain, boxTP); got != intT {
		t.Errorf("resolveByIdentity(outer T) = %s, want int (inner binding leaked)", got)
	}
	if got := e.resolveByIdentity(chain, recvTP); got != strT {
		t.Errorf("resolveByIdentity(inner T) = %s, want string", got)
	}
	// Unbound: a third T declaration binds to nothing.
	otherPkg := types.NewPackage("p", "p")
	otherTP := types.NewTypeParam(types.NewTypeName(9999, otherPkg, "T", nil), intT)
	if got := e.resolveByIdentity(chain, otherTP); got != nil {
		t.Errorf("resolveByIdentity(unknown T) = %s, want nil (must fail loudly, not bind by name)", got)
	}
}

// TestResolveByIdentityAcrossChecks pins the cross-check tier: the same
// declaration observed through two separate type-checks of identical
// source has different object identity but the same declaration key,
// so a frame built from the first check resolves a query from the
// second. This is the case the old name matching was bridging.
func TestResolveByIdentityAcrossChecks(t *testing.T) {
	pkg1, fset1, boxTP1, _ := checkTypeParamIdentitySrc(t, typeParamIdentitySrc)
	pkg2, fset2, boxTP2, _ := checkTypeParamIdentitySrc(t, typeParamIdentitySrc)
	if boxTP1 == boxTP2 {
		t.Fatal("two checks produced identical TypeParam objects; test premise broken")
	}
	e := &exporter{pkgFsets: map[*types.Package]*token.FileSet{pkg1: fset1, pkg2: fset2}}
	k1, ok1 := e.typeParamDeclKey(boxTP1)
	k2, ok2 := e.typeParamDeclKey(boxTP2)
	if !ok1 || !ok2 {
		t.Fatal("no declaration key for a checked parameter")
	}
	if k1 != k2 {
		t.Fatalf("declaration keys differ across checks of identical source: %q vs %q", k1, k2)
	}
	intT := types.Typ[types.Int]
	chain := &typeSubst{bind: map[*types.TypeParam]types.Type{boxTP1: intT}}
	if got := e.resolveByIdentity(chain, boxTP2); got != intT {
		t.Errorf("resolveByIdentity across checks = %v, want int", got)
	}
}

// TestResolveByIdentityNoPos pins the NoPos fallback: a synthesized
// parameter with no position has no declaration key, so only pointer
// identity resolves it — never a same-named stranger.
func TestResolveByIdentityNoPos(t *testing.T) {
	e := &exporter{}
	pkg := types.NewPackage("p", "p")
	intT := types.Typ[types.Int]
	tp := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), intT)
	if _, ok := e.typeParamDeclKey(tp); ok {
		t.Fatal("NoPos parameter unexpectedly has a declaration key")
	}
	chain := &typeSubst{bind: map[*types.TypeParam]types.Type{tp: intT}}
	if got := e.resolveByIdentity(chain, tp); got != intT {
		t.Errorf("resolveByIdentity(same NoPos object) = %v, want int", got)
	}
	stranger := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), intT)
	if got := e.resolveByIdentity(chain, stranger); got != nil {
		t.Errorf("resolveByIdentity(NoPos stranger) = %s, want nil", got)
	}
}

// exportFuncDeclNames exports src and returns its funcdecl display names.
func exportFuncDeclNames(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFiles([]string{f}, "")
	if err != nil {
		t.Fatalf("ExportFiles failed: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, fd := range findAllKind(doc, "funcdecl") {
		fl, _ := fd["fields"].(map[string]any)
		names = append(names, fmt.Sprintf("%v", fl["name"]))
	}
	return names
}

func funcDeclNamesContain(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestNestedSameNamedTypeParamsExport is the end-to-end R12 case:
// generic types Box[T] and Wrap[T] plus a generic function Use[T] share
// the parameter name T across nested scopes. Every instantiation must
// bind T to int; no instantiation may leak another concrete type.
func TestNestedSameNamedTypeParamsExport(t *testing.T) {
	names := exportFuncDeclNames(t, `package main

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

type Wrap[T any] struct {
	b Box[T]
	w T
}

func (w Wrap[T]) Get() T { return w.w }

func Use[T any](w Wrap[T]) T { return w.Get() }

func main() {
	w := Wrap[int]{b: Box[int]{v: 7}, w: 3}
	println(Use[int](w))
	println(w.Get())
	println(w.b.Get())
}
`)
	for _, want := range []string{"main.Box[int].Get", "main.Wrap[int].Get", "main.Use[int]"} {
		if !funcDeclNamesContain(names, want) {
			t.Errorf("missing instantiation %q in %v", want, names)
		}
	}
	for _, n := range names {
		if strings.Contains(n, "[string]") {
			t.Errorf("misbound instantiation %q: no string binding exists in this program", n)
		}
	}
}

// TestCmpLessIsNaNInstantiation is the P3-M9 motivating case end to end:
// Less[float64] calls isNaN(x) with x: T, so the nested call's type
// argument must resolve T -> float64 (and Compare[int]'s to int).
func TestCmpLessIsNaNInstantiation(t *testing.T) {
	names := exportFuncDeclNames(t, `package main

import "cmp"

func main() {
	println(cmp.Less[float64](1.5, 2.5))
	println(cmp.Compare[int](1, 2))
}
`)
	for _, want := range []string{"cmp.Less[float64]", "cmp.isNaN[float64]", "cmp.Compare[int]", "cmp.isNaN[int]"} {
		if !funcDeclNamesContain(names, want) {
			t.Errorf("missing instantiation %q in %v", want, names)
		}
	}
}

// TestUnboundTypeParamSentinel pins the P2-11 typed error: an
// instantiation whose type arguments are still unbound fails with an
// error that errors.Is-matches errUnboundTypeParam, with the exact
// historical message — while a different error that merely mentions
// the phrase does not match (the old substring classification would
// have treated it as skip-and-mark).
func TestUnboundTypeParamSentinel(t *testing.T) {
	pkg := types.NewPackage("main", "main")
	tp := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), types.Typ[types.Int])
	sig := types.NewSignatureType(nil, nil, []*types.TypeParam{tp},
		types.NewTuple(types.NewParam(token.NoPos, pkg, "x", tp)), nil, false)
	fi := &funcDeclInfo{
		obj: types.NewFunc(token.NoPos, pkg, "F", sig),
		sig: sig,
	}
	_, err := (&exporter{}).newFuncInstantiation(fi, []types.Type{tp}, nil)
	if err == nil {
		t.Fatal("newFuncInstantiation with an unbound type argument succeeded")
	}
	if !errors.Is(err, errUnboundTypeParam) {
		t.Fatalf("error %v does not errors.Is-match errUnboundTypeParam", err)
	}
	want := "gofrontend: unbound type parameter in instantiation of F"
	if err.Error() != want {
		t.Errorf("error string = %q, want %q (message must stay byte-identical)", err.Error(), want)
	}
	lookalike := errors.New("gofrontend: unbound type parameter in some other context")
	if errors.Is(lookalike, errUnboundTypeParam) {
		t.Error("lookalike error errors.Is-matches the sentinel; classification must be by identity, not text")
	}
}
