package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGolden: every non-bad_ testdata program exports byte-identical JSON to
// its checked-in golden file. The goldens pin the frontend/v1 wire format;
// any intentional format change must regenerate them explicitly
// (UPDATE_GOLDEN=1) and update docs/frontend-v1.md in the same change.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no testdata programs found")
	}
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".go")
		if strings.HasPrefix(base, "bad_") {
			continue
		}
		t.Run(base, func(t *testing.T) {
			out, err := ExportFile(f)
			if err != nil {
				t.Fatalf("ExportFile(%s): %v", f, err)
			}
			golden := filepath.Join("testdata", base+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, []byte(out+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", golden)
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to create)", golden, err)
			}
			if got := out + "\n"; got != string(want) {
				t.Fatalf("golden mismatch for %s:\nfirst diff at line %d", f, firstDiffLine(got, string(want)))
			}
		})
	}
}

func firstDiffLine(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	if len(al) != len(bl) {
		return min(len(al), len(bl)) + 1
	}
	return 0
}

// TestDeterministic: two exports of the same program must be byte-identical
// (no map-iteration nondeterminism may leak into the wire format).
func TestDeterministic(t *testing.T) {
	a, err := ExportFile("testdata/straight02.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ExportFile("testdata/straight02.go")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("ExportFile is not deterministic")
	}
}

// TestScopeErrors: out-of-scope programs fail fast with an explicit error
// naming the workstream that owns the extension.
func TestScopeErrors(t *testing.T) {
	cases := map[string]string{
		"testdata/bad_import.go": "WS G",
		"testdata/bad_select.go": "WS E",
		"testdata/bad_call.go":   "WS D",
	}
	for f, want := range cases {
		_, err := ExportFile(f)
		if err == nil {
			t.Fatalf("%s: expected scope error, got success", f)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %q does not attribute %s", f, err, want)
		}
	}
}

// TestTypeError: ill-typed programs are rejected by the vendored go/types
// before any JSON is emitted.
func TestTypeError(t *testing.T) {
	_, err := ExportFile("testdata/bad_type.go")
	if err == nil {
		t.Fatal("expected type error, got success")
	}
	if !strings.Contains(err.Error(), "type error") {
		t.Fatalf("expected a type error, got: %v", err)
	}
}

// TestNoMain: a file without func main is rejected explicitly.
func TestNoMain(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nomain.go")
	if err := os.WriteFile(p, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatal("expected no-func-main error, got success")
	}
	if !strings.Contains(err.Error(), "no func main") {
		t.Fatalf("expected no-func-main error, got: %v", err)
	}
}
