package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticMathRandV2ChaCha8 is the review23-chacha8 regression test
// for P1-7 (carried from the 13:00 review's P1-14): guest math/rand/v2's
// ChaCha8 was a panic stub ("ChaCha8.Read not implemented"), which also
// blocked crypto/ecdsa signing on non-standard curves (signLegacy drives
// ChaCha8 through io.Reader). The generator now carries gc 1.27.1's
// chacha8.go verbatim (with the __chacha8* engine restored), so a guest
// program must export cleanly when it uses ChaCha8 through every
// supported call path, and no raw multi-result funcdecl may leak into
// the wire doc (multi-result methods are pair-transformed).
//
// This is an export-level test: it proves the frontend accepts the real
// ChaCha8 and emits it in pair form. Byte-identity of the stream vs gc
// is proven by the gc harness (see the lane report: the generated
// synthetic source compiled under gc matches gc's math/rand/v2 for
// fixed seeds, including Read spans and the Marshal/Unmarshal
// round-trip), and end-to-end behavior by the ChaCha8 regression test
// in test/regression/chacha8. The ecdsa half is pending on a
// pre-existing backend gap (no funcfieldcall wire-kind handler; see the
// pending test's header) and is not in test/regression/ by design.
func TestSyntheticMathRandV2ChaCha8(t *testing.T) {
	src := `package main

import (
	"fmt"
	"io"
	randv2 "math/rand/v2"
)

func main() {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	c := randv2.NewChaCha8(seed)
	// Uint64 stream (single-result method).
	fmt.Printf("%x\n", c.Uint64())
	// Direct multi-result Read call.
	buf := make([]byte, 37)
	n, err := c.Read(buf)
	fmt.Println(n, err)
	// Interface dispatch: io.ReadFull drives Read through io.Reader,
	// the exact path crypto/ecdsa's signLegacy uses.
	var r io.Reader = randv2.NewChaCha8(seed)
	full := make([]byte, 64)
	if _, err := io.ReadFull(r, full); err != nil {
		fmt.Println("readfull err:", err)
	}
	// Marshal/Unmarshal round-trip (multi-result methods).
	c2 := randv2.NewChaCha8(seed)
	_ = c2.Uint64()
	enc, err := c2.MarshalBinary()
	fmt.Println(err)
	c3 := randv2.NewChaCha8(seed)
	_ = c3.Uint64()
	if err := c3.UnmarshalBinary(enc); err != nil {
		fmt.Println("unmarshal err:", err)
	}
	fmt.Println(c2.Uint64() == c3.Uint64())
	// AppendBinary (multi-result method, direct call).
	ab, err := c2.AppendBinary(nil)
	fmt.Println(len(ab), err)
	// Seed determinism through the Rand wrapper.
	ra := randv2.New(randv2.NewChaCha8(seed))
	fmt.Println(ra.Int64N(1000000))
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
	// The panic stub must be gone: the wire doc must not contain its
	// message, and the Read body must not be a panic.
	if strings.Contains(out, "ChaCha8.Read not implemented") {
		t.Errorf("wire doc still contains the ChaCha8 panic stub")
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	results := map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if pp, _ := f["pkgpath"].(string); pp == "math/rand/v2" {
						if n, ok := f["name"].(string); ok {
							nr := 0
							if rs, ok := f["results"].([]any); ok {
								nr = len(rs)
							}
							if nr > results[n] {
								results[n] = nr
							}
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	// The three multi-result methods must be present in pair form
	// (exactly 1 pair result): present-but-pair proves the exporter
	// pair-transformed them; a raw 2-result leak or an absence would
	// both mean the fix did not land.
	for _, name := range []string{"Read", "AppendBinary", "MarshalBinary"} {
		nr, ok := results[name]
		if !ok {
			t.Errorf("wire doc missing math/rand/v2 funcdecl %q", name)
		} else if nr != 1 {
			t.Errorf("wire doc math/rand/v2 funcdecl %q has %d results, want 1 (pair form)", name, nr)
		}
	}
	// Negative control: the same program with a bogus method must fail
	// to export, proving this test is capable of failing (not vacuous).
	bad := strings.Replace(src, "c.Uint64()", "c.NoSuchMethod()", 1)
	bp := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bp, []byte(bad), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(bp); err == nil {
		t.Errorf("negative control: export of program calling ChaCha8.NoSuchMethod succeeded, want failure")
	}
}
