package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticMathRandV2ChaCha8 (R4) verifies the ChaCha8 surface that
// was a fail-fast stub before R4: NewChaCha8/Uint64/Seed plus the
// multi-result Read/AppendBinary/MarshalBinary, desugared to their
// __-helpers (Appendix M). The program must export cleanly — a
// regression to the stub would still export (the stub is a real
// funcdecl), so the test also asserts the wire carries the helper
// funcdecls, carries no stub Read funcdecl, and contains none of the
// old stub panic text. Runtime byte-identity vs gc is pinned by
// test/corpus/wave3/math/rand/v2/01_chacha8.go.
func TestSyntheticMathRandV2ChaCha8(t *testing.T) {
	src := `package main

import (
	"io"
	randv2 "math/rand/v2"
)

func main() {
	c := randv2.NewChaCha8([32]byte{1, 2, 3})
	println(c.Uint64())
	buf := make([]byte, 16)
	n, err := c.Read(buf)
	println(n, err == nil)
	var r io.Reader = randv2.NewChaCha8([32]byte{1, 2, 3})
	n2, err2 := r.Read(buf)
	println(n2, err2 == nil)
	b, err3 := c.MarshalBinary()
	println(len(b), err3 == nil)
	b2, err4 := c.AppendBinary(nil)
	println(len(b2), err4 == nil)
	c2 := randv2.NewChaCha8([32]byte{})
	println(c2.UnmarshalBinary(b) == nil)
	c.Seed([32]byte{4, 5, 6})
	println(c.Uint64())
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "chacha8.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("wire JSON: %v", err)
	}
	decls := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["kind"] == "funcdecl" {
				if f, ok := x["fields"].(map[string]any); ok {
					if pp, _ := f["pkgpath"].(string); pp == "math/rand/v2" {
						if n, ok := f["name"].(string); ok {
							decls[n] = true
						}
					}
				}
			}
			for _, v2 := range x {
				walk(v2)
			}
		case []any:
			for _, v2 := range x {
				walk(v2)
			}
		}
	}
	walk(raw)
	for _, helper := range []string{"__chacha8Read", "__chacha8AppendBinary", "__chacha8MarshalBinary", "__chacha8Block", "__chacha8Init", "NewChaCha8"} {
		if !decls[helper] {
			t.Errorf("wire lacks math/rand/v2 funcdecl %s", helper)
		}
	}
	if decls["Read"] {
		t.Errorf("wire still emits a math/rand/v2 Read stub funcdecl; the Appendix-M desugar should skip it")
	}
	if strings.Contains(out, "ChaCha8.Read not implemented") {
		t.Errorf("wire still contains the pre-R4 ChaCha8.Read stub panic")
	}
}
