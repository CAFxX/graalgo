package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBuiltinConstFold (R22-F-6): constant builtin calls must fold to a
// basiclit, mirroring BinaryExpr/UnaryExpr/ParenExpr. Before the fix,
// real(3+4i) exported as a live ComplexNode over a folded constant, whose
// UNTYPED_COMPLEX argument the factory builds as a GoComplex64 while the
// node's kind stays UNTYPED_FLOAT — a float32 consumer then crashed in
// ComplexNode.executeFloat (ClassCastException), and float32(real(3+4i))
// crashed in ConvertNode.executeFloat (UnsupportedOperationException).
//
// The control program pins the negative: real/imag of a non-constant
// variable must still export as a builtin call node.
func TestBuiltinConstFold(t *testing.T) {
	constSrc := `package main

func main() {
	var f32 float32 = real(3 + 4i)
	var g32 float32 = imag(3 + 4i)
	var f64 float64 = real(3 + 4i)
	var g64 float64 = imag(3 + 4i)
	var c64 complex64 = complex(1, 2)
	var c128 complex128 = complex(1, 2)
	h32 := float32(real(3 + 4i))
	h64 := float64(imag(3 + 4i))
	println(f32, g32, f64, g64, c64, c128, h32, h64)
}
`
	out := exportSource(t, constSrc)
	for _, b := range []string{"real", "imag", "complex"} {
		if hasBuiltinCall(out, b) {
			t.Fatalf("constant %s(...) call was not folded: builtin call node still present in export", b)
		}
	}
	// The folded values must survive as basiclit constants.
	for _, want := range []string{"3", "4"} {
		if !hasBasiclitConst(out, want) {
			t.Fatalf("folded constant %q not found as a basiclit in export", want)
		}
	}
}

func TestBuiltinNonConstNotFolded(t *testing.T) {
	ctrlSrc := `package main

func main() {
	c := complex(1.5, 2.5)
	println(real(c), imag(c))
}
`
	out := exportSource(t, ctrlSrc)
	for _, b := range []string{"real", "imag"} {
		if !hasBuiltinCall(out, b) {
			t.Fatalf("non-constant %s(c) call was folded or dropped: no builtin call node in export", b)
		}
	}
}

// exportSource writes src to a temp file and returns its export JSON.
func exportSource(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(path)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	return out
}

// hasBuiltinCall reports whether the export JSON contains a call node
// whose fields carry builtin == name.
func hasBuiltinCall(exportJSON, name string) bool {
	var doc any
	if err := json.Unmarshal([]byte(exportJSON), &doc); err != nil {
		return false
	}
	found := false
	walkJSON(doc, func(n map[string]any) {
		if n["kind"] == "call" {
			if f, ok := n["fields"].(map[string]any); ok && f["builtin"] == name {
				found = true
			}
		}
	})
	return found
}

// hasBasiclitConst reports whether the export JSON contains a basiclit
// node whose fields carry const == want.
func hasBasiclitConst(exportJSON, want string) bool {
	var doc any
	if err := json.Unmarshal([]byte(exportJSON), &doc); err != nil {
		return false
	}
	found := false
	walkJSON(doc, func(n map[string]any) {
		if n["kind"] == "basiclit" {
			if f, ok := n["fields"].(map[string]any); ok && f["const"] == want {
				found = true
			}
		}
	})
	return found
}

// walkJSON visits every object node in a decoded JSON document.
func walkJSON(v any, fn func(map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		fn(v)
		for _, c := range v {
			walkJSON(c, fn)
		}
	case []any:
		for _, c := range v {
			walkJSON(c, fn)
		}
	}
}
