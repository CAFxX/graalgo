// Encode round-trip for generic function instantiations.
//
// A monomorphized instantiation must cross the Go→Java binary interface as
// an ordinary funcdecl with concrete types: no new node kinds, no format
// changes. This test proves the wire format carries instantiations without
// modification.
package encode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// encodedName returns the binary pattern for a "name" map entry with the
// given value: the bare key (varint length + "name") followed by the
// tagged string value (0x03 tag + varint length + bytes). The
// length-prefixing makes the match exact: "id" does not match "id[int]".
func encodedName(value string) string {
	return fmt.Sprintf("\x04name\x03%c%s", len(value), value)
}

// TestEncodeGenerics runs a generic program through parse → check → emit →
// encode and asserts the instantiated functions survive as plain funcdecls.
func TestEncodeGenerics(t *testing.T) {
	src := `package main

func id[T any](x T) T { return x }
func pair[A, B any](a A, b B) B { return b }

func main() {
	x := id[int](42)
	y := pair[int, string](1, "s")
	z := id("inferred")
	println(x, y, z)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "gen.go")
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
	sbin := string(bin)

	// Each instantiation is an ordinary funcdecl under its mangled name.
	for _, want := range []string{"main", "id[int]", "pair[int,string]", "id[string]"} {
		if !strings.Contains(sbin, encodedName(want)) {
			t.Errorf("encoded binary has no funcdecl named %q (instantiation lost on the wire)", want)
		}
	}
	// The generic templates are never emitted as funcdecls.
	for _, notWant := range []string{"id", "pair"} {
		if strings.Contains(sbin, encodedName(notWant)) {
			t.Errorf("encoded binary has a funcdecl named %q (template leaked as callable function)", notWant)
		}
	}
	// No type parameter may leak into the encoded type table: every
	// emitted type is concrete.
	if strings.Contains(sbin, "typeparam") {
		t.Errorf("encoded binary contains \"typeparam\" (a type parameter leaked into the type table)")
	}
}
