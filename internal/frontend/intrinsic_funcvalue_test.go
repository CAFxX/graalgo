package main

// R10-09 (review-10-math-funcvalue): exporter tests for synthetic
// intrinsics used in value position.
//
// Red-green: before the fix, a math intrinsic in value position is a
// compile-time scope error on the qualified form ("math.Sqrt is an
// intrinsic and cannot be used in value position (M7)") and an
// "unknown function" error on the bare-ident (dot-import) form — even
// though gc accepts both and the direct call works. The alias
// intrinsics are worse: the value binds the synthetic `return false`
// placeholder body, so the program compiles and silently returns the
// wrong answer. After the fix, every intrinsic in value position
// lowers to a synthesized wrapper closure (the exportTimeIntrinsicValue
// precedent): func(params) results { return <intrinsic>(params...) },
// so calling the value behaves exactly like the direct call, and no
// intrinsic is ever emitted as a funcdecl.
//
//  1. TestMathIntrinsicFuncValueSqrt pins the exact reported construct:
//     f := math.Sqrt; f(2) — the binding is a funclit whose body
//     returns a "mathsqrt" wire node.
//  2. TestMathIntrinsicFuncValueFloat64bits pins g := math.Float64bits.
//  3. TestMathIntrinsicFuncValueFMA pins h := math.FMA (3 args).
//  4. TestMathIntrinsicFuncValueDotImport pins the bare-ident form
//     (dot-imported math.Sqrt) through exportIdent.
//  5. TestMathIntrinsicFuncValueAsArgument pins math.Sqrt passed as a
//     call argument.
//  6. TestAliasIntrinsicFuncValue pins f := alias.AnyOverlap (via a
//     test driver in crypto/, the only importers allowed by Go's
//     internal-visibility rule): the binding is a funclit returning a
//     "sliceoverlap" node, and no AnyOverlap funcdecl (with its
//     `return false` placeholder body) is emitted.
//  7. TestTimeIntrinsicFuncValueDotImport pins the same bare-ident
//     gap for time.Now, which the qualified form already handled via
//     the wrapper closure.

import (
	"path/filepath"
	"runtime"
	"testing"
)

// funcValueFunclit returns the funclit node bound to the local var
// `name` in main (its vardecl's single value), failing the test when
// the binding is not a funclit.
func funcValueFunclit(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		if names[0] != name {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			t.Fatalf("vardecl %q has %d values, want 1", name, len(vals))
		}
		lit, _ := vals[0].(map[string]any)
		if lit == nil || lit["kind"] != "funclit" {
			kind := ""
			if lit != nil {
				kind, _ = lit["kind"].(string)
			}
			t.Fatalf("var %q binds kind %q, want funclit", name, kind)
		}
		return lit
	}
	t.Fatalf("no vardecl for %q", name)
	return nil
}

// funclitReturnKind returns the kind of the single expression returned
// by a wrapper funclit's body ("" when the body is not a lone return).
func funclitReturnKind(t *testing.T, lit map[string]any) string {
	t.Helper()
	body, _ := lit["fields"].(map[string]any)["body"].(map[string]any)
	if body == nil {
		t.Fatal("funclit has no body")
	}
	stmts, _ := body["fields"].(map[string]any)["stmts"].([]any)
	if len(stmts) != 1 {
		t.Fatalf("funclit body has %d statements, want 1 return", len(stmts))
	}
	ret, _ := stmts[0].(map[string]any)
	if ret == nil || ret["kind"] != "return" {
		t.Fatalf("funclit body statement kind = %q, want return", ret["kind"])
	}
	results, _ := ret["fields"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("funclit return has %d results, want 1", len(results))
	}
	res, _ := results[0].(map[string]any)
	if res == nil {
		t.Fatal("funclit return result is not a node")
	}
	kind, _ := res["kind"].(string)
	return kind
}

// assertNoFuncDecl fails the test when the wire doc contains a
// funcdecl with the given name (intrinsics are never funcdecls).
func assertNoFuncDecl(t *testing.T, doc map[string]any, name string) {
	t.Helper()
	if funcNames(doc)[name] {
		t.Fatalf("wire doc contains funcdecl %q; intrinsics are never exported as funcdecls", name)
	}
}

func TestMathIntrinsicFuncValueSqrt(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "math"

func main() {
	f := math.Sqrt
	f(2)
}
`)
	lit := funcValueFunclit(t, doc, "f")
	if got := funclitReturnKind(t, lit); got != "mathsqrt" {
		t.Fatalf("Sqrt wrapper returns %q, want mathsqrt", got)
	}
	assertNoFuncDecl(t, doc, "Sqrt")
}

func TestMathIntrinsicFuncValueFloat64bits(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "math"

func main() {
	g := math.Float64bits
	g(2)
}
`)
	lit := funcValueFunclit(t, doc, "g")
	if got := funclitReturnKind(t, lit); got != "mathf64bits" {
		t.Fatalf("Float64bits wrapper returns %q, want mathf64bits", got)
	}
	assertNoFuncDecl(t, doc, "Float64bits")
}

func TestMathIntrinsicFuncValueFMA(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "math"

func main() {
	h := math.FMA
	h(2, 3, 4)
}
`)
	lit := funcValueFunclit(t, doc, "h")
	if got := funclitReturnKind(t, lit); got != "mathfma" {
		t.Fatalf("FMA wrapper returns %q, want mathfma", got)
	}
	assertNoFuncDecl(t, doc, "FMA")
}

func TestMathIntrinsicFuncValueDotImport(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import . "math"

func main() {
	f := Sqrt
	f(2)
}
`)
	lit := funcValueFunclit(t, doc, "f")
	if got := funclitReturnKind(t, lit); got != "mathsqrt" {
		t.Fatalf("dot-imported Sqrt wrapper returns %q, want mathsqrt", got)
	}
	assertNoFuncDecl(t, doc, "Sqrt")
}

func TestMathIntrinsicFuncValueAsArgument(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "math"

func apply(f func(float64) float64, x float64) float64 { return f(x) }

func main() {
	apply(math.Sqrt, 2)
}
`)
	// The call's first argument is the wrapper funclit.
	found := false
	for _, n := range findAllKind(doc, "call") {
		fun, _ := n["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] != "apply" {
			continue
		}
		args, _ := n["fields"].(map[string]any)["args"].([]any)
		if len(args) != 2 {
			continue
		}
		lit, _ := args[0].(map[string]any)
		if lit == nil || lit["kind"] != "funclit" {
			continue
		}
		if got := funclitReturnKind(t, lit); got != "mathsqrt" {
			t.Fatalf("Sqrt argument wrapper returns %q, want mathsqrt", got)
		}
		found = true
	}
	if !found {
		t.Fatal("no apply(math.Sqrt, 2) call with a funclit first argument found")
	}
	assertNoFuncDecl(t, doc, "Sqrt")
}

func TestAliasIntrinsicFuncValue(t *testing.T) {
	// crypto/internal/fips140/alias is importable only from within
	// crypto/ (Go internal-visibility rule): export through a driver
	// declared as a crypto package, exactly as `graalgo test` does.
	p := writeIfaceProg(t, `package main

import "crypto/internal/fips140/alias"

func main() {
	f := alias.AnyOverlap
	f([]byte("abc"), []byte("def"))
}
`)
	out, err := ExportFiles([]string{p}, filepath.Join(runtime.GOROOT(), "src", "crypto", "aes"))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	lit := funcValueFunclit(t, doc, "f")
	if got := funclitReturnKind(t, lit); got != "sliceoverlap" {
		t.Fatalf("AnyOverlap wrapper returns %q, want sliceoverlap", got)
	}
	// The `return false` placeholder body must not be emitted: a value
	// bound to it would silently answer false for overlapping slices.
	assertNoFuncDecl(t, doc, "AnyOverlap")
}

func TestTimeIntrinsicFuncValueDotImport(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import . "time"

func main() {
	f := Now
	f()
}
`)
	lit := funcValueFunclit(t, doc, "f")
	if got := funclitReturnKind(t, lit); got != "timenow" {
		t.Fatalf("dot-imported Now wrapper returns %q, want timenow", got)
	}
	assertNoFuncDecl(t, doc, "Now")
}
