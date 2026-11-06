// Regression tests for the review-1106-p0-basiclit remediation lane
// (independent codebase review, 2026-11-06, R-P0-2).
//
// The v2 emitter used to send the raw source spelling of a basic literal
// as its wire "value", while the Java consumer parsed "value" as the
// value itself (never unescaping): "a\nb" decoded to 4 chars, '\n' to
// rune 92, and legacy octal 017 to 17. The emitter now sends the
// canonical decoded value and the consumer uses it verbatim.
//
// The tests are demonstrably capable of failing: each expected value
// differs from the raw spelling the old emitter sent, and the multiset
// is exact, so any surviving raw spelling (or any missing literal)
// fails the test. They fail on the pre-fix emitter (verified by running
// them against master@5041ac00).
package emit

import (
	"testing"
)

// TestBasicLitCanonicalValues: every basic literal kind round-trips as
// its canonical value — escapes decoded, rune as code point, legacy
// octal/hex/underscored ints as decimal, floats as parseable decimal
// (never go/constant's "a/b" ExactString form).
func TestBasicLitCanonicalValues(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	s := "a\nb"
	u := "a\x41b"
	r := `+"`raw\\n`"+`
	c := '\n'
	d := 'a'
	n := 017
	h := 0x10
	k := 1_000
	f := 1.5
	g := 1e3
	j := 0x1.8p1
	q := "\"x\""
	e := ""
	p := "hello"
	var f32 float32 = 1.1
	println(s, u, r, c, d, n, h, k, f, g, j, q, e, p, f32)
}
`)
	lits := nodesOf(t, doc, "basiclit")
	want := map[string]int{
		"a\nb":  1, // escape decoded: real newline, 3 chars
		"aAb":   1, // \x41 decoded
		`raw\n`: 1, // raw string: backslash kept, not unescaped
		"10":    1, // '\n' as code point, not 92 (backslash)
		"97":    1, // 'a'
		"15":    1, // legacy octal 017, not 17
		"16":    1, // 0x10
		"1000":  2, // 1_000 and 1e3
		"1.5":   1, // never "3/2"
		"3":     1, // 0x1.8p1
		`"x"`:   1, // decoded value starts+ends with '"'; must survive intact
		"":      1, // empty string
		"hello": 1,
		"1.1":   1, // float32-typed: float32 precision, not "9227469/8388608"
	}
	if len(lits) != 15 {
		t.Fatalf("basiclit nodes = %d, want 15", len(lits))
	}
	got := map[string]int{}
	for _, lit := range lits {
		v, _ := fieldsOf(lit)["value"].(string)
		got[v]++
	}
	for v, n := range want {
		if got[v] != n {
			t.Errorf("basiclit value %q occurs %d times, want %d (all values: %v)", v, got[v], n, got)
		}
	}
	for v, n := range got {
		if want[v] != n {
			t.Errorf("unexpected basiclit value %q occurs %d times (all values: %v)", v, n, got)
		}
	}
}

// TestBasicLitConstFieldKept: the "const" debug field still carries the
// go/constant ExactString, unchanged by the canonical-value fix.
func TestBasicLitConstFieldKept(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	n := 017
	s := "a\nb"
	println(n, s)
}
`)
	lits := nodesOf(t, doc, "basiclit")
	if len(lits) != 2 {
		t.Fatalf("basiclit nodes = %d, want 2", len(lits))
	}
	seen := map[string]string{}
	for _, lit := range lits {
		f := fieldsOf(lit)
		seen[f["value"].(string)] = f["const"].(string)
	}
	if seen["15"] != "15" {
		t.Errorf("017 const = %q, want \"15\"", seen["15"])
	}
	if seen["a\nb"] != `"a\nb"` {
		t.Errorf("string const = %q, want quoted \"a\\nb\"", seen["a\nb"])
	}
}
