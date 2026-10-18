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

// TestEncodeAllKinds exercises every v2 emitter node kind through the
// full pipeline: parse → check → emit → encode. If any kind is missing
// from the emitter or encoder, this test fails.
//
// The test decodes the binary it just produced (exactly what the Java
// BinaryDecoder would receive) and asserts the emitted type vocabulary is
// within GoKind.fromBasic's accepted set: every basic (name, class) pair
// must resolve on the Java side instead of throwing. (L20: the test used
// to assert only the magic header, so a program the Java factory cannot
// decode — e.g. bool/uint types via the L01 basicClass bug, or keyed
// literals on named structs via L16 — still passed.)
func TestEncodeAllKinds(t *testing.T) {
	src := `package main

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

	// Decode the document exactly as the Java BinaryDecoder would, then
	// assert the type vocabulary the Java factory must resolve. A program
	// whose types the Java side cannot decode must fail HERE, not pass on
	// the strength of the magic header alone (L20).
	decoded, tail := decodeTestValue(t, rest[n:])
	if len(tail) != 0 {
		t.Fatalf("%d trailing bytes after decoded document", len(tail))
	}
	top, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded top-level value is %T, want map", decoded)
	}
	typesVal, ok := top["types"]
	if !ok {
		t.Fatal(`decoded document has no "types" table`)
	}
	typeTable, ok := typesVal.([]any)
	if !ok {
		t.Fatalf(`"types" is %T, want list`, typesVal)
	}
	assertTypeVocabulary(t, typeTable)
}

// goKindFromBasicAccepted mirrors the (class → names) accepted set of
// GoKind.fromBasic in
// internal/backend/truffle/src/main/java/graalgo/values/GoKind.java.
// The Java side throws on any basic (name, class) pair outside this set,
// so the emitter's vocabulary must stay within it. Keep the two in
// lockstep: any change to fromBasic's switch must update this table (and
// vice versa). Class "pointer" accepts any name on the Java side
// (unsafe.Pointer), handled separately below.
var goKindFromBasicAccepted = map[string]map[string]bool{
	"int": {
		"int8": true, "int16": true, "int32": true, "rune": true,
		"int64": true, "int": true,
		"untyped int": true, "untyped rune": true,
	},
	"uint": {
		"uint8": true, "byte": true, "uint16": true, "uint32": true,
		"uint64": true, "uint": true, "uintptr": true,
		"untyped int": true, "untyped rune": true,
	},
	"float": {
		"float32": true, "float64": true, "untyped float": true,
	},
	"complex": {
		"complex64": true, "complex128": true, "untyped complex": true,
	},
	"bool": {
		"bool": true, "untyped bool": true,
	},
	"string": {
		"string": true, "untyped string": true,
	},
}

// goKindHandledKinds mirrors the type-table kinds NodeFactoryV2 resolves
// in goKindForTypeId
// (internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java).
// An emitter kind outside this set is vocabulary drift and must fail
// loudly here.
var goKindHandledKinds = map[string]bool{
	"basic": true, "pointer": true, "slice": true, "array": true,
	"map": true, "chan": true, "signature": true, "func": true,
	"struct": true, "interface": true, "named": true,
}

// assertTypeVocabulary checks every entry of the decoded v2 type table
// against what the Java factory can resolve: basic (name, class) pairs
// must be within GoKind.fromBasic's accepted set, every other kind must
// be one NodeFactoryV2 handles, and named types must reference a valid
// underlying type id (the L16 resolution path walks it).
func assertTypeVocabulary(t *testing.T, typeTable []any) {
	t.Helper()
	// The all-kinds program must actually exercise the historically broken
	// vocabulary (L01): require the bool and uint basic types to be
	// present, so a future edit cannot "fix" this test by deleting them
	// from the program.
	seenBasic := map[string]bool{}
	for i, te := range typeTable {
		tm, ok := te.(map[string]any)
		if !ok {
			t.Fatalf("types[%d] is %T, want map", i, te)
		}
		kindVal, ok := tm["kind"]
		if !ok {
			t.Fatalf("types[%d] has no \"kind\"", i)
		}
		kind, ok := kindVal.(string)
		if !ok {
			t.Fatalf("types[%d].kind is %T, want string", i, kindVal)
		}
		if !goKindHandledKinds[kind] {
			t.Errorf("types[%d]: kind %q is not handled by NodeFactoryV2.goKindForTypeId", i, kind)
			continue
		}
		detailVal, _ := tm["detail"]
		detail, _ := detailVal.(map[string]any)
		switch kind {
		case "basic":
			name, ok1 := detail["name"].(string)
			class, ok2 := detail["class"].(string)
			if !ok1 || !ok2 {
				t.Errorf("types[%d]: basic detail name/class not strings: %#v", i, detail)
				continue
			}
			seenBasic[name] = true
			if class == "pointer" {
				continue // Java: any name with class "pointer" → UNSAFE_POINTER
			}
			names, ok := goKindFromBasicAccepted[class]
			if !ok {
				t.Errorf("types[%d]: basic class %q is rejected by GoKind.fromBasic (name %q) — the Java factory would throw",
					i, class, name)
				continue
			}
			if !names[name] {
				t.Errorf("types[%d]: GoKind.fromBasic(%q, %q) throws — the Java factory cannot decode this type",
					i, name, class)
			}
		case "named":
			under, ok := detail["underlying"].(uint64)
			if !ok {
				t.Errorf("types[%d]: named detail.underlying is %T, want type id", i, detail["underlying"])
				continue
			}
			if under >= uint64(len(typeTable)) {
				t.Errorf("types[%d]: named underlying type id %d out of range (table has %d entries)",
					i, under, len(typeTable))
			}
		}
	}
	for _, want := range []string{"bool", "uint"} {
		if !seenBasic[want] {
			t.Errorf("test program does not exercise basic type %q — add it to the program, don't weaken this check", want)
		}
	}
}
