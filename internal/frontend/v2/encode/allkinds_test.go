package encode

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// TestEncodeAllKinds exercises every v2 emitter node kind through the
// full pipeline: parse → check → emit → encode.
//
// The critical assertion is on the EMITTED DOCUMENT (not just the byte
// stream): every emitter node kind must appear ≥1 in doc.Funcs. This pins
// the SILENT-DROP failure mode — a regression where a node kind vanishes
// from the emitter without an error — which the byte-level assertions below
// cannot catch. (The LOUD-failure path, an unhandled kind making Emit
// return an error, is pinned by emit/coverage_test.go + negative_test.go.)
func TestEncodeAllKinds(t *testing.T) {
	src := `package main

import "fmt"

type Point struct {
	X int
	Y int
}

func add(a, b int) int {
	return a + b
}

func main() {
	// vardecl with values
	var x int = 10
	var y = 20
	var s string = "hello"
	var b bool = true
	var f float64 = 3.14

	// assign (= and :=)
	x = y + 5
	z := x * 2

	// binary ops (all)
	_ = x + y
	_ = x - y
	_ = x * y
	_ = x / y
	_ = x % y
	_ = x & y
	_ = x | y
	_ = x ^ y
	_ = x << 2
	_ = x >> 2
	_ = x == y
	_ = x != y
	_ = x < y
	_ = x <= y
	_ = x > y
	_ = x >= y
	_ = b && true
	_ = b || false

	// unary ops
	_ = -x
	_ = ^x
	_ = !b
	_ = +x

	// paren
	_ = (x + y) * z

	// if / else if / else
	if x > 0 {
		z = 1
	} else if x < 0 {
		z = -1
	} else {
		z = 0
	}

	// if with init
	if n := x + y; n > 0 {
		z = n
	}

	// for (classic)
	sum := 0
	for i := 0; i < 10; i++ {
		sum += i
	}

	// for (condition only)
	for sum < 100 {
		sum++
	}

	// for (infinite with break)
	for {
		if sum > 200 {
			break
		}
		sum++
	}

	// range over int
	for i := range 5 {
		sum += i
	}

	// range over slice
	nums := []int{1, 2, 3}
	for i, v := range nums {
		sum += i + v
	}

	// range over array
	arr := [3]int{4, 5, 6}
	for _, v := range arr {
		sum += v
	}

	// range over map
	m := map[string]int{"a": 1}
	for k, v := range m {
		_ = k
		sum += v
	}

	// range over string
	for i, r := range s {
		_ = i
		_ = r
	}

	// incdec
	x++
	y--

	// branch (continue)
	for i := 0; i < 10; i++ {
		if i == 5 {
			continue
		}
		sum += i
	}

	// call (user function)
	result := add(x, y)
	_ = result

	// call (builtin)
	n := len(nums)
	_ = n
	c := cap(nums)
	_ = c

	// call (package-qualified → pkgref)
	fmt.Sprint(x)

	// index
	v0 := nums[0]
	_ = v0

	// slice
	sl := nums[1:2]
	_ = sl

	// selector (field access)
	p := Point{X: 1, Y: 2}
	px := p.X
	_ = px
	p.Y = 10

	// basiclit (all types)
	var i8 int8 = 8
	var i16 int16 = 16
	var i32 int32 = 32
	var i64 int64 = 64
	var u uint = 1
	var f32 float32 = 1.5
	var c64 complex64 = 1 + 2i
	var ch rune = 'a'
	_, _, _, _, _, _, _, _ = i8, i16, i32, i64, u, f32, c64, ch

	// return tested via add() above
	_ = z
	_ = f
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "allkinds.go")
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

	// Pin the silent-drop path: every emitter node kind must appear ≥1 in
	// the emitted document. Walk only doc.Funcs — the Types/Objects tables
	// use "kind" keys too ("slice", "map", ...) that would mask a dropped
	// AST node kind.
	assertAllNodeKindsPresent(t, doc)

	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(bin) == 0 {
		t.Fatal("encoded output is empty")
	}
	t.Logf("encoded %d bytes for all-kinds program", len(bin))

	// Verify the binary starts with the magic + version header, then the
	// top-level map tag (not JSON: a JSON encoding would start with '{').
	if string(bin[:len(formatMagic)]) != formatMagic {
		t.Fatalf("missing magic header: first bytes = %x", bin[:min(4, len(bin))])
	}
	rest := bin[len(formatMagic):]
	ver, n := binary.Uvarint(rest)
	if n <= 0 || ver != formatVersion {
		t.Fatalf("bad version header: %x", rest[:min(4, len(rest))])
	}
	if rest[n] != tagMap {
		t.Fatalf("first value tag = 0x%02x, want map tag 0x%02x", rest[n], tagMap)
	}
}

// expectedNodeKinds is the full set of node kinds the v2 emitter produces
// (one per emit function in emit/nodes.go, plus the inline "keyvalue"
// nodes from emitCompositeLit). The all-kinds program above is written to
// exercise every one of them. When a new node kind is added to the emitter,
// add a construct that produces it to the program above and extend this
// list — a missing entry here is a hole in the silent-drop pin.
var expectedNodeKinds = []string{
	// Top-level / structure.
	"funcdecl", "param", "block",
	// Statements.
	"exprstmt", "return", "assign", "vardecl", "if", "for", "range",
	"incdec", "branch",
	// Expressions.
	"unary", "paren", "call", "selector", "pkgref",
	"compositelit", "keyvalue", "index", "slice",
	"ident", "basiclit", "binary",
}

// assertAllNodeKindsPresent fails if any expected emitter node kind is
// absent from the emitted document. A missing kind means the emitter
// silently dropped that construct — the pipeline stayed green while the
// program's meaning changed, which this test exists to forbid.
func assertAllNodeKindsPresent(t *testing.T, doc *emit.Document) {
	t.Helper()
	found := collectNodeKinds(doc.Funcs)
	sorted := make([]string, 0, len(found))
	for k := range found {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	t.Logf("emitted node kinds (%d): %s", len(sorted), strings.Join(sorted, ", "))
	var missing []string
	for _, want := range expectedNodeKinds {
		if !found[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("silent-drop: emitted document is missing %d node kind(s): %s\npresent kinds: %s",
			len(missing), strings.Join(missing, ", "), strings.Join(sorted, ", "))
	}
}

// collectNodeKinds returns the set of every "kind" string appearing in the
// node maps of v. Nodes are map[string]any{"kind", "pos", "fields"} (or the
// inline {"kind", "fields"} keyvalue nodes); position maps carry no "kind"
// key, so every hit is a node kind.
func collectNodeKinds(v any) map[string]bool {
	kinds := map[string]bool{}
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if k, ok := t["kind"].(string); ok {
				kinds[k] = true
			}
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return kinds
}
