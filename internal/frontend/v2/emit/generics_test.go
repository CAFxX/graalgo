// Positive tests for generic function instantiation (monomorphization).
//
// Each test feeds a Go program using generics through the full pipeline
// (parse → check → emit) and asserts structural properties of the emitted
// Document: the generic template is NOT emitted as a funcdecl, each
// distinct instantiation IS emitted under its mangled name with concrete
// (substituted) types, and call sites reference the instantiation.
//
// The assertions are tight enough to fail if substitution is skipped:
// e.g. TestGenericFuncSingleTypeArg requires the parameter type to be the
// concrete basic type int, not the type parameter T.
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitGenericsSrc runs the full pipeline on src and returns the Document.
func emitGenericsSrc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "gen.go")
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

// pipelineErr runs the full pipeline on src and returns the first error
// from any stage, expecting a loud failure.
func pipelineErr(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "gen.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		return err
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		return err
	}
	_, err = Emit(cres, pres.Pkg, pres.Files)
	return err
}

// emitGenericsErr runs the pipeline and expects a loud emitter error.
func emitGenericsErr(t *testing.T, src string) error {
	t.Helper()
	err := pipelineErr(t, src)
	if err == nil {
		t.Fatalf("expected loud emitter failure, got success")
	}
	return err
}

func nodeKind(n any) string {
	m, ok := n.(map[string]any)
	if !ok {
		return ""
	}
	k, _ := m["kind"].(string)
	return k
}

func nodeFields(n any) map[string]any {
	m, _ := n.(map[string]any)
	if m == nil {
		return nil
	}
	f, _ := m["fields"].(map[string]any)
	return f
}

// funcByName finds a funcdecl node by name.
func funcByName(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, f := range doc.Funcs {
		if nodeKind(f) != "funcdecl" {
			continue
		}
		if nodeFields(f)["name"] == name {
			return nodeFields(f)
		}
	}
	t.Fatalf("no funcdecl named %q in %d funcs", name, len(doc.Funcs))
	return nil
}

func funcNames(doc *Document) []string {
	var out []string
	for _, f := range doc.Funcs {
		if nodeKind(f) == "funcdecl" {
			out = append(out, nodeFields(f)["name"].(string))
		}
	}
	return out
}

// basicTypeName resolves a type-table id to the basic type's name.
func basicTypeName(t *testing.T, doc *Document, id any) string {
	t.Helper()
	idx, ok := id.(int)
	if !ok {
		t.Fatalf("type id is %T, want int", id)
	}
	tm, ok := doc.Types[idx].(map[string]any)
	if !ok {
		t.Fatalf("types[%d] is %T", idx, doc.Types[idx])
	}
	if tm["kind"] != "basic" {
		t.Fatalf("types[%d] kind is %v, want basic (type was not substituted to a concrete type)", idx, tm["kind"])
	}
	detail, _ := tm["detail"].(map[string]any)
	name, _ := detail["name"].(string)
	return name
}

// objectName resolves an object-table id to the object's name.
func objectName(t *testing.T, doc *Document, id any) string {
	t.Helper()
	idx, ok := id.(int)
	if !ok {
		t.Fatalf("object id is %T, want int", id)
	}
	om, ok := doc.Objects[idx].(map[string]any)
	if !ok {
		t.Fatalf("objects[%d] is %T", idx, doc.Objects[idx])
	}
	name, _ := om["name"].(string)
	return name
}

// callFunObj finds the first call expression in a funcdecl body and returns
// the object-table name its fun ident references.
func callFunObj(t *testing.T, doc *Document, funcName string) string {
	t.Helper()
	fields := funcByName(t, doc, funcName)
	body, _ := fields["body"].(map[string]any)
	var find func(n any) any
	find = func(n any) any {
		m, ok := n.(map[string]any)
		if !ok {
			return nil
		}
		if m["kind"] == "call" {
			return m
		}
		f, _ := m["fields"].(map[string]any)
		for _, v := range f {
			if r := find(v); r != nil {
				return r
			}
			if l, ok := v.([]any); ok {
				for _, e := range l {
					if r := find(e); r != nil {
						return r
					}
				}
			}
		}
		return nil
	}
	call := find(body)
	if call == nil {
		t.Fatalf("no call expression in body of %s", funcName)
	}
	fun := nodeFields(call)["fun"]
	oid := nodeFields(fun)["obj"]
	return objectName(t, doc, oid)
}

func TestGenericFuncSingleTypeArg(t *testing.T) {
	doc := emitGenericsSrc(t, `package main
func id[T any](x T) T { return x }
func main() {
	y := id[int](42)
	println(y)
}
`)
	names := funcNames(doc)
	if len(names) != 2 || names[0] != "main" || names[1] != "id[int]" {
		t.Fatalf("funcs = %v, want [main id[int]] (template must not be emitted)", names)
	}
	// The instantiation's parameter must have the CONCRETE type int.
	fn := funcByName(t, doc, "id[int]")
	params, _ := fn["params"].([]any)
	if len(params) != 1 {
		t.Fatalf("id[int] has %d params, want 1", len(params))
	}
	if got := basicTypeName(t, doc, nodeFields(params[0])["type"]); got != "int" {
		t.Fatalf("id[int] param type = %q, want int (type parameter was not substituted)", got)
	}
	// The call site must reference the instantiation, not the template.
	if got := callFunObj(t, doc, "main"); got != "id[int]" {
		t.Fatalf("call fun references %q, want id[int]", got)
	}
}

func TestGenericFuncTypeInference(t *testing.T) {
	doc := emitGenericsSrc(t, `package main
func id[T any](x T) T { return x }
func main() {
	y := id(42)
	println(y)
}
`)
	names := funcNames(doc)
	if len(names) != 2 || names[1] != "id[int]" {
		t.Fatalf("funcs = %v, want [main id[int]] (inferred type argument)", names)
	}
	if got := callFunObj(t, doc, "main"); got != "id[int]" {
		t.Fatalf("call fun references %q, want id[int]", got)
	}
}

func TestGenericFuncMultiTypeArg(t *testing.T) {
	doc := emitGenericsSrc(t, `package main
func pair[A, B any](a A, b B) B { return b }
func main() {
	s := pair[int, string](1, "x")
	println(s)
}
`)
	names := funcNames(doc)
	if len(names) != 2 || names[1] != "pair[int,string]" {
		t.Fatalf("funcs = %v, want [main pair[int,string]]", names)
	}
	fn := funcByName(t, doc, "pair[int,string]")
	params, _ := fn["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("pair[int,string] has %d params, want 2", len(params))
	}
	if got := basicTypeName(t, doc, nodeFields(params[0])["type"]); got != "int" {
		t.Fatalf("param 0 type = %q, want int", got)
	}
	if got := basicTypeName(t, doc, nodeFields(params[1])["type"]); got != "string" {
		t.Fatalf("param 1 type = %q, want string", got)
	}
}

func TestGenericInstantiationDedup(t *testing.T) {
	doc := emitGenericsSrc(t, `package main
func id[T any](x T) T { return x }
func main() {
	a := id[int](1)
	b := id[int](2)
	println(a, b)
}
`)
	count := 0
	for _, n := range funcNames(doc) {
		if n == "id[int]" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("id[int] emitted %d times, want 1 (instantiations must be deduplicated)", count)
	}
}

func TestGenericMultipleTypeArgs(t *testing.T) {
	doc := emitGenericsSrc(t, `package main
func id[T any](x T) T { return x }
func main() {
	a := id[int](1)
	b := id[string]("x")
	println(a, b)
}
`)
	names := funcNames(doc)
	want := map[string]bool{"main": true, "id[int]": true, "id[string]": true}
	if len(names) != 3 {
		t.Fatalf("funcs = %v, want 3 funcs", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected func %q in %v", n, names)
		}
	}
	fn := funcByName(t, doc, "id[string]")
	params, _ := fn["params"].([]any)
	if got := basicTypeName(t, doc, nodeFields(params[0])["type"]); got != "string" {
		t.Fatalf("id[string] param type = %q, want string", got)
	}
}

func TestGenericNestedInstantiation(t *testing.T) {
	// twice[T] calls id[T]: the inner instantiation's type argument is
	// itself a type parameter and must be substituted with the outer
	// instantiation's type argument.
	doc := emitGenericsSrc(t, `package main
func id[T any](x T) T { return x }
func twice[T any](x T) T { return id[T](x) }
func main() {
	y := twice[int](3)
	println(y)
}
`)
	names := funcNames(doc)
	want := map[string]bool{"main": true, "twice[int]": true, "id[int]": true}
	if len(names) != 3 {
		t.Fatalf("funcs = %v, want [main twice[int] id[int]]", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected func %q in %v (nested instantiation not resolved)", n, names)
		}
	}
	// Inside twice[int], the call must reference id[int] (not id[T]).
	if got := callFunObj(t, doc, "twice[int]"); got != "id[int]" {
		t.Fatalf("twice[int] body calls %q, want id[int]", got)
	}
}

func TestGenericFuncLocalVarSubstitution(t *testing.T) {
	// A local declared from a type parameter must get the concrete type.
	doc := emitGenericsSrc(t, `package main
func wrap[T any](x T) T {
	y := x
	return y
}
func main() {
	z := wrap[int](7)
	println(z)
}
`)
	fn := funcByName(t, doc, "wrap[int]")
	body, _ := fn["body"].(map[string]any)
	stmts, _ := body["fields"].(map[string]any)["stmts"].([]any)
	if len(stmts) != 2 {
		t.Fatalf("wrap[int] body has %d stmts, want 2", len(stmts))
	}
	// First stmt is y := x; the declared object's type must be int.
	assign := nodeFields(stmts[0])
	lhs, _ := assign["lhs"].([]any)
	if len(lhs) != 1 {
		t.Fatalf("lhs has %d exprs, want 1", len(lhs))
	}
	if got := basicTypeName(t, doc, nodeFields(lhs[0])["type"]); got != "int" {
		t.Fatalf("local y type = %q, want int (local was not substituted)", got)
	}
}

func TestGenericFuncValueLoud(t *testing.T) {
	// go/types itself rejects an uninstantiated generic function in value
	// position — the pipeline must fail loudly (at the check stage), never
	// silently accept it.
	err := pipelineErr(t, `package main
func id[T any](x T) T { return x }
func main() {
	f := id
	_ = f
}
`)
	if err == nil {
		t.Fatalf("expected loud failure for uninstantiated generic function value, got success")
	}
	want := "cannot use generic function id without instantiation"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

func TestGenericTypeDeclLoud(t *testing.T) {
	err := emitGenericsErr(t, `package main
type Box[T any] struct{ v T }
func main() {}
`)
	want := "generic type declarations are not yet supported"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

func TestGenericMethodLoud(t *testing.T) {
	// Method declared before the generic type: the method check fires first.
	err := emitGenericsErr(t, `package main
func (b Box[T]) Get() T { return b.v }
type Box[T any] struct{ v T }
func main() {}
`)
	want := "generic methods are not yet supported"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}
