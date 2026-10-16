// Wire-level regression test: a named constant's value must survive the
// full pipeline (parse → check → emit → encode) and read back correctly
// from the binary document the Java backend consumes.
package encode

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// walkField descends decoded map[string]any nodes via kind/fields structure.
func walkField(t *testing.T, m map[string]any, keys ...string) any {
	t.Helper()
	cur := any(m)
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("expected map at %q, got %T (%#v)", k, cur, cur)
		}
		cur, ok = cm[k]
		if !ok {
			t.Fatalf("missing key %q in %#v", k, cm)
		}
	}
	return cur
}

func TestEncodeConstPipeline(t *testing.T) {
	src := `package main

const K = 5

func main() {
	println(K)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "const.go")
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
	doc, err := emit.Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Strip magic + version, decode the document with the test decoder.
	rest := bin[len(formatMagic):]
	_, n := binary.Uvarint(rest)
	if n <= 0 {
		t.Fatal("truncated version header")
	}
	got, trail := decodeTestValue(t, rest[n:])
	if len(trail) != 0 {
		t.Fatalf("%d trailing bytes after decode", len(trail))
	}
	decoded, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("decoded document is %T, want map", got)
	}

	// Navigate to the println call's first argument.
	funcs := walkField(t, decoded, "funcs").([]any)
	arg := walkField(t, funcs[0].(map[string]any),
		"fields", "body", "fields", "stmts").([]any)[0].(map[string]any)
	arg = walkField(t, arg, "fields", "x", "fields", "args").([]any)[0].(map[string]any)

	if kind := walkField(t, arg, "kind"); kind != "basiclit" {
		t.Fatalf("wire arg kind = %v, want basiclit (folded const)", kind)
	}
	if v := walkField(t, arg, "fields", "value"); v != "5" {
		t.Fatalf("wire const value = %v, want %q", v, "5")
	}
}
