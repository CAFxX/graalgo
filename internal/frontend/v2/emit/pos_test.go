// Source-position tests (review-1105-v2-types).
//
// Spec §3: col is rune-based (not byte-based) and len is the rune
// length; a node whose file is not in package.files reports file -1.
package emit

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// allPositions walks the funcs tree and returns every pos map.
func allPositions(t *testing.T, doc *Document) []map[string]any {
	t.Helper()
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if p, ok := x["pos"].(map[string]any); ok {
				out = append(out, p)
			}
			for _, f := range x {
				walk(f)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc.Funcs)
	return out
}

func TestRunePositions(t *testing.T) {
	// "héllo" on the same line, before the println call: é is 2 bytes
	// but 1 rune, so the call's byte column (17) and rune column (16)
	// differ, as do the string literal's byte length (8) and rune
	// length (7, quotes included). The source must be a real file on
	// disk: rune columns are computed by reading the source, and an
	// in-memory-only parse falls back to byte columns (see
	// TestPosInMemoryFallback).
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
	src := "package demo\n\nfunc main() {\n\ts := \"héllo\"; println(s)\n}\n"
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatal(err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	call := findNode(t, doc, "call", nil)
	pos, _ := call["pos"].(map[string]any)
	if pos["col"] != 16 {
		t.Errorf("println call col = %v, want 16 (rune-based; byte col is 17)", pos["col"])
	}
	lit := findNode(t, doc, "basiclit", func(m map[string]any) bool {
		f, _ := m["fields"].(map[string]any)
		return f["value"] == "\"héllo\""
	})
	lpos, _ := lit["pos"].(map[string]any)
	if lpos["len"] != 7 {
		t.Errorf("string literal len = %v, want 7 (rune length; byte length is 8)", lpos["len"])
	}
}

func TestPosUnknownFileIsMinusOne(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
	if err := os.WriteFile(fn, []byte("package demo\n\nfunc main() { println(42) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatal(err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately pass a files list that does not contain the parsed file.
	doc, err := Emit(cres, pres.Pkg, []string{"/nonexistent/other.go"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	positions := allPositions(t, doc)
	if len(positions) == 0 {
		t.Fatal("no positions emitted")
	}
	for _, p := range positions {
		if p["file"] != -1 {
			t.Errorf("pos file = %v, want -1 (unknown file must not alias file 0)", p["file"])
		}
	}
}

func TestPosInMemoryFallback(t *testing.T) {
	// In-memory-only parse: the source file does not exist on disk,
	// so rune columns cannot be computed and pos falls back to the
	// byte-based values instead of crashing or lying.
	fset := token.NewFileSet()
	src := "package demo\n\nfunc main() { x := \"héllo\"; println(x) }\n"
	pkg, err := parser.ParseSources(fset, map[string]string{"virtual.go": src})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(fset, pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pkg, []string{"virtual.go"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	call := findNode(t, doc, "call", nil)
	pos, _ := call["pos"].(map[string]any)
	line := strings.Split(src, "\n")[2]
	byteCol := strings.Index(line, "println") + 1
	runeCol := utf8.RuneCountInString(line[:strings.Index(line, "println")]) + 1
	if byteCol == runeCol {
		t.Fatalf("probe is vacuous: byte col %d == rune col %d", byteCol, runeCol)
	}
	if pos["col"] != byteCol {
		t.Errorf("in-memory fallback col = %v, want byte col %d (rune col would be %d)", pos["col"], byteCol, runeCol)
	}
}
