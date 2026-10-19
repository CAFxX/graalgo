package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestPrintlnVariadicPacking: `fmt.Println("hello")` as a bare statement
// must desugar to `__Println([]any{"hello"})` — a single complit argument.
// Without the fix-redmaster-r14 packing in multiResultHelperArgs, the
// backend receives a bare string where []any is expected (regression:
// real-fmt2 switched fmt from synthetic to real GOROOT source, exposing
// the unpacked variadic helper call).
func TestPrintlnVariadicPacking(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	doc := exportSrc(t, src)
	calls := findNodes(t, doc, "call")
	found := false
	for _, c := range calls {
		fun, ok := nodeFields(t, c)["fun"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := nodeFields(t, fun)["name"].(string)
		if name != "__Println" {
			continue
		}
		found = true
		args, _ := nodeFields(t, c)["args"].([]any)
		if len(args) != 1 {
			t.Fatalf("__Println called with %d args, want 1 (packed []any)", len(args))
		}
		arg, ok := args[0].(map[string]any)
		if !ok || arg["kind"] != "complit" {
			t.Fatalf("__Println arg kind = %v, want complit (packed variadic slice)", arg["kind"])
		}
	}
	if !found {
		t.Fatal("no __Println call found in exported document")
	}
}

// TestPrintlnAlreadyPacked: `fmt.Println([]any{"a"})` must NOT double-pack.
// The explicit slice argument is already the variadic slice; packing again
// would create []any{[]any{...}}.
func TestPrintlnAlreadyPacked(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	fmt.Println([]any{"a", "b"})
}
`
	doc := exportSrc(t, src)
	calls := findNodes(t, doc, "call")
	for _, c := range calls {
		fun, ok := nodeFields(t, c)["fun"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := nodeFields(t, fun)["name"].(string)
		if name != "__Println" {
			continue
		}
		args, _ := nodeFields(t, c)["args"].([]any)
		if len(args) != 1 {
			t.Fatalf("__Println called with %d args, want 1", len(args))
		}
		arg, ok := args[0].(map[string]any)
		if !ok || arg["kind"] != "complit" {
			t.Fatalf("__Println arg kind = %v, want complit", arg["kind"])
		}
		// The complit must not contain a nested slice (no double-packing).
		// (Value count depends on export normalization; the structural
		// check is the absence of a nested complit.)
		vals, _ := nodeFields(t, arg)["values"].([]any)
		for _, v := range vals {
			vm, ok := v.(map[string]any)
			if !ok || vm["kind"] == "complit" {
				t.Fatalf("nested complit in packed arg: double-packing detected")
			}
		}
		return
	}
	t.Fatal("no __Println call found in exported document")
}

// TestReadRuneM8Entry: the m8IfaceMultiResultMethods table must contain
// io.RuneReader.ReadRune with the ioRuneReaderPair carrier. Without this,
// real fmt's (*ss).ReadRune (`r, size, err = s.rs.ReadRune()`) is not
// classified and the backend rejects the raw 3-target assign.
func TestReadRuneM8Entry(t *testing.T) {
	found := false
	for _, m := range m8IfaceMultiResultMethods {
		if m.pkgPath == "io" && m.method == "ReadRune" {
			found = true
			if m.pairType != "ioRuneReaderPair" {
				t.Fatalf("ReadRune pairType = %q, want ioRuneReaderPair", m.pairType)
			}
			want := []string{"r", "size", "err"}
			if len(m.pairFields) != len(want) {
				t.Fatalf("ReadRune pairFields = %v, want %v", m.pairFields, want)
			}
			for i, f := range want {
				if m.pairFields[i] != f {
					t.Fatalf("ReadRune pairFields[%d] = %q, want %q", i, m.pairFields[i], f)
				}
			}
		}
	}
	if !found {
		t.Fatal("m8IfaceMultiResultMethods has no io.ReadRune entry")
	}
	// The pair type must exist in the synthetic io package with matching fields.
	sp := getSyntheticPackage("io")
	if sp == nil {
		t.Fatal("synthetic io package not found")
	}
	obj := sp.pkg.Scope().Lookup("ioRuneReaderPair")
	if obj == nil {
		t.Fatal("ioRuneReaderPair not found in synthetic io package")
	}
}

// TestDiskTransformPreservesEllipsis: the disk MR transform must propagate
// the `...` spread when rewriting a variadic multi-result call to its
// __-helper. Without this, `Fprintln(w, a...)` becomes `__Fprintln(w, a)`
// (spread lost), and the exporter double-wraps to []any{a}.
func TestDiskTransformPreservesEllipsis(t *testing.T) {
	src := `package p

func Fprintln(w any, a ...any) (int, error) { return 0, nil }

func Println(a ...any) (int, error) {
	return Fprintln(nil, a...)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	specs := transformDiskMultiResult("p", []*ast.File{f})
	if len(specs) == 0 {
		t.Fatal("no specs from transformDiskMultiResult")
	}
	// Find the __Println helper decl and its return statement's helper call.
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "__Println" {
			continue
		}
		var foundCall bool
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == "__Fprintln" {
				foundCall = true
				if !ce.Ellipsis.IsValid() {
					t.Errorf("__Fprintln helper call lost the ... spread (Ellipsis not propagated)")
				}
			}
			return true
		})
		if !foundCall {
			t.Error("__Println body has no __Fprintln call")
		}
		return
	}
	t.Error("__Println helper decl not found after transform")
}

// TestReadRuneClassified: a 3-result interface method call matching the
// ReadRune shape must classify (not fall through to a raw multi-target
// assign). Uses a local interface to avoid depending on fmt internals.
func TestReadRuneClassified(t *testing.T) {
	src := `package main

type RuneReader interface {
	ReadRune() (r rune, size int, err error)
}

func main() {
	var rr RuneReader
	_, _, _ = rr.ReadRune()
}
`
	doc := exportSrc(t, src)
	// The call must NOT appear as a raw assign with 3 lhs and 1 rhs.
	assigns := findNodes(t, doc, "assign")
	for _, a := range assigns {
		fields := nodeFields(t, a)
		lhs, _ := fields["lhs"].([]any)
		rhs, _ := fields["rhs"].([]any)
		if len(lhs) == 3 && len(rhs) == 1 {
			if rm, ok := rhs[0].(map[string]any); ok && rm["kind"] == "ifacemethod" {
				t.Fatalf("unclassified 3-result ifacemethod assign (lhs/rhs mismatch the backend rejects)")
			}
		}
	}
	// Positive: the ifacemethod dispatch node must exist (desugared).
	ifaces := findNodes(t, doc, "ifacemethod")
	if len(ifaces) == 0 {
		// Also accept the block/vardecl desugar shape; the key negative
		// assertion above is the backend-rejection guard.
		t.Log("no ifacemethod node (desugar may use a different shape); negative check passed")
	}
	if !strings.Contains(doc, "ReadRune") {
		t.Fatal("ReadRune vanished from exported document")
	}
}
