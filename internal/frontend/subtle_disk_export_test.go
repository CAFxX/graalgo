package main

// R-02 (2026-10-31): crypto/subtle retirement test. The synthetic
// subtle package is deleted; a program importing "crypto/subtle" must
// export via the general disk importer from real GOROOT go1.27.1
// sources. The test distinguishes real from synthetic by exact
// funcdecl names, not substrings:
//   - WithDataIndependentTiming exists ONLY in the real source
//     (crypto/subtle/dit.go); the synthetic never defined it.
//   - boolToUint / inexactOverlap were helpers of the synthetic
//     source only. (boolToUint8, with a trailing 8, is a DIFFERENT
//     function in the still-synthetic crypto/internal/constanttime
//     package and may legitimately appear; the check below compares
//     whole names, so it cannot false-positive on it.)
// The test is capable of failing: re-adding the syntheticSources
// entry drops WithDataIndependentTiming and restores boolToUint /
// inexactOverlap, and breaking the disk route fails ExportFile.

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
	for _, gone := range []string{"boolToUint", "inexactOverlap"} {
		if names[gone] {
			t.Errorf("synthetic-only funcdecl %s still present: synthetic subtle was not retired", gone)
		}
	}
	// Note: the still-synthetic constanttime helper boolToUint8 (with
	// a trailing 8) is a different name and must not satisfy the
	// absence check above; the check compares whole names.
}
