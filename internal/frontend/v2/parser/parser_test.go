// Parser tests (review-1105-v2-types): nil FileSet guard and
// duplicate-path deduplication.
package parser

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSourcesNilFileSet(t *testing.T) {
	_, err := ParseSources((*token.FileSet)(nil), map[string]string{
		"x.go": "package demo\nfunc main() {}\n",
	})
	if err == nil {
		t.Fatal("ParseSources(nil fset) returned nil error, want a clean error (it panicked before the fix)")
	}
}

func TestParseFilesDedupsRepeatedPath(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "dup.go")
	if err := os.WriteFile(fn, []byte("package demo\nfunc main() { println(42) }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := ParseFiles([]string{fn, fn, fn})
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	if len(res.Files) != 1 {
		t.Errorf("Files = %v (len %d), want the repeated path recorded once", res.Files, len(res.Files))
	}
	if len(res.Pkg.Files) != 1 {
		t.Errorf("Pkg.Files has %d entries, want 1", len(res.Pkg.Files))
	}
}

func TestParseFilesKeepsInputOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		src := "package demo\nfunc F" + name[:1] + "() { println(42) }\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	res, err := ParseFiles([]string{b, a})
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	if len(res.Files) != 2 || res.Files[0] != b || res.Files[1] != a {
		t.Errorf("Files = %v, want [b.go a.go] in input order", res.Files)
	}
}
