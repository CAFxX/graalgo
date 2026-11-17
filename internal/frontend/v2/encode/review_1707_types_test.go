// Regression tests for the review-1707-p0-v2-types remediation lane
// (independent codebase review, 2026-11-17, P0-1): the wire must
// distinguish generic instantiations.
//
// The two programs below are position-identical (Aaa/Bbb are
// same-length names, so every node position matches) and differ only
// in their type argument. Tag's underlying struct never mentions T,
// so pre-fix the documents were identical and encoded to identical
// bytes; post-fix the type args distinguish them.
package encode

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// encodeSrc runs one source file through parse → check → emit → Encode.
func encodeSrc(t *testing.T, src string) []byte {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
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
	return bin
}

func TestEncodeGenericTagDiffers(t *testing.T) {
	aaaBin := encodeSrc(t, `package main

type Aaa struct{ x int }
type Bbb struct{ x int }
type Tag[T any] struct{ n int }

func Use() {
	var a Tag[Aaa]
	_ = a
}
`)
	bbbBin := encodeSrc(t, `package main

type Aaa struct{ x int }
type Bbb struct{ x int }
type Tag[T any] struct{ n int }

func Use() {
	var a Tag[Bbb]
	_ = a
}
`)
	if bytes.Equal(aaaBin, bbbBin) {
		t.Fatalf("Tag[Aaa] and Tag[Bbb] encode to identical %d bytes", len(aaaBin))
	}
}
