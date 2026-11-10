// Regression tests for the review-1110-p1-v2-emit remediation lane
// (independent codebase review, 2026-11-10, findings C-P1-1, C-P1-2,
// C-P1-4: the v2 emitter loses type/object identity).
//
// Each test pins a shape the reviewer proved wrong by read-from-disk
// verification: aliases/generics misdescribed as "basic", always-empty
// method sets, and missing import/package-path identity. The tests are
// demonstrably capable of failing: each one fails on the pre-fix
// emitter (verified by running them against master@a0df481b).
package emit

import (
	"go/types"
	"strings"
	"testing"
)

// typeDetail returns the detail map of the first type with the given
// kind whose detail satisfies match, or nil.
func typeDetail(doc *Document, kind string, match func(d map[string]any) bool) map[string]any {
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != kind {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d == nil {
			continue
		}
		if match == nil || match(d) {
			return d
		}
	}
	return nil
}

// typeIDOf returns the type-table id of the first type with the given
// kind whose detail satisfies match, or -1.
func typeIDOf(doc *Document, kind string, match func(d map[string]any) bool) int {
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != kind {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d == nil {
			continue
		}
		if match == nil || match(d) {
			return i
		}
	}
	return -1
}

// typeKind returns the kind recorded for the type-table id, or "".
func typeKind(doc *Document, id int) string {
	if id < 0 || id >= len(doc.Types) {
		return ""
	}
	m, ok := doc.Types[id].(map[string]any)
	if !ok {
		return ""
	}
	k, _ := m["kind"].(string)
	return k
}

// derefAlias follows alias rhs links (e.g. the `any` alias to
// interface{}) and returns the final type id.
func derefAlias(doc *Document, id int) int {
	for typeKind(doc, id) == "alias" {
		d := typeDetailOf(doc, id)
		rhs, _ := d["rhs"].(int)
		if rhs == id {
			break
		}
		id = rhs
	}
	return id
}

// typeDetailOf returns the detail map of the type-table id, or nil.
func typeDetailOf(doc *Document, id int) map[string]any {
	if id < 0 || id >= len(doc.Types) {
		return nil
	}
	m, ok := doc.Types[id].(map[string]any)
	if !ok {
		return nil
	}
	d, _ := m["detail"].(map[string]any)
	return d
}

// methodNames returns the names in a methods list ([]any of maps with
// a "name" field).
func methodNames(t *testing.T, methods any) []string {
	t.Helper()
	list, ok := methods.([]any)
	if !ok {
		t.Fatalf("methods is %T, want []any", methods)
	}
	var names []string
	for _, m := range list {
		mm, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("method entry is %T, want map", m)
		}
		names = append(names, mm["name"].(string))
	}
	return names
}

// methodSig returns the "type" id of the named method entry, or -1.
func methodSig(methods any, name string) int {
	list, _ := methods.([]any)
	for _, m := range list {
		mm, _ := m.(map[string]any)
		if mm != nil && mm["name"] == name {
			id, _ := mm["type"].(int)
			return id
		}
	}
	return -1
}

// objectField returns one field of the object with the given name, or
// nil if no such object exists.
func objectField(doc *Document, name, field string) any {
	for _, o := range doc.Objects {
		if m, ok := o.(map[string]any); ok && m["name"] == name {
			return m[field]
		}
	}
	return nil
}

// assertNoUnknownBasic fails if any type is a "basic" with class
// "unknown": the fail-loud guard's whole point is that no type is ever
// silently misdescribed this way again.
func assertNoUnknownBasic(t *testing.T, doc *Document) {
	t.Helper()
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "basic" {
			continue
		}
		if d, _ := m["detail"].(map[string]any); d != nil && d["class"] == "unknown" {
			t.Errorf("Types[%d] is basic/unknown: %v", i, d)
		}
	}
}

// TestAliasTypeDescribed: `type A = B` is an alias, not a basic type.
// Before the fix every *types.Alias came out as
// {"kind":"basic","detail":{"class":"unknown"}}.
func TestAliasTypeDescribed(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Base struct{ N int }
type A = Base

var a A

func main() { println(a.N) }
`)
	assertNoUnknownBasic(t, doc)
	d := typeDetail(doc, "alias", func(d map[string]any) bool { return d["name"] == "A" })
	if d == nil {
		t.Fatalf("no alias type named A; types = %#v", doc.Types)
	}
	rhs, _ := d["rhs"].(int)
	if typeKind(doc, rhs) != "named" {
		t.Errorf("alias A rhs kind = %q, want named", typeKind(doc, rhs))
	}
	if rd := typeDetailOf(doc, rhs); rd == nil || !strings.HasSuffix(rd["name"].(string), ".Base") {
		t.Errorf("alias A rhs name = %v, want *.Base", rd["name"])
	}
	under, _ := d["underlying"].(int)
	if typeKind(doc, under) != "struct" {
		t.Errorf("alias A underlying kind = %q, want struct", typeKind(doc, under))
	}
	// The var's object points at the alias type, not at Base.
	if got := objectField(doc, "a", "type"); got != typeIDOf(doc, "alias", func(d map[string]any) bool { return d["name"] == "A" }) {
		t.Errorf("var a type = %v, want the alias type id", got)
	}
}

// TestGenericSignatureTypeParams: generic functions keep their type
// parameters, and type parameters keep their constraints — including
// union constraints, whose terms are tilde types.
// Before the fix the Signature detail omitted TypeParams entirely,
// *types.TypeParam came out basic/unknown, and the union in a
// constraint interface was invisible (embedded was always empty).
func TestGenericSignatureTypeParams(t *testing.T) {
	doc := emitTestSrc(t, `package main

func F[T any](x T) T {
	var y T = x
	_ = y
	return x
}

func G[T ~int | ~string](x T) T { return x }

func main() {
	println(F(1), G(2))
}
`)
	assertNoUnknownBasic(t, doc)
	// The type parameter itself is described, not misdescribed.
	tpd := typeDetail(doc, "typeparam", func(d map[string]any) bool { return d["name"] == "T" })
	if tpd == nil {
		t.Fatalf("no typeparam type named T; types = %#v", doc.Types)
	}
	// F's signature carries its type parameter; its constraint is the
	// empty interface.
	fds := funcDecls(t, doc)
	var fSig, gSig map[string]any
	for _, fd := range fds {
		f := fieldsOf(fd)
		switch f["name"] {
		case "F":
			fSig = typeDetailOf(doc, f["sig"].(int))
		case "G":
			gSig = typeDetailOf(doc, f["sig"].(int))
		}
	}
	if fSig == nil || gSig == nil {
		t.Fatalf("F/G signatures not found: F=%v G=%v", fSig, gSig)
	}
	checkTypeParam := func(sig map[string]any, fname string) map[string]any {
		t.Helper()
		tps, _ := sig["typeparams"].([]any)
		if len(tps) != 1 {
			t.Fatalf("%s typeparams = %d, want 1", fname, len(tps))
		}
		tp, _ := tps[0].(map[string]any)
		if tp["name"] != "T" {
			t.Errorf("%s typeparam name = %v, want T", fname, tp["name"])
		}
		return tp
	}
	fTP := checkTypeParam(fSig, "F")
	// F's constraint is the `any` alias; deref to the empty interface.
	fConstr := derefAlias(doc, fTP["constraint"].(int))
	if typeKind(doc, fConstr) != "interface" {
		t.Errorf("F constraint kind = %q, want interface", typeKind(doc, fConstr))
	} else if cd := typeDetailOf(doc, fConstr); cd["empty"] != true {
		t.Errorf("F constraint empty = %v, want true (any)", cd["empty"])
	}
	// G's constraint is an interface embedding a union of ~int | ~string.
	gTP := checkTypeParam(gSig, "G")
	gConstr := derefAlias(doc, gTP["constraint"].(int))
	if typeKind(doc, gConstr) != "interface" {
		t.Fatalf("G constraint kind = %q, want interface", typeKind(doc, gConstr))
	}
	cd := typeDetailOf(doc, gConstr)
	emb, _ := cd["embedded"].([]any)
	if len(emb) != 1 {
		t.Fatalf("G constraint embedded = %d, want 1 (the union)", len(emb))
	}
	unionID, _ := emb[0].(map[string]any)["type"].(int)
	if typeKind(doc, unionID) != "union" {
		t.Fatalf("G constraint embedded type kind = %q, want union", typeKind(doc, unionID))
	}
	terms, _ := typeDetailOf(doc, unionID)["terms"].([]any)
	if len(terms) != 2 {
		t.Fatalf("union terms = %d, want 2", len(terms))
	}
	termKinds := map[string]bool{}
	for _, term := range terms {
		tm := term.(map[string]any)
		if tm["tilde"] != true {
			t.Errorf("union term %v has tilde = %v, want true", tm, tm["tilde"])
		}
		termKinds[typeKind(doc, tm["type"].(int))] = true
	}
	if !termKinds["basic"] || len(termKinds) != 1 {
		t.Errorf("union term kinds = %v, want two basics (int, string)", termKinds)
	}
}

// TestNamedTypeArgs: an instantiated generic named type records both
// its declared type parameters and its instantiation arguments.
// Before the fix the Named detail omitted both.
func TestNamedTypeArgs(t *testing.T) {
	doc := emitTestSrc(t, `package main

type List[T any] struct{ V T }

var l List[int]

func main() { println(l.V) }
`)
	assertNoUnknownBasic(t, doc)
	intID := basicTypeID(doc, "int")
	if intID < 0 {
		t.Fatalf("no basic int type")
	}
	found := false
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "named" {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d["name"] != "main.List" {
			continue
		}
		tps, _ := d["typeparams"].([]any)
		if len(tps) != 1 || tps[0].(map[string]any)["name"] != "T" {
			t.Errorf("List typeparams = %v, want [T]", tps)
		}
		args, _ := d["typeargs"].([]any)
		if len(args) == 1 && args[0] == intID {
			found = true
		}
	}
	if !found {
		t.Errorf("no main.List with typeargs [int]; types = %#v", doc.Types)
	}
}

// TestNamedMethodsPopulated: a named type's methods are listed, with
// each method's signature id joining to its funcdecl.
// Before the fix Named methods were always [].
func TestNamedMethodsPopulated(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Point struct{ X, Y int }

func (p Point) Name() string { return "p" }
func (p *Point) Ptr() int    { return p.X }

func main() {
	p := Point{}
	println(p.Name(), p.Ptr())
}
`)
	d := typeDetail(doc, "named", func(d map[string]any) bool { return d["name"] == "main.Point" })
	if d == nil {
		t.Fatalf("no named main.Point; types = %#v", doc.Types)
	}
	names := methodNames(t, d["methods"])
	if len(names) != 2 {
		t.Fatalf("Point methods = %v, want [Name Ptr]", names)
	}
	want := map[string]bool{"Name": true, "Ptr": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected Point method %q", n)
		}
		if sig := methodSig(d["methods"], n); typeKind(doc, sig) != "signature" {
			t.Errorf("Point.%s method type kind = %q, want signature", n, typeKind(doc, sig))
		}
	}
	// Each method's signature id is the same id its funcdecl carries.
	for _, fd := range funcDecls(t, doc) {
		f := fieldsOf(fd)
		if f["name"] == "Name" || f["name"] == "Ptr" {
			if got := methodSig(d["methods"], f["name"].(string)); got != f["sig"] {
				t.Errorf("Point.%s method sig %d != funcdecl sig %v", f["name"], got, f["sig"])
			}
		}
	}
}

// TestInterfaceMethodsPopulated: an interface's explicit methods are
// listed, and embedded interfaces are listed with their own type ids —
// the embedded interface's methods are reachable through that id.
// Before the fix both lists were always [].
func TestInterfaceMethodsPopulated(t *testing.T) {
	doc := emitTestSrc(t, `package main

type R interface{ Read() int }
type RW interface {
	R
	Write(x int)
}

var rw RW

func main() { println(rw) }
`)
	rw := typeDetail(doc, "interface", func(d map[string]any) bool {
		return len(methodNames(t, d["methods"])) == 1 && methodNames(t, d["methods"])[0] == "Write"
	})
	if rw == nil {
		t.Fatalf("no interface with method Write; types = %#v", doc.Types)
	}
	if sig := methodSig(rw["methods"], "Write"); typeKind(doc, sig) != "signature" {
		t.Errorf("RW.Write method type kind = %q, want signature", typeKind(doc, sig))
	}
	emb, _ := rw["embedded"].([]any)
	if len(emb) != 1 {
		t.Fatalf("RW embedded = %d, want 1 (R)", len(emb))
	}
	rID, _ := emb[0].(map[string]any)["type"].(int)
	// The embedded type is the *named* type R; its underlying
	// interface carries the promoted method.
	if typeKind(doc, rID) != "named" {
		t.Fatalf("RW embedded type kind = %q, want named (R)", typeKind(doc, rID))
	}
	if rd := typeDetailOf(doc, rID); rd["name"] != "main.R" {
		t.Fatalf("RW embedded type name = %v, want main.R", rd["name"])
	}
	rUnder, _ := typeDetailOf(doc, rID)["underlying"].(int)
	if typeKind(doc, rUnder) != "interface" {
		t.Fatalf("R underlying kind = %q, want interface", typeKind(doc, rUnder))
	}
	rMethods := methodNames(t, typeDetailOf(doc, rUnder)["methods"])
	if len(rMethods) != 1 || rMethods[0] != "Read" {
		t.Errorf("embedded R methods = %v, want [Read]", rMethods)
	}
}

// TestFuncObjectPkgpath: a Func object carries the import path of its
// defining package, so a reference to fmt.Println is distinguishable
// from any other package's Println. Before the fix Func objects had
// kind/name/type only.
// TestPackageImportsList (same program): the Document carries the
// import list mapping local names to paths, and pkgref nodes carry the
// path of the package they reference.
func TestFuncObjectPkgpathAndImports(t *testing.T) {
	doc := emitTestSrc(t, `package main

import (
	"fmt"
	f "os"
)

func Hello() string { return "hi" }

func main() {
	fmt.Println(f.Args, Hello())
}
`)
	if got := objectField(doc, "Println", "pkgpath"); got != "fmt" {
		t.Errorf("Println pkgpath = %v, want fmt", got)
	}
	if got := objectField(doc, "Hello", "pkgpath"); got != "main" {
		t.Errorf("Hello pkgpath = %v, want main", got)
	}
	if got := objectField(doc, "main", "pkgpath"); got != "main" {
		t.Errorf("main pkgpath = %v, want main", got)
	}
	// The import list maps every local name to its path.
	imports, _ := doc.Package["imports"].([]any)
	if len(imports) != 2 {
		t.Fatalf("imports = %#v, want 2 entries", doc.Package["imports"])
	}
	want := []map[string]string{
		{"name": "fmt", "path": "fmt"},
		{"name": "f", "path": "os"},
	}
	for i, w := range want {
		got, _ := imports[i].(map[string]any)
		if got["name"] != w["name"] || got["path"] != w["path"] {
			t.Errorf("imports[%d] = %v, want %v", i, got, w)
		}
	}
	// pkgref nodes carry the same path, so a use site resolves without
	// consulting the import list.
	refs := nodesOf(t, doc, "pkgref")
	if len(refs) != 2 {
		t.Fatalf("pkgref nodes = %d, want 2", len(refs))
	}
	byPkg := map[string]string{}
	for _, r := range refs {
		f := fieldsOf(r)
		byPkg[f["pkg"].(string)] = f["pkgpath"].(string)
	}
	if byPkg["fmt"] != "fmt" {
		t.Errorf("fmt pkgref pkgpath = %q, want fmt", byPkg["fmt"])
	}
	if byPkg["f"] != "os" {
		t.Errorf("f pkgref pkgpath = %q, want os", byPkg["f"])
	}
}

// fakeType is a types.Type implementation go/types will never produce,
// used to prove describeType fails loudly on unhandled types instead
// of misdescribing them as basic/unknown.
type fakeType struct{ s string }

func (f fakeType) Underlying() types.Type { return nil }
func (f fakeType) String() string         { return f.s }

// TestUnhandledTypeFailsLoudly: the default case of describeType is an
// error, and typeID records it so Emit aborts. After this lane every
// types.Type go/types can produce has its own case, so the guard is
// reachable only through a future go/types addition — the test pins
// the guard itself, not any current type.
func TestUnhandledTypeFailsLoudly(t *testing.T) {
	e := &Emitter{
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	if _, err := e.describeType(fakeType{"mystery"}); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("describeType(fake) err = %v, want unsupported type", err)
	}
	e.typeID(fakeType{"mystery"})
	if e.typeErr == nil || !strings.Contains(e.typeErr.Error(), "unsupported type") {
		t.Fatalf("typeID(fake) typeErr = %v, want unsupported type", e.typeErr)
	}
}
