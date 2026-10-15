package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticPathErrBadPattern is the PKG-029 regression test: the
// synthetic `path` package must expose ErrBadPattern. The generator used
// to port only path.go, dropping match.go's ErrBadPattern along with the
// scope-limited pattern-matching machinery — but ErrBadPattern is a
// dependency-free exported var that consumers reference directly (io/fs's
// Glob reports path.ErrBadPattern; io/fs/glob_test.go asserts on it).
// Without it, any path.ErrBadPattern reference fails type-checking with
// "undefined: path.ErrBadPattern" even though the importing file's import
// block is correct (the test-driver synthesis was exonerated: the
// generated driver does carry `import "path"`, and gc accepts it).
//
// The test mirrors the real io/fs driver shape: "path" imported both
// plain and under an explicit name (glob_test.go and walk_test.go do
// exactly this), with the qualified reference through the plain import.
func TestSyntheticPathErrBadPattern(t *testing.T) {
	src := `package main

import (
	"path"
	pathpkg "path"
)

func isBadPattern(err error) bool { return err == path.ErrBadPattern }

func main() {
	println(isBadPattern(pathpkg.ErrBadPattern))
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synpath.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}
