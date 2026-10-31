package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticFailFastStubPolicy verifies the review-bytes-mr stub
// policy: a synthetic fail-fast stub (single panic("... not in M...
// scope ...") body, no desugar-table entry) is never registered as a
// callable function and never emitted, so every use position fails at
// export instead of linking to a guest-runtime panic.
//
// Probe: encoding/asn1.Unmarshal — a non-table-driven fail-fast stub
// (decoding stores through the recipient pointer, which v1 cannot
// express, so it is deliberately not in the m7 table).
//
// Red-green: before the policy fix, the stub was registered in
// funcByObj/synthFuncIDs and pair-transformed by the
// exportSyntheticBodies pre-pass, so a program merely importing
// encoding/asn1 carried an emitted "Unmarshal" funcdecl whose body
// panics at guest runtime if reached through a position the scope
// checks missed. After the fix, no such funcdecl exists.
func TestSyntheticFailFastStubPolicy(t *testing.T) {
	t.Run("call fails at export", func(t *testing.T) {
		src := `package main

import "encoding/asn1"

func main() {
	_, _ = asn1.Unmarshal([]byte{}, nil)
}
`
		dir := t.TempDir()
		p := filepath.Join(dir, "stubcall.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ExportFile(p)
		if err == nil {
			t.Fatal("ExportFile succeeded for asn1.Unmarshal call, want frontend scope error")
		}
		if !strings.Contains(err.Error(), "asn1.Unmarshal") {
			t.Fatalf("unexpected error for asn1.Unmarshal call: %v", err)
		}
	})

	t.Run("value position fails at export", func(t *testing.T) {
		src := `package main

import "encoding/asn1"

func main() {
	_ = asn1.Unmarshal
}
`
		dir := t.TempDir()
		p := filepath.Join(dir, "stubvalue.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ExportFile(p)
		if err == nil {
			t.Fatal("ExportFile succeeded for asn1.Unmarshal in value position, want frontend scope error")
		}
	})

	t.Run("stub never emitted on mere import", func(t *testing.T) {
		src := `package main

import "encoding/asn1"

func main() {
	_, _ = asn1.Marshal(1)
}
`
		// Note: asn1.Marshal is table-driven (its __-helper is real),
		// so importing the package and calling Marshal exports cleanly;
		// the point is that the Unmarshal stub body is not in the doc.
		doc := decodeExport(t, mustExport(t, src))
		for _, f := range findAllKind(doc, "funcdecl") {
			fl, _ := f["fields"].(map[string]any)
			if fl["name"] == "Unmarshal" {
				if pp, _ := fl["pkgpath"].(string); pp == "encoding/asn1" || pp == "" {
					t.Fatalf("wire doc contains emitted fail-fast stub funcdecl %q (pkgpath %q); stubs must never be emitted", "Unmarshal", pp)
				}
			}
		}
	})
}
