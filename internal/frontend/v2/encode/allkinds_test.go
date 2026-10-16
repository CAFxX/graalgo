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

	// basiclit (all types, incl. every unsigned width: V2-1 regression —
	// unsigned basics used to emit class "int", bool class "unknown")
	var i8 int8 = 8
	var i16 int16 = 16
	var i32 int32 = 32
	var i64 int64 = 64
	var u uint = 1
	var u8 uint8 = 2
	var u16 uint16 = 3
	var u32 uint32 = 4
	var u64 uint64 = 5
	var up uintptr = 6
	var by byte = 7
	var f32 float32 = 1.5
	var c64 complex64 = 1 + 2i
	var ch rune = 'a'
	_, _, _, _, _, _, _, _, _, _, _, _, _, _, _ = i8, i16, i32, i64, u, u8, u16, u32, u64, up, by, f32, c64, ch, b

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
}
