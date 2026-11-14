package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStringIntConvGcParity pins stringintconv to gc vet 1.27.1 parity
// (R0614-P1-14).
//
// gc flags string(<int>) for every integer kind EXCEPT int32, uint8 and
// untyped rune constants. In go/types, rune IS int32
// (types.Rune == types.Int32) and byte IS uint8 (types.Byte == types.Uint8),
// so gc cannot distinguish a rune value from an int32 value (or a byte
// from a uint8) and exempts the whole kind. The ground truth is the gc
// vet 1.27.1 run over the same shapes (see the lane evidence file):
// flagged: int, int8, int16, int64, uint, uint16, uint32, uint64,
// uintptr, untyped int const, named int64; not flagged: int32, uint8,
// rune, byte, untyped rune const, named int32.
func TestStringIntConvGcParity(t *testing.T) {
	cases := []struct {
		name string
		stmt string
		want bool // gc vet flags this conversion
	}{
		{"int", `_ = string(i)`, true},
		{"int8", `_ = string(i8)`, true},
		{"int16", `_ = string(i16)`, true},
		{"int32", `_ = string(i32)`, false},
		{"int64", `_ = string(i64)`, true},
		{"uint", `_ = string(u)`, true},
		{"uint8", `_ = string(u8)`, false},
		{"uint16", `_ = string(u16)`, true},
		{"uint32", `_ = string(u32)`, true},
		{"uint64", `_ = string(u64)`, true},
		{"uintptr", `_ = string(up)`, true},
		{"rune", `_ = string(r)`, false},
		{"byte", `_ = string(b)`, false},
		{"untyped int const", `_ = string(65)`, true},
		{"untyped rune const", `_ = string('a')`, false},
		{"named int32", `_ = string(n32)`, false},
		{"named int64", `_ = string(n64)`, true},
		{"named rune", `_ = string(rr)`, false},
		{"named byte", `_ = string(bb)`, false},
	}

	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("type N32 int32\n")
	b.WriteString("type N64 int64\n")
	b.WriteString("type R rune\n")
	b.WriteString("type B byte\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tvar i int\n")
	b.WriteString("\tvar i8 int8\n")
	b.WriteString("\tvar i16 int16\n")
	b.WriteString("\tvar i32 int32\n")
	b.WriteString("\tvar i64 int64\n")
	b.WriteString("\tvar u uint\n")
	b.WriteString("\tvar u8 uint8\n")
	b.WriteString("\tvar u16 uint16\n")
	b.WriteString("\tvar u32 uint32\n")
	b.WriteString("\tvar u64 uint64\n")
	b.WriteString("\tvar up uintptr\n")
	b.WriteString("\tvar r rune\n")
	b.WriteString("\tvar b byte\n")
	b.WriteString("\tvar n32 N32\n")
	b.WriteString("\tvar n64 N64\n")
	b.WriteString("\tvar rr R\n")
	b.WriteString("\tvar bb B\n")
	headerLines := strings.Count(b.String(), "\n")
	wantLines := make(map[int]string)
	for j, c := range cases {
		b.WriteString("\t" + c.stmt + "\n")
		if c.want {
			wantLines[headerLines+j+1] = c.name
		}
	}
	b.WriteString("}\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	gotLines := make(map[int]string)
	for _, d := range ds {
		if !strings.Contains(d.Message, "one rune") {
			continue
		}
		gotLines[d.Pos.Line] = d.Message
	}
	for line, name := range wantLines {
		if _, ok := gotLines[line]; !ok {
			t.Errorf("string(%s): gc vet flags it, graalgo did not (line %d)", name, line)
		}
	}
	for line, msg := range gotLines {
		if _, ok := wantLines[line]; !ok {
			t.Errorf("unexpected stringintconv diagnostic at line %d: %s (gc vet does not flag this shape)", line, msg)
		}
	}
}
