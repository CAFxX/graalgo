package main

// R-02 (2026-10-31): crypto/subtle retirement test. The synthetic
// subtle package is deleted; a program importing "crypto/subtle" must
// export via the general disk importer from real GOROOT go1.27.1
// sources. The test distinguishes real from synthetic by exact
// funcdecl names, not substrings:
//   - WithDataIndependentTiming exists ONLY in the real source
//     (crypto/subtle/dit.go); the synthetic never defined it.
//   - inexactOverlap was a helper of the synthetic source only.
// No boolToUint* absence is asserted, deliberately: the deleted
// synthetic defined only boolToUint8, and the still-synthetic
// crypto/internal/constanttime package legitimately defines
// boolToUint8 too, so a boolToUint8 absence check could never
// discriminate the retired synthetic from the live one — and a
// bare "boolToUint" name never existed in either source, so an
// assertion on it would pass whether or not the synthetic returns.
// Discrimination rests only on the real-only
// WithDataIndependentTiming and the synthetic-only inexactOverlap.
// The test is capable of failing: re-adding the syntheticSources
// entry drops WithDataIndependentTiming and restores inexactOverlap,
// and breaking the disk route fails ExportFile.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSubtleDiskExport(t *testing.T) {
	src := `package main

import "crypto/subtle"

func main() {
	println(subtle.ConstantTimeCompare([]byte("abc"), []byte("abc")))
	println(subtle.ConstantTimeByteEq(1, 1))
	dst := []byte("x")
	subtle.ConstantTimeCopy(1, dst, []byte("y"))
	println(string(dst))
	println(subtle.XORBytes([]byte("a"), []byte("b"), []byte("c")))
	subtle.WithDataIndependentTiming(func() { println("dit") })
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "subtle.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
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
	for _, want := range []string{"ConstantTimeCompare", "ConstantTimeByteEq", "ConstantTimeCopy", "XORBytes", "WithDataIndependentTiming"} {
		if !names[want] {
			t.Errorf("missing funcdecl %s (real-source marker)", want)
		}
	}
	for _, gone := range []string{"inexactOverlap"} {
		if names[gone] {
			t.Errorf("synthetic-only funcdecl %s still present: synthetic subtle was not retired", gone)
		}
	}
	// Note: boolToUint8 is deliberately NOT asserted absent here: it
	// is legitimately defined by the still-synthetic constanttime
	// package, so its presence cannot discriminate, and the deleted
	// synthetic defined no other boolToUint* name.
}
