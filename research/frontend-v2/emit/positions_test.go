package emit

// Positions are rune-based (spec §3): token.Position.Column counts
// bytes, so a node after multi-byte runes on its line must report the
// rune column, not the byte column. Regression test for review
// P2-1 (v2 runeCol returned the byte column verbatim).

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// The second statement sits on a line with two 2-byte runes before it:
// `x := "éé"; y := 1` — y is byte column 14 but rune column 12.
// The first statement (x) is unaffected (column 1 on both scales),
// which pins that the fix only shifts columns after multi-byte runes.
const runeColSource = `package p

func F() {
x := "éé"; y := 1
_ = x
_ = y
}
`

func TestEmitRuneColumns(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "p.go")
	if err := os.WriteFile(fn, []byte(runeColSource), 0644); err != nil {
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
	doc, err := Emit(cres, pres.Pkg, pres.Files, pres.Sources)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Funcs) != 1 {
		t.Fatalf("funcs = %d, want 1", len(doc.Funcs))
	}
	fnNode, ok := doc.Funcs[0].(map[string]any)
	if !ok {
		t.Fatalf("func node is %T, want map", doc.Funcs[0])
	}
	fields, ok := fnNode["fields"].(map[string]any)
	if !ok {
		t.Fatalf("func fields is %T, want map", fnNode["fields"])
	}
	body, ok := fields["body"].(map[string]any)
	if !ok {
		t.Fatalf("func body is %T, want map", fields["body"])
	}
	bodyFields, ok := body["fields"].(map[string]any)
	if !ok {
		t.Fatalf("body fields is %T, want map", body["fields"])
	}
	stmts, ok := bodyFields["stmts"].([]any)
	if !ok || len(stmts) < 2 {
		t.Fatalf("stmts = %v, want at least 2", bodyFields["stmts"])
	}
	posOf := func(stmt any) map[string]any {
		node, ok := stmt.(map[string]any)
		if !ok {
			t.Fatalf("stmt is %T, want map", stmt)
		}
		pos, ok := node["pos"].(map[string]any)
		if !ok {
			t.Fatalf("stmt pos is %T, want map", node["pos"])
		}
		return pos
	}
	first := posOf(stmts[0])
	if first["line"] != 4 || first["col"] != 1 {
		t.Errorf("first stmt pos = line %v col %v, want line 4 col 1", first["line"], first["col"])
	}
	second := posOf(stmts[1])
	if second["line"] != 4 || second["col"] != 12 {
		t.Errorf("second stmt pos = line %v col %v, want line 4 col 12 (rune column; byte column would be 14)", second["line"], second["col"])
	}
}
