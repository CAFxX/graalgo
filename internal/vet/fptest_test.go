package vet

import (
	"os"
	"path/filepath"
	"testing"
)

// testPass parses and type-checks src (a complete Go source file) and
// returns the resulting Pass, mirroring the front end of vet.Run without
// running any analyzer. FP regression tests invoke the analyzer under
// test directly, so each test asserts only on that analyzer's
// diagnostics and stays independent of import resolution and of other
// analyzers' behavior.
func testPass(t *testing.T, src string) *Pass {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pass, err := checkPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return pass
}
