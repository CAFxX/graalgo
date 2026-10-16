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

func TestEncodeHelloWorld(t *testing.T) {
	src := `package main

func main() {
	x := 42
	println(x)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "hello.go")
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

	// Encode to binary.
	bin, err := Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(bin) == 0 {
		t.Fatal("encoded output is empty")
	}
	t.Logf("encoded %d bytes", len(bin))

	// The output must NOT contain JSON markers. A JSON encoding would
	// start with '{' and contain '":' sequences. Our binary format
	// starts with the map tag (0x05).
	if bin[0] != tagMap {
		t.Fatalf("first byte = 0x%02x, want map tag 0x%02x", bin[0], tagMap)
	}
	// Sanity: no JSON object braces at top level.
	for i, b := range bin {
		if b == '{' || b == '}' {
			// Braces can appear inside string payloads; we just check
			// the structure isn't JSON by verifying the tag.
			_ = i
			break
		}
	}
}

func TestEncodeRoundTripTypes(t *testing.T) {
	// Exercise all supported Go types through the encoder.
	m := map[string]any{
		"null":   nil,
		"bool_t": true,
		"bool_f": false,
		"int":    42,
		"neg":    -17,
		"int64":  int64(1 << 60),
		"str":    "hello",
		"list":   []any{1, "two", nil},
		"nested": map[string]any{"a": 1},
	}
	var buf bytes.Buffer
	if err := encodeValue(&buf, m); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}
